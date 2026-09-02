#include "rt_remote_task_internal.h"
#include "rt_value_cell.h"

#include <string.h>

rt_remote_task_state* rt_remote_task_state_get(rt_executor* ex) {
    return ex != NULL ? ex->remote_tasks : NULL;
}

rt_runtime_status rt_remote_task_state_init(rt_executor* ex) {
    if (ex == NULL || ex->remote_tasks != NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    rt_remote_task_state* state =
        (rt_remote_task_state*)rt_alloc(sizeof(*state), _Alignof(rt_remote_task_state));
    if (state == NULL) {
        return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
    }
    memset(state, 0, sizeof(*state));
    state->executor = ex;
    atomic_store_explicit(&state->next_request_id, 1, memory_order_relaxed);
    if (pthread_mutex_init(&state->lock, NULL) != 0) {
        rt_free((uint8_t*)state, sizeof(*state), _Alignof(rt_remote_task_state));
        return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
    }
    ex->remote_tasks = state;
    return RT_RUNTIME_STATUS_OK;
}

rt_runtime_status rt_remote_task_state_destroy(rt_executor* ex) {
    if (ex == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    rt_remote_task_state* state = ex->remote_tasks;
    if (state == NULL) {
        return RT_RUNTIME_STATUS_OK;
    }
    pthread_mutex_lock(&state->lock);
    if (state->pending_head != NULL || state->lease_head != NULL) {
        pthread_mutex_unlock(&state->lock);
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    ex->remote_tasks = NULL;
    pthread_mutex_unlock(&state->lock);
    pthread_mutex_destroy(&state->lock);
    rt_free((uint8_t*)state, sizeof(*state), _Alignof(rt_remote_task_state));
    return RT_RUNTIME_STATUS_OK;
}

static void pending_link_locked(rt_remote_task_state* state, rt_remote_task_pending* pending) {
    pending->next = state->pending_head;
    pending->listed = 1;
    state->pending_head = pending;
}

static void pending_unlink_locked(rt_remote_task_state* state, rt_remote_task_pending* pending) {
    rt_remote_task_pending** cursor = &state->pending_head;
    while (*cursor != NULL) {
        if (*cursor == pending) {
            *cursor = pending->next;
            pending->next = NULL;
            pending->listed = 0;
            return;
        }
        cursor = &(*cursor)->next;
    }
}

rt_remote_task_pending* rt_remote_task_pending_new(rt_executor* ex,
                                                   const rt_far_task_handle* handle,
                                                   uint32_t source_shard_id,
                                                   rt_remote_task_op op,
                                                   int listed) {
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (state == NULL || state->executor != ex || handle == NULL) {
        return NULL;
    }
    rt_remote_task_pending* pending =
        (rt_remote_task_pending*)rt_alloc(sizeof(*pending), _Alignof(rt_remote_task_pending));
    if (pending == NULL) {
        return NULL;
    }
    memset(pending, 0, sizeof(*pending));
    pending->executor = ex;
    pending->request_id =
        atomic_fetch_add_explicit(&state->next_request_id, 1, memory_order_relaxed);
    pending->handle = *handle;
    pending->source_shard_id = source_shard_id;
    pending->op = (uint8_t)op;
    pending->status = RT_REMOTE_TASK_STATUS_PENDING;
    pending->reply_status = RT_REMOTE_TASK_STATUS_PENDING;
    pending->result_kind = 2;
    pending->select_committed_index = RT_FAR_CHANNEL_SELECT_NO_COMMIT;
    atomic_store_explicit(&pending->refs, 1, memory_order_relaxed);
    if (listed) {
        pthread_mutex_lock(&state->lock);
        pending_link_locked(state, pending);
        pthread_mutex_unlock(&state->lock);
    }
    return pending;
}

void rt_remote_task_pending_add_ref(rt_remote_task_pending* pending) {
    if (pending != NULL) {
        (void)atomic_fetch_add_explicit(&pending->refs, 1, memory_order_relaxed);
    }
}

void rt_remote_task_pending_release(rt_remote_task_pending* pending) {
    if (pending == NULL) {
        return;
    }
    uint32_t refs = atomic_fetch_sub_explicit(&pending->refs, 1, memory_order_acq_rel);
    if (refs == 1) {
        if (pending->state_owned != 0 && pending->state_type_id != 0 &&
            pending->body_state != NULL) {
            rt_value_release_owned_block(rt_channel_element_ops_for(pending->state_type_id),
                                         pending->body_state);
            pending->body_state = NULL;
        }
        // A landed reply whose capability nobody spent: the caller tore down
        // before its next poll, or resolved through another path. Release the
        // pin so the producer can be freed; what its slot still holds is
        // destroyed by the producer's own dispose, exactly once, there.
        if (pending->result_source.task_id != 0) {
            rt_remote_task_release_result_source(pending->executor, &pending->result_source);
            pending->result_source = (rt_result_source){0, 0, 0, 0};
        }
        if (pending->select_arms != NULL) {
            // Every SEND arm's payload is still owned here except the ones
            // already moved out: the winner, which the destination's select
            // took, and any the terminal retry handed back. Each of those left
            // its cell MOVED, so disposing every cell destroys exactly what is
            // still owned and nothing else -- the arm's own state answers the
            // question that select_committed_index used to have to answer from
            // outside.
            for (uint64_t i = 0; i < pending->select_count; i++) {
                rt_value_cell_dispose(&pending->select_arms[i].payload);
            }
            rt_free((uint8_t*)pending->select_arms,
                    pending->select_count * sizeof(rt_far_channel_select_arm),
                    _Alignof(rt_far_channel_select_arm));
        }
        rt_free((uint8_t*)pending, sizeof(*pending), _Alignof(rt_remote_task_pending));
    }
}

void rt_remote_task_pending_consume(rt_remote_task_pending* pending) {
    rt_remote_task_state* state =
        pending != NULL ? rt_remote_task_state_get(pending->executor) : NULL;
    if (state == NULL) {
        rt_remote_task_pending_release(pending);
        return;
    }
    pthread_mutex_lock(&state->lock);
    if (pending->listed != 0) {
        pending_unlink_locked(state, pending);
    }
    pthread_mutex_unlock(&state->lock);
    rt_remote_task_pending_release(pending);
}

rt_remote_task_status rt_remote_task_pending_snapshot(const rt_remote_task_pending* pending,
                                                      uint8_t* out_kind,
                                                      uint64_t* out_bits) {
    rt_remote_task_state* state =
        pending != NULL ? rt_remote_task_state_get(pending->executor) : NULL;
    if (state == NULL) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    pthread_mutex_lock(&state->lock);
    rt_remote_task_status status = (rt_remote_task_status)pending->status;
    if (out_kind != NULL) {
        *out_kind = pending->result_kind;
    }
    if (out_bits != NULL) {
        *out_bits = pending->result_bits;
    }
    pthread_mutex_unlock(&state->lock);
    return status;
}

void rt_remote_task_pending_set_reply(rt_remote_task_pending* pending,
                                      rt_remote_task_status status,
                                      uint8_t result_kind,
                                      uint64_t result_bits,
                                      const rt_result_source* result_source) {
    rt_remote_task_state* state =
        pending != NULL ? rt_remote_task_state_get(pending->executor) : NULL;
    if (state == NULL) {
        return;
    }
    pthread_mutex_lock(&state->lock);
    pending->reply_status = (uint8_t)status;
    pending->result_kind = result_kind;
    pending->result_bits = result_bits;
    if (result_source != NULL) {
        pending->result_source = *result_source;
    }
    pthread_mutex_unlock(&state->lock);
}

waker_key rt_remote_task_reply_key(uint64_t request_id, uint32_t source_shard_id) {
    waker_key key = {WAKER_REMOTE_TASK_REPLY, request_id, source_shard_id};
    return key;
}

static int reply_wait_retire_locked(rt_remote_task_pending* pending) {
    if (pending->reply_wait_retired != 0) {
        return 0;
    }
    pending->reply_wait_retired = 1;
    return 1;
}

void rt_remote_task_pending_retire_reply_wait(rt_executor* ex, rt_remote_task_pending* pending) {
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (pending == NULL || state == NULL || pending->executor != ex) {
        return;
    }
    int should_wake = 0;
    pthread_mutex_lock(&state->lock);
    should_wake = reply_wait_retire_locked(pending);
    pthread_mutex_unlock(&state->lock);
#ifndef RV2_SEQ0_TERMINAL_RETIRE_NEGATIVE_CONTROL
    if (should_wake) {
        // Both key fields are immutable from pending_new until final release;
        // request ids are monotonic, so retirement cannot consume a later
        // operation's registration even after the caller task is reused.
        wake_key_all_with_policy(
            ex, rt_remote_task_reply_key(pending->request_id, pending->source_shard_id), 0);
    }
#else
    (void)should_wake;
#endif
}

void rt_remote_task_pending_finish(rt_executor* ex,
                                   rt_remote_task_pending* pending,
                                   rt_remote_task_status status,
                                   uint8_t result_kind,
                                   uint64_t result_bits,
                                   const rt_result_source* result_source) {
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (pending == NULL || state == NULL || pending->executor != ex) {
        return;
    }
    int should_wake = 0;
    pthread_mutex_lock(&state->lock);
    if (pending->status == RT_REMOTE_TASK_STATUS_PENDING) {
        pending->status = (uint8_t)status;
        pending->result_kind = result_kind;
        pending->result_bits = result_bits;
        if (result_source != NULL) {
            pending->result_source = *result_source;
        }
        pending->owner_registered = 0;
        should_wake = reply_wait_retire_locked(pending);
    }
    pthread_mutex_unlock(&state->lock);
    if (should_wake) {
        wake_key_all_with_policy(
            ex, rt_remote_task_reply_key(pending->request_id, pending->source_shard_id), 0);
    }
}

rt_result_source rt_remote_task_pending_result_source(const rt_remote_task_pending* pending) {
    rt_result_source source = {0, 0, 0, 0};
    rt_remote_task_state* state =
        pending != NULL ? rt_remote_task_state_get(pending->executor) : NULL;
    if (state == NULL) {
        return source;
    }
    pthread_mutex_lock(&state->lock);
    source = pending->result_source;
    pthread_mutex_unlock(&state->lock);
    return source;
}

void rt_remote_task_pending_clear_result_source(rt_remote_task_pending* pending) {
    rt_remote_task_state* state =
        pending != NULL ? rt_remote_task_state_get(pending->executor) : NULL;
    if (state == NULL) {
        return;
    }
    pthread_mutex_lock(&state->lock);
    pending->result_source = (rt_result_source){0, 0, 0, 0};
    pthread_mutex_unlock(&state->lock);
}

void rt_remote_task_pending_set_owner_registered(rt_remote_task_pending* pending, int value) {
    rt_remote_task_state* state =
        pending != NULL ? rt_remote_task_state_get(pending->executor) : NULL;
    if (state == NULL) {
        return;
    }
    pthread_mutex_lock(&state->lock);
    pending->owner_registered = value != 0;
    pthread_mutex_unlock(&state->lock);
}
