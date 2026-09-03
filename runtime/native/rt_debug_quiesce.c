#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_async_internal.h"

#include <sched.h>
#include <time.h>

// rt_debug_quiesce waits until the executor is at rest around the caller:
// no task other than the caller's own is running on any shard, no shard holds
// runnable work (inject or local deques), no shard is mid-publication, no
// inbound envelope is undrained, and the blocking pool is empty and idle. It
// returns how many samples it took to see that, so a reader can tell a
// barrier that had to wait from one that did not.
//
// It exists for an observer that reads a process-wide counter and wants the
// number to be about the work it just awaited: an awaited completion is
// visible to the awaiter while the completing carrier is still inside its
// turn, and a count taken then can miss that carrier's last allocation or
// include the next batch's first (RV2-DEBT-330, the paired benchmark's exact
// structural budget). The wait is bounded and fails closed: an executor that
// does not settle within the budget is reported, never waited for
// indefinitely, so a wedge reads as a red rather than a hang.
//
// Sleep and net waiters are deliberately NOT part of "at rest": a parked task
// is not running and allocates nothing, and this is a barrier for a
// benchmark observer, not the deadlock detector's notion of quiescence
// (rt_remote_task_deadlock.c), which asks a different question.

#define RT_DEBUG_QUIESCE_BUDGET_NS (2ull * 1000ull * 1000ull * 1000ull)

static uint64_t debug_quiesce_now_ns(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000000000ull + (uint64_t)ts.tv_nsec;
}

// Under the shard's lock. own_running is the number of running tasks this
// shard may hold on the caller's account: one when the caller is a task
// owned by it, else zero. The caller itself may or may not be in
// running_count (a poll driven from the main thread is not), so the test is
// "no running task beyond the caller", not "exactly the caller".
static int debug_quiesce_shard_at_rest_locked(const rt_shard* shard, uint32_t own_running) {
    const rt_scheduler* scheduler = rt_shard_scheduler_const(shard);
    if (scheduler == NULL) {
        return 1;
    }
    if (scheduler->running_count > own_running || scheduler->publishing_count != 0) {
        return 0;
    }
    if (scheduler->inject.len != 0) {
        return 0;
    }
    if (scheduler->local_queues != NULL) {
        for (uint32_t i = 0; i < scheduler->worker_count; i++) {
            if (scheduler->local_queues[i].len != 0) {
                return 0;
            }
        }
    }
    if (rt_transport_inbound_len_locked(shard) != 0) {
        return 0;
    }
    return 1;
}

static int debug_quiesce_at_rest(rt_executor* ex, const rt_shard* own_shard) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    size_t shard_count = rt_runtime_shard_count(runtime);
    for (size_t i = 0; i < shard_count; i++) {
        rt_shard* shard = rt_runtime_shard(runtime, i);
        if (shard == NULL) {
            continue;
        }
        rt_shard_lock(shard);
        int at_rest = debug_quiesce_shard_at_rest_locked(shard, shard == own_shard ? 1u : 0u);
        rt_shard_unlock(shard);
        if (!at_rest) {
            return 0;
        }
    }
    pthread_mutex_lock(&ex->blocking_lock);
    int blocking_empty = ex->blocking_head == NULL;
    pthread_mutex_unlock(&ex->blocking_lock);
    if (!blocking_empty || atomic_load_explicit(&ex->blocking_running, memory_order_acquire) != 0) {
        return 0;
    }
    return 1;
}

uint64_t rt_debug_quiesce(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return 0;
    }
    if (rt_lane_holds_any_shard() || rt_lane_holds_control()) {
        rt_fatal_static(RT_FATAL_PANIC, (const uint8_t*)"rt_debug_quiesce: called under a lane lock",
                        sizeof("rt_debug_quiesce: called under a lane lock") - 1);
        return 0;
    }
    const rt_task* self = rt_current_task();
    const rt_shard* own_shard = self != NULL ? rt_task_owner_shard(ex, self) : NULL;
    uint64_t started = debug_quiesce_now_ns();
    uint64_t samples = 0;
    for (;;) {
        samples++;
        if (debug_quiesce_at_rest(ex, own_shard)) {
            return samples;
        }
        if (debug_quiesce_now_ns() - started > RT_DEBUG_QUIESCE_BUDGET_NS) {
            rt_fatal_static(RT_FATAL_PANIC,
                            (const uint8_t*)"rt_debug_quiesce: the executor did not come to rest within 2 s",
                            sizeof("rt_debug_quiesce: the executor did not come to rest within 2 s") - 1);
            return samples;
        }
        sched_yield();
    }
}
