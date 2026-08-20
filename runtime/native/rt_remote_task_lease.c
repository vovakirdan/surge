#include "rt_remote_task_internal.h"

#include <stdbool.h>
#include <string.h>

rt_far_task_lease* rt_far_task_lease_find_locked(rt_remote_task_state* state,
                                                 const rt_far_task_handle* handle) {
    for (rt_far_task_lease* it = state != NULL ? state->lease_head : NULL; it != NULL;
         it = it->next) {
        if (&it->handle == handle) {
            return it;
        }
    }
    return NULL;
}

static void lease_unlink_locked(rt_remote_task_state* state, rt_far_task_lease* lease) {
    rt_far_task_lease** cursor = &state->lease_head;
    while (*cursor != NULL) {
        if (*cursor == lease) {
            *cursor = lease->next;
            lease->next = NULL;
            return;
        }
        cursor = &(*cursor)->next;
    }
}

void rt_far_task_lease_drop_ref(rt_far_task_lease* lease) {
    rt_remote_task_state* state = lease != NULL ? rt_remote_task_state_get(lease->executor) : NULL;
    if (state == NULL) {
        return;
    }
    int free_lease = 0;
    pthread_mutex_lock(&state->lock);
    uint32_t refs = atomic_fetch_sub_explicit(&lease->refs, 1, memory_order_acq_rel);
    if (refs == 1) {
        lease_unlink_locked(state, lease);
        free_lease = 1;
    }
    pthread_mutex_unlock(&state->lock);
    if (free_lease) {
        rt_free((uint8_t*)lease, sizeof(*lease), _Alignof(rt_far_task_lease));
    }
}

void rt_far_task_lease_release_route(rt_far_task_lease* lease) {
    if (lease == NULL) {
        return;
    }
    if (rt_remote_spawn_abandon_handle(&lease->handle)) {
        return;
    }
    if (lease->handle.task_id != 0 && lease->handle.generation != 0) {
        (void)rt_far_task_release(&lease->handle);
    }
}

rt_remote_spawn_status rt_far_task_handle_alloc(rt_far_task_handle** out_handle) {
    if (out_handle == NULL) {
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }
    *out_handle = NULL;
    rt_executor* ex = ensure_exec();
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (state == NULL) {
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }
    rt_far_task_lease* lease =
        (rt_far_task_lease*)rt_alloc(sizeof(*lease), _Alignof(rt_far_task_lease));
    if (lease == NULL) {
        return RT_REMOTE_SPAWN_STATUS_REFUSED;
    }
    memset(lease, 0, sizeof(*lease));
    lease->executor = ex;
    lease->holder = rt_current_task();
    atomic_store_explicit(&lease->state, RT_FAR_TASK_LEASE_OPEN, memory_order_relaxed);
    atomic_store_explicit(&lease->refs, 1, memory_order_relaxed);
    pthread_mutex_lock(&state->lock);
    lease->next = state->lease_head;
    state->lease_head = lease;
    pthread_mutex_unlock(&state->lock);
    *out_handle = &lease->handle;
    return RT_REMOTE_SPAWN_STATUS_OK;
}

void rt_far_task_handle_free(const rt_far_task_handle* handle) {
    rt_executor* ex = ensure_exec();
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (state == NULL || handle == NULL) {
        return;
    }
    int release_route = 0;
    pthread_mutex_lock(&state->lock);
    rt_far_task_lease* lease = rt_far_task_lease_find_locked(state, handle);
    if (lease != NULL) {
        uint8_t lease_state = atomic_load_explicit(&lease->state, memory_order_acquire);
        if (lease_state == RT_FAR_TASK_LEASE_OPEN || lease_state == RT_FAR_TASK_LEASE_RETURNING) {
            atomic_store_explicit(&lease->state, RT_FAR_TASK_LEASE_RELEASING, memory_order_release);
            lease->holder = NULL;
            release_route = 1;
        }
    }
    pthread_mutex_unlock(&state->lock);
    if (lease == NULL) {
        return;
    }
    if (release_route) {
        rt_far_task_lease_release_route(lease);
    }
    rt_far_task_lease_drop_ref(lease);
}

// A failed compare_exchange OVERWRITES `expected` with the value it actually
// found, so a second attempt from a different state cannot reuse the variable
// without resetting it. That reset used to live in a comma expression inside
// the `if` condition, which reads as a typo and which clang-tidy flags as
// bugprone-assignment-in-if-condition. Naming the transition says the same
// thing and each call gets its own `expected`.
static bool lease_state_transition(rt_far_task_lease* lease, uint8_t from, uint8_t to) {
    uint8_t expected = from;
    return atomic_compare_exchange_strong_explicit(
        &lease->state, &expected, to, memory_order_acq_rel, memory_order_acquire);
}

void rt_far_task_begin_transfer(const rt_far_task_handle* handle) {
    rt_remote_task_state* state = rt_remote_task_state_get(ensure_exec());
    if (state == NULL || handle == NULL) {
        return;
    }
    pthread_mutex_lock(&state->lock);
    rt_far_task_lease* lease = rt_far_task_lease_find_locked(state, handle);
    if (lease != NULL && lease->holder == rt_current_task() &&
        lease_state_transition(lease, RT_FAR_TASK_LEASE_OPEN, RT_FAR_TASK_LEASE_TRANSFERRING)) {
        lease->holder = NULL;
        (void)atomic_fetch_add_explicit(&lease->refs, 1, memory_order_relaxed);
    }
    pthread_mutex_unlock(&state->lock);
}

