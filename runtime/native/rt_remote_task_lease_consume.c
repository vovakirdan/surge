#include "rt_remote_task_internal.h"

rt_remote_task_status rt_far_task_lease_consume(const rt_far_task_handle* handle) {
    rt_remote_task_state* state = rt_remote_task_state_get(ensure_exec());
    if (state == NULL || handle == NULL) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    pthread_mutex_lock(&state->lock);
    rt_far_task_lease* lease = rt_far_task_lease_find_locked(state, handle);
    if (lease == NULL) {
        pthread_mutex_unlock(&state->lock);
        return RT_REMOTE_TASK_STATUS_OK;
    }
    uint8_t expected = RT_FAR_TASK_LEASE_OPEN;
    if (lease->holder != rt_current_task() ||
        !atomic_compare_exchange_strong_explicit(&lease->state,
                                                 &expected,
                                                 RT_FAR_TASK_LEASE_CONSUMED,
                                                 memory_order_acq_rel,
                                                 memory_order_acquire)) {
        expected = RT_FAR_TASK_LEASE_TRANSFERRING;
        if (!atomic_compare_exchange_strong_explicit(&lease->state,
                                                     &expected,
                                                     RT_FAR_TASK_LEASE_CONSUMED,
                                                     memory_order_acq_rel,
                                                     memory_order_acquire)) {
            pthread_mutex_unlock(&state->lock);
            return RT_REMOTE_TASK_STATUS_CONSUMED;
        }
    }
    lease->holder = NULL;
    pthread_mutex_unlock(&state->lock);
    return RT_REMOTE_TASK_STATUS_OK;
}

void rt_far_task_lease_restore(const rt_far_task_handle* handle) {
    rt_remote_task_state* state = rt_remote_task_state_get(ensure_exec());
    if (state == NULL || handle == NULL) {
        return;
    }
    pthread_mutex_lock(&state->lock);
    rt_far_task_lease* lease = rt_far_task_lease_find_locked(state, handle);
    uint8_t expected = RT_FAR_TASK_LEASE_CONSUMED;
    if (lease != NULL && atomic_compare_exchange_strong_explicit(&lease->state,
                                                                 &expected,
                                                                 RT_FAR_TASK_LEASE_OPEN,
                                                                 memory_order_acq_rel,
                                                                 memory_order_acquire)) {
        lease->holder = rt_current_task();
    }
    pthread_mutex_unlock(&state->lock);
}
