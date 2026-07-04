#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_async_internal.h"

#include <time.h>

// Sync-channel blocking compatibility lane (Epic 8 extraction): the parked
// OS-worker wait, its self-refreshing compat_cv slice, the local-queue
// handoff to inject, and compensation worker spawn. This is the deprecated
// thread-blocking path for sync channel helpers; the async fast lanes never
// enter it. See RV2-DEBT-017 for its recorded latency envelope.

void maybe_start_compensation_worker_locked(rt_executor* ex) {
    rt_scheduler* scheduler = current_worker_scheduler(ex);
    if (scheduler == NULL) {
        scheduler = rt_executor_scheduler(ex);
    }
    if (ex == NULL || scheduler == NULL || scheduler->worker_count <= 1) {
        return;
    }
    rt_channel_blocking_compat* compat = rt_executor_channel_blocking_compat(ex);
    if (compat == NULL) {
        return;
    }
    uint32_t total_workers = scheduler->worker_count + compat->compensation_count;
    if (compat->channel_blocked_workers < total_workers) {
        return;
    }
    // Stateful channel fanout can park many workers behind sync request/reply chains.
    uint32_t limit =
        scheduler->worker_count > UINT32_MAX / 32U ? UINT32_MAX : scheduler->worker_count * 32U;
    if (compat->compensation_count >= limit) {
        return;
    }
    // Compensation workers live until executor shutdown; their context is process-lifetime.
    rt_worker_ctx* ctx = (rt_worker_ctx*)rt_alloc(sizeof(rt_worker_ctx), _Alignof(rt_worker_ctx));
    if (ctx == NULL) {
        panic_msg("async: compensation worker context allocation failed");
        return;
    }
    memset(ctx, 0, sizeof(rt_worker_ctx));
    ctx->ex = ex;
    ctx->shard = tls_worker_ctx != NULL && tls_worker_ctx->ex == ex
                     ? tls_worker_ctx->shard
                     : rt_runtime_shard0(ex->runtime);
    ctx->scheduler = scheduler;
    ctx->heap_cell = rt_heap_accounting_compensation_cell(rt_executor_heap_accounting(ex),
                                                          compat->compensation_count);
    ctx->shard_id = ctx->shard != NULL ? ctx->shard->shard_id : 0;
    ctx->worker_id = compat->compensation_count % scheduler->worker_count;
    ctx->worker_index = UINT32_MAX;
    ctx->sched_rng = scheduler->sched_seed +
                     UINT64_C(0x9e3779b97f4a7c15) *
                         (uint64_t)(scheduler->worker_count + compat->compensation_count + 1U);

    pthread_t thread;
    if (pthread_create(&thread, NULL, rt_worker_main, ctx) != 0) {
        rt_free((uint8_t*)ctx, sizeof(rt_worker_ctx), _Alignof(rt_worker_ctx));
        panic_msg("async: compensation worker start failed");
        return;
    }
    (void)pthread_detach(thread);
    rt_trace_compensation_started();
    compat->compensation_count++;
    if (compat->compensation_count > compat->compensation_high_water) {
        compat->compensation_high_water = compat->compensation_count;
    }
}

static void move_current_local_to_inject_locked(const rt_executor* ex) {
    rt_scheduler* scheduler = current_worker_scheduler(ex);
    if (scheduler == NULL || tls_worker_ctx == NULL ||
        tls_worker_ctx->worker_id >= scheduler->worker_count || scheduler->local_queues == NULL) {
        return;
    }
    // Queue moves, the wake-token bump, and the broadcast share one shard
    // lock hold; the caller holds the control lock and no shard lock.
    rt_shard* own_shard = tls_worker_ctx->shard;
    rt_shard_lock(own_shard);
    rt_deque* local = &scheduler->local_queues[tls_worker_ctx->worker_id];
    uint64_t id = 0;
    uint32_t moved = 0;
    while (deque_pop_head(local, &id)) {
        if (deque_push_tail(&scheduler->inject,
                            id,
                            "async: inject queue overflow",
                            "async: inject queue allocation failed")) {
            moved++;
        }
    }
    if (moved > 0) {
        if (scheduler->wake_pending > UINT32_MAX - moved) {
            scheduler->wake_pending = UINT32_MAX;
        } else {
            scheduler->wake_pending += moved;
        }
        pthread_cond_broadcast(&own_shard->worker_cv);
    }
    rt_shard_unlock(own_shard);
}

static void compat_cv_timedwait_slice(rt_executor* ex) {
    // Self-refreshing wait: not every ready push can reach a compat-parked
    // worker (a wake into a shard whose workers all sit here signals only
    // that shard's worker_cv), so the wait re-checks for drainable work at
    // least every slice instead of relying on compat_cv alone.
    struct timespec deadline;
    clock_gettime(CLOCK_REALTIME, &deadline);
    deadline.tv_nsec += 10L * 1000L * 1000L;
    if (deadline.tv_nsec >= 1000000000L) {
        deadline.tv_sec += 1;
        deadline.tv_nsec -= 1000000000L;
    }
    (void)pthread_cond_timedwait(&ex->compat_cv, &ex->lock, &deadline);
}

int rt_wait_current_worker_wakeup(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL || tls_worker_id < 0) {
        return 0;
    }
    rt_scheduler* scheduler = current_worker_scheduler(ex);
    if (scheduler == NULL) {
        return 0;
    }
    rt_channel_blocking_compat* compat = rt_executor_channel_blocking_compat(ex);
    if (compat == NULL) {
        return 0;
    }
    rt_trace_channel_blocking_wait();
    rt_control_lock(ex);
    move_current_local_to_inject_locked(ex);
    // This sync helper parks the OS worker, so it stops contributing to scheduler progress.
    atomic_fetch_add_explicit(&compat->channel_blocked_workers, 1, memory_order_acq_rel);
    int dropped_running = 0;
    rt_shard* own_shard = tls_worker_ctx != NULL ? tls_worker_ctx->shard : NULL;
    if (own_shard != NULL) {
        rt_shard_lock(own_shard);
        if (scheduler->running_count > 0) {
            scheduler->running_count--;
            dropped_running = 1;
        }
        rt_shard_unlock(own_shard);
    }
    maybe_start_compensation_worker_locked(ex);
    while (!ex->shutdown && task->resume_kind == RESUME_NONE &&
           task_wake_token_exchange(task, 0) == 0) {
        compat_cv_timedwait_slice(ex);
    }
    if (dropped_running && own_shard != NULL) {
        rt_shard_lock(own_shard);
        scheduler->running_count++;
        rt_shard_unlock(own_shard);
    }
    if (atomic_load_explicit(&compat->channel_blocked_workers, memory_order_acquire) > 0) {
        atomic_fetch_sub_explicit(&compat->channel_blocked_workers, 1, memory_order_acq_rel);
    }
    rt_control_unlock(ex);
    return 1;
}
