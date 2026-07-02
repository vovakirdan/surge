#include "rt_async_internal.h"

rt_runtime_status rt_executor_drain_shutdown_net_waiters(rt_executor* ex) {
    if (ex == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    rt_lock(ex);
    (void)rt_fd_registry_drain_shutdown_net_waiters_locked(ex, rt_executor_fd_registry(ex));
    rt_unlock(ex);
    return RT_RUNTIME_STATUS_OK;
}

rt_runtime_status rt_executor_request_shutdown(rt_executor* ex) {
    if (ex == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    rt_lock(ex);
    ex->shutdown = 1;
    (void)rt_fd_registry_drain_shutdown_net_waiters_locked(ex, rt_executor_fd_registry(ex));
    rt_net_wake_poll();
    pthread_cond_broadcast(&ex->io_cv);
    pthread_cond_broadcast(&ex->ready_cv);
    pthread_cond_broadcast(&ex->done_cv);
    rt_unlock(ex);

    if (ex->blocking_started) {
        pthread_mutex_lock(&ex->blocking_lock);
        ex->blocking_shutdown = 1;
        pthread_cond_broadcast(&ex->blocking_cv);
        pthread_mutex_unlock(&ex->blocking_lock);
    }
    return RT_RUNTIME_STATUS_OK;
}
