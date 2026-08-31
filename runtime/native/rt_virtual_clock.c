#include "rt_async_internal.h"

// Yield tick (D7): advance the clock, fire the ticking shard's own due
// sleepers inline, and hand other shards a wake token when their atomic
// min-deadline mirror says they have due work — the owner pops its own
// store on its next scheduler turn.
void tick_virtual(rt_executor* ex) {
    if (ex == NULL) {
        return;
    }
    uint64_t now = rt_clock_tick(ex);
    rt_runtime* runtime = rt_executor_runtime(ex);
    rt_shard* own = tls_worker_ctx != NULL && tls_worker_ctx->ex == ex ? tls_worker_ctx->shard
                                                                       : rt_runtime_shard0(runtime);
    size_t shard_count = rt_runtime_shard_count(runtime);
    for (size_t i = 0; i < shard_count; i++) {
        rt_shard* shard = rt_runtime_shard(runtime, i);
        if (shard == NULL || rt_sleep_store_min(&shard->sleep_store) > now) {
            continue;
        }
        if (shard == own) {
            (void)rt_sleep_fire_due_on_shard(ex, shard, now);
        } else {
            rt_sched_wake_signal_shard_n(shard, 1);
        }
    }
}

int rt_next_sleep_deadline(const rt_executor* ex, uint64_t* out_deadline) {
    const rt_runtime* runtime = ex != NULL ? ex->runtime : NULL;
    size_t shard_count = rt_runtime_shard_count(runtime);
    uint64_t next_deadline = UINT64_MAX;
    for (size_t i = 0; i < shard_count; i++) {
        const rt_shard* shard = rt_runtime_shard_const(runtime, i);
        uint64_t min = shard != NULL ? rt_sleep_store_min(&shard->sleep_store) : UINT64_MAX;
        if (min < next_deadline) {
            next_deadline = min;
        }
    }
    if (next_deadline == UINT64_MAX) {
        return 0;
    }
    if (out_deadline != NULL) {
        *out_deadline = next_deadline;
    }
    return 1;
}

int advance_time_to_next_timer(rt_executor* ex) {
    if (ex == NULL) {
        return 0;
    }
    // Wall-bounded: refuses while a socket waiter is registered and the wall
    // has not reached the deadline yet, so the caller waits instead of
    // inventing the time.
    if (!rt_clock_advance_to_next_deadline(ex)) {
        return 0;
    }
    uint64_t now = rt_clock_now(ex);
    rt_runtime* runtime = rt_executor_runtime(ex);
    size_t shard_count = rt_runtime_shard_count(runtime);
    for (size_t i = 0; i < shard_count; i++) {
        (void)rt_sleep_fire_due_on_shard(ex, rt_runtime_shard(runtime, i), now);
    }
    return 1;
}