void rt_far_task_finish_transfer(const rt_far_task_handle* handle, void* child_task) {
    rt_remote_task_state* state = rt_remote_task_state_get(ensure_exec());
    if (state == NULL || handle == NULL) {
        return;
    }
    rt_task* child = task_from_handle(child_task);
    int release_route = 0;
    int drop_value_ref = 0;
    pthread_mutex_lock(&state->lock);
    rt_far_task_lease* lease = rt_far_task_lease_find_locked(state, handle);
    uint8_t lease_state =
        lease != NULL ? atomic_load_explicit(&lease->state, memory_order_acquire) : 0;
    if (lease != NULL && lease_state == RT_FAR_TASK_LEASE_TRANSFERRING) {
        if (child != NULL && task_status_load(child) != TASK_DONE &&
            task_cancelled_load(child) == 0) {
            lease->holder = child;
            atomic_store_explicit(&lease->state, RT_FAR_TASK_LEASE_OPEN, memory_order_release);
        } else {
            atomic_store_explicit(&lease->state, RT_FAR_TASK_LEASE_RELEASING, memory_order_release);
            release_route = 1;
            drop_value_ref = 1;
        }
    }
    pthread_mutex_unlock(&state->lock);
    if (lease == NULL) {
        return;
    }
    if (release_route) {
        rt_far_task_lease_release_route(lease);
    }
    if (drop_value_ref) {
        rt_far_task_lease_drop_ref(lease);
    }
    rt_far_task_lease_drop_ref(lease);
}

void rt_far_task_prepare_return(const rt_far_task_handle* handle) {
    rt_task* producer = rt_current_task();
    rt_remote_task_state* state = rt_remote_task_state_get(ensure_exec());
    if (state == NULL || producer == NULL || handle == NULL) {
        return;
    }
    pthread_mutex_lock(&state->lock);
    rt_far_task_lease* lease = rt_far_task_lease_find_locked(state, handle);
    // Either the producer still holds an OPEN lease, or the lease was handed to
    // it and sits at TRANSFERRING. Both become RETURNING; the second attempt
    // runs only if the first did not take it.
    if (lease != NULL &&
        ((lease->holder == producer &&
          lease_state_transition(lease, RT_FAR_TASK_LEASE_OPEN, RT_FAR_TASK_LEASE_RETURNING)) ||
         lease_state_transition(
             lease, RT_FAR_TASK_LEASE_TRANSFERRING, RT_FAR_TASK_LEASE_RETURNING))) {
        lease->holder = NULL;
        lease->result_owner = producer;
        atomic_store_explicit(&producer->far_task_result_lease, lease, memory_order_release);
        atomic_store_explicit(&producer->far_task_result_state, 1, memory_order_release);
    }
    pthread_mutex_unlock(&state->lock);
}

// holder == NULL selects every live lease (shutdown); otherwise only OPEN
// leases held by that task (task teardown).
static rt_far_task_lease* lease_next_releasable_locked(rt_remote_task_state* state,
                                                       const rt_task* holder) {
    for (rt_far_task_lease* lease = state->lease_head; lease != NULL; lease = lease->next) {
        uint8_t lease_state = atomic_load_explicit(&lease->state, memory_order_acquire);
        if (holder != NULL) {
            if (lease->holder == holder && lease_state == RT_FAR_TASK_LEASE_OPEN) {
                return lease;
            }
            continue;
        }
        if (lease_state == RT_FAR_TASK_LEASE_OPEN ||
            lease_state == RT_FAR_TASK_LEASE_TRANSFERRING ||
            lease_state == RT_FAR_TASK_LEASE_RETURNING) {
            return lease;
        }
    }
    return NULL;
}

static void release_matching_leases(rt_executor* ex, const rt_task* holder) {
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (state == NULL) {
        return;
    }
    for (;;) {
        pthread_mutex_lock(&state->lock);
        rt_far_task_lease* lease = lease_next_releasable_locked(state, holder);
        if (lease != NULL) {
            if (holder == NULL) {
                if (lease->result_owner != NULL) {
                    atomic_store_explicit(
                        &lease->result_owner->far_task_result_state, 2, memory_order_release);
                    (void)atomic_exchange_explicit(
                        &lease->result_owner->far_task_result_lease, NULL, memory_order_acq_rel);
                }
                lease->result_owner = NULL;
            }
            lease->holder = NULL;
            atomic_store_explicit(&lease->state, RT_FAR_TASK_LEASE_RELEASING, memory_order_release);
        }
        pthread_mutex_unlock(&state->lock);
        if (lease == NULL) {
            return;
        }
        rt_far_task_lease_release_route(lease);
        rt_far_task_lease_drop_ref(lease);
    }
}

void rt_far_task_release_owned(rt_executor* ex, const rt_task* holder) {
    if (holder != NULL) {
        release_matching_leases(ex, holder);
    }
}

void rt_far_task_release_all(rt_executor* ex) {
    release_matching_leases(ex, NULL);
}
