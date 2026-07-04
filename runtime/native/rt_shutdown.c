#include "rt_async_internal.h"
#include "rt_net_trace.h"

rt_runtime_status rt_executor_drain_shutdown_net_waiters(rt_executor* ex) {
    if (ex == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    rt_control_lock(ex);
    size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
    for (size_t i = 0; i < shard_count; i++) {
        (void)rt_fd_registry_drain_shutdown_net_waiters_locked_on_owner(
            ex, rt_executor_fd_registry_for_shard(ex, i), (uint32_t)i);
    }
    rt_control_unlock(ex);
    return RT_RUNTIME_STATUS_OK;
}

rt_runtime_status rt_executor_request_shutdown(rt_executor* ex) {
    if (ex == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    rt_control_lock(ex);
    ex->shutdown = 1;
    size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
    for (size_t i = 0; i < shard_count; i++) {
        (void)rt_fd_registry_drain_shutdown_net_waiters_locked_on_owner(
            ex, rt_executor_fd_registry_for_shard(ex, i), (uint32_t)i);
    }
    uint64_t pollers_woken = rt_net_wake_poll_all_shards(ex);
    rt_net_trace_shutdown_poller_wakeups(pollers_woken);
    rt_io_poll_nudge(ex);
    rt_sched_wake_broadcast_all(ex);
    pthread_cond_broadcast(&ex->compat_cv);
    pthread_cond_broadcast(&ex->done_cv);
    rt_control_unlock(ex);

    if (ex->blocking_started) {
        pthread_mutex_lock(&ex->blocking_lock);
        ex->blocking_shutdown = 1;
        pthread_cond_broadcast(&ex->blocking_cv);
        pthread_mutex_unlock(&ex->blocking_lock);
    }
    return RT_RUNTIME_STATUS_OK;
}
