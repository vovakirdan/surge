#include "rt_remote_task_internal.h"

// Claims the producer's returned-result lease under the state lock: flips
// far_task_result_state 1 -> 2 and detaches far_task_result_lease. Returns
// the detached lease, or NULL when another path already claimed it.
static rt_far_task_lease* result_lease_take_locked(rt_task* producer) {
    uint8_t expected = 1;
    if (!atomic_compare_exchange_strong_explicit(&producer->far_task_result_state,
                                                 &expected,
                                                 2,
                                                 memory_order_acq_rel,
                                                 memory_order_acquire)) {
        return NULL;
    }
    return atomic_exchange_explicit(&producer->far_task_result_lease, NULL, memory_order_acq_rel);
}

int rt_far_task_adopt_result(rt_task* producer, rt_task* holder) {
    if (producer == NULL) {
        return 1;
    }
    rt_far_task_lease* lease =
        atomic_load_explicit(&producer->far_task_result_lease, memory_order_acquire);
    if (lease == NULL) {
        return atomic_load_explicit(&producer->far_task_result_state, memory_order_acquire) == 0;
    }
    rt_remote_task_state* state = rt_remote_task_state_get(lease->executor);
    if (state == NULL) {
        return 0;
    }
    pthread_mutex_lock(&state->lock);
    rt_far_task_lease* taken = result_lease_take_locked(producer);
    if (taken == lease) {
        lease->result_owner = NULL;
        lease->holder = holder;
        atomic_store_explicit(&lease->state, RT_FAR_TASK_LEASE_OPEN, memory_order_release);
    }
    pthread_mutex_unlock(&state->lock);
    return taken == lease;
}

void rt_far_task_release_result(rt_executor* ex, rt_task* producer) {
    (void)ex;
    if (producer == NULL) {
        return;
    }
    rt_far_task_lease* lease =
        atomic_load_explicit(&producer->far_task_result_lease, memory_order_acquire);
    rt_remote_task_state* state = lease != NULL ? rt_remote_task_state_get(lease->executor) : NULL;
    if (state == NULL) {
        return;
    }
    pthread_mutex_lock(&state->lock);
    rt_far_task_lease* taken = result_lease_take_locked(producer);
    if (taken == lease) {
        lease->result_owner = NULL;
        atomic_store_explicit(&lease->state, RT_FAR_TASK_LEASE_RELEASING, memory_order_release);
    }
    pthread_mutex_unlock(&state->lock);
    if (taken == lease) {
        rt_far_task_lease_release_route(lease);
        rt_far_task_lease_drop_ref(lease);
    }
}

// Reports whether the result came back from another shard, which carries it in
// a lease exactly one holder may adopt. That lease is its own answer to "who
// gets this", and it answers a second asker with "gone" rather than by
// reclaiming under the first, so the copy-per-asker path leaves it alone.
static int result_is_far_carried(const rt_task* producer) {
    return atomic_load_explicit(&producer->far_task_result_lease, memory_order_acquire) != NULL ||
           atomic_load_explicit(&producer->far_task_result_state, memory_order_acquire) != 0;
}

uint8_t rt_far_task_take_result(rt_task* producer, rt_task* holder, uint64_t* out_bits) {
    if (producer == NULL) {
        if (out_bits != NULL) {
            *out_bits = 0;
        }
        return 2;
    }
    uint8_t kind = rt_remote_task_result_kind(producer);
    if (kind == 1 && producer->result_copy_fn != NULL && !result_is_far_carried(producer)) {
        // More than one handle can ask for this result, so it stays the task's
        // and this asker gets a copy of its own. Nothing here reclaims the
        // original: a later asker still has to be able to read it, and
        // free_task discharges it once the last handle is gone.
        if (out_bits != NULL) {
            *out_bits = (uint64_t)(uintptr_t)producer->result_copy_fn(
                (void*)(uintptr_t)producer->result_bits);
        }
        return kind;
    }
    if (kind == 1 && !rt_far_task_adopt_result(producer, holder)) {
        kind = 2;
    }
    if (kind == 1) {
        // Local consume: result_bits is handed to the holder, which now owns
        // it and drops it at scope exit. Clear the owner-side obligation so
        // free_task does not double-free (RV2-DEBT-053a).
        producer->result_drop_fn_id = 0;
    }
    if (out_bits != NULL) {
        *out_bits = kind == 1 ? producer->result_bits : 0;
    }
    return kind;
}
