#include "rt_async_internal.h"

// Shard-lane worker wake path (Epic 7 Task 7, spike D5/D6 applied to worker
// sleep). Wake producers bump wake_pending under the target shard's lock
// before signaling; sleepers re-check wake_pending under the same lock
// before waiting, so a wake between the control-lane "no work" decision and
// the shard cond_wait is never lost. Every caller here holds the control
// lock; the shard lock nests beneath it per the D2 order.

void rt_sched_wake_signal_shard_n(rt_shard* shard, uint32_t tokens) {
    if (shard == NULL || tokens == 0) {
        return;
    }
    rt_shard_lock(shard);
    rt_scheduler* scheduler = rt_shard_scheduler(shard);
    if (scheduler != NULL) {
        if (scheduler->wake_pending > UINT32_MAX - tokens) {
            scheduler->wake_pending = UINT32_MAX;
        } else {
            scheduler->wake_pending += tokens;
        }
    }
    if (tokens > 1) {
        pthread_cond_broadcast(&shard->worker_cv);
    } else {
        pthread_cond_signal(&shard->worker_cv);
    }
    rt_shard_unlock(shard);
}

void rt_sched_wake_broadcast_all(rt_executor* ex) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    size_t count = rt_runtime_shard_count(runtime);
    for (size_t i = 0; i < count; i++) {
        rt_shard* shard = rt_runtime_shard(runtime, i);
        rt_scheduler* scheduler = rt_shard_scheduler(shard);
        uint32_t sleepers = scheduler != NULL ? scheduler->worker_count : 1U;
        // Give every potential sleeper a token so none re-sleeps on a stale
        // zero after a shutdown or compat broadcast.
        rt_sched_wake_signal_shard_n(shard, sleepers > 0 ? sleepers : 1U);
    }
}

// Release the control lock and sleep on the shard worker cv until a wake
// token or shutdown arrives; reacquires the control lock before returning.
// The caller re-scans its queues after this returns: a leftover token costs
// one extra scan, a missing token is impossible (see file header).
void rt_sched_worker_sleep(rt_executor* ex, rt_shard* shard) {
    if (ex == NULL || shard == NULL) {
        return;
    }
    rt_scheduler* scheduler = rt_shard_scheduler(shard);
    rt_control_unlock(ex);
    rt_shard_lock(shard);
    while (scheduler != NULL && scheduler->wake_pending == 0 && !ex->shutdown) {
        pthread_cond_wait(&shard->worker_cv, &shard->lock);
    }
    if (scheduler != NULL && scheduler->wake_pending > 0) {
        scheduler->wake_pending--;
    }
    rt_shard_unlock(shard);
    rt_control_lock(ex);
}
