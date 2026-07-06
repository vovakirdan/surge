#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_async_internal.h"
#include "rt_sync_point.h"

#include <limits.h>
#include <time.h>

// The shard-lane worker turn and the io thread (peel B1b). The worker owns
// only its shard lock across the scan; popped ids come from its own queues,
// so the task's owner is this shard and the deref is legal under the held
// lock (D3). Lane-aware helpers take their own locks, so the lock is
// released around them and around every poll.

int rt_run_ready_one_nowait_locked(rt_executor* ex) {
    if (ex == NULL) {
        return 0;
    }
    uint64_t id = 0;
    if (!ready_pop(ex, &id)) {
        return 0;
    }
    rt_task* task = get_task(ex, id);
    if (task == NULL || task_status_load(task) == TASK_DONE) {
        return 1;
    }
    rt_shard* owner_shard = rt_task_owner_shard(ex, task);
    rt_scheduler* scheduler = rt_shard_scheduler(owner_shard);
    if (owner_shard == NULL || scheduler == NULL) {
        return 1;
    }
    task_status_store(task, TASK_RUNNING);
    (void)task_wake_token_exchange(task, 0);
    rt_shard_lock(owner_shard);
    scheduler->running_count++;
    rt_shard_unlock(owner_shard);
    rt_set_current_task(task);

    uint8_t kind = task->kind;
    if (kind != TASK_KIND_USER) {
        task_polling_enter(task);
        poll_outcome outcome = poll_task(ex, task);
        task_polling_exit(task);
        rt_shard_lock(owner_shard);
        scheduler->running_count--;
        rt_shard_unlock(owner_shard);
        apply_poll_outcome(ex, task, outcome);
        rt_set_current_task(NULL);
        return 1;
    }

    rt_control_unlock(ex);
    task_polling_enter(task);
    poll_outcome outcome = poll_task(ex, task);
    task_polling_exit(task);
    RT_SYNC_POINT_IF(outcome.kind == POLL_PARKED, SP_PARK_BEFORE_WAITING);
    rt_control_lock(ex);

    rt_shard_lock(owner_shard);
    scheduler->running_count--;
    rt_shard_unlock(owner_shard);
    apply_poll_outcome(ex, task, outcome);
    rt_set_current_task(NULL);
    return 1;
}

void rt_io_wait_slice(rt_executor* ex) {
    // Self-refreshing wait (peel B4): the io thread sleeps on shard 0's
    // poller_cv — its predicates (net waiters, timers, idleness) are
    // shard-lane state — and re-checks them at least every poll slice. The
    // control lock is released for the sleep so control-lane work never
    // stalls behind an idle io thread.
    struct timespec deadline;
    clock_gettime(CLOCK_REALTIME, &deadline);
    deadline.tv_nsec += 50L * 1000L * 1000L;
    if (deadline.tv_nsec >= 1000000000L) {
        deadline.tv_sec += 1;
        deadline.tv_nsec -= 1000000000L;
    }
    rt_shard* shard0 = rt_runtime_shard0(rt_executor_runtime(ex));
    if (shard0 == NULL) {
        return;
    }
    rt_control_unlock(ex);
    rt_shard_lock(shard0);
    if (shard0->poller_nudges == 0) {
        (void)pthread_cond_timedwait(&shard0->poller_cv, &shard0->lock, &deadline);
    }
    shard0->poller_nudges = 0;
    rt_shard_unlock(shard0);
    rt_control_lock(ex);
}

void* rt_worker_main(void* arg) {
    rt_worker_ctx* ctx = (rt_worker_ctx*)arg;
    rt_executor* ex = ctx != NULL ? ctx->ex : NULL;
    const int worker_net_poll_slice_ms = 1;
    if (ex == NULL) {
        return NULL;
    }
    rt_shard* shard = ctx != NULL ? ctx->shard : NULL;
    rt_scheduler* scheduler = ctx != NULL ? ctx->scheduler : NULL;
    if (shard == NULL || scheduler == NULL) {
        return NULL;
    }
    rt_heap_accounting_set_current_cell(ctx->heap_cell);
    tls_worker_ctx = ctx;
    tls_worker_id = ctx->worker_id <= (uint32_t)INT_MAX ? (int)ctx->worker_id : -1;
    rt_set_current_task(NULL);
    // Peel B1b: the worker turn owns only its shard lock. Popped ids come
    // from this shard's queues, so the task's owner is this shard and the
    // deref is legal under the held lock (D3). The lane-aware helpers
    // (sleep fire, net poll pass, wake/park/mark_done) take their own locks,
    // so the lock is released around them and around every poll.
    for (;;) {
        rt_trace_drain_signal_dump();
        if (++ctx->net_tick % 61U == 0U && rt_net_begin_poll_on_shard(ex, ctx->shard_id)) {
            (void)rt_net_poll_waiters_owned_on_shard(ex, ctx->shard_id, 0);
        }
        rt_shard_lock(shard);
        uint64_t id = 0;
        while (!ex->shutdown && !worker_next_ready(ctx, &id)) {
            if (rt_sleep_store_min(&shard->sleep_store) <= rt_clock_now(ex)) {
                rt_shard_unlock(shard);
                size_t fired = rt_sleep_fire_due_on_shard(ex, shard, rt_clock_now(ex));
                rt_shard_lock(shard);
                if (fired > 0) {
                    continue;
                }
            }
            rt_shard_unlock(shard);
            if (rt_net_begin_poll_on_shard(ex, ctx->shard_id)) {
                int woke_net =
                    rt_net_poll_waiters_owned_on_shard(ex, ctx->shard_id, worker_net_poll_slice_ms);
                rt_shard_lock(shard);
                if (woke_net) {
                    continue;
                }
                if (ex->shutdown || worker_next_ready(ctx, &id)) {
                    break;
                }
                if (rt_net_has_waiters_on_shard(ex, ctx->shard_id)) {
                    continue;
                }
            } else {
                rt_shard_lock(shard);
                if (ex->shutdown || worker_next_ready(ctx, &id)) {
                    break;
                }
            }
            // Sleep only after local, inject, and steal queues have been
            // checked under the shard lock; wake_pending closes the window
            // between that check and the wait.
            rt_debug_assert_no_parked_with_work(ex, ctx->shard_id);
            rt_trace_worker_sleep();
            while (scheduler->wake_pending == 0 && !ex->shutdown) {
                pthread_cond_wait(&shard->worker_cv, &shard->lock);
            }
            if (scheduler->wake_pending > 0) {
                scheduler->wake_pending--;
            }
            rt_trace_worker_wake();
        }
        if (ex->shutdown) {
            rt_shard_unlock(shard);
            break;
        }
        rt_task* task = get_task(ex, id);
        if (task == NULL || task_status_load(task) == TASK_DONE) {
            rt_shard_unlock(shard);
            continue;
        }
        task_status_store(task, TASK_RUNNING);
        (void)task_wake_token_exchange(task, 0);
        scheduler->running_count++;
        rt_shard_unlock(shard);
        rt_set_current_task(task);

        task_polling_enter(task);
        poll_outcome outcome = poll_task(ex, task);
        task_polling_exit(task);
        RT_SYNC_POINT_IF(task->kind == TASK_KIND_USER && outcome.kind == POLL_PARKED,
                         SP_PARK_BEFORE_WAITING);

        rt_shard_lock(shard);
        if (scheduler->running_count > 0) {
            scheduler->running_count--;
        }
        rt_shard_unlock(shard);
        apply_poll_outcome(ex, task, outcome);
        rt_set_current_task(NULL);
    }
    rt_set_current_task(NULL);
    tls_worker_ctx = NULL;
    tls_worker_id = -1;
    rt_heap_accounting_set_current_cell(NULL);
    return NULL;
}

void* rt_io_main(void* arg) {
    rt_executor* ex = (rt_executor*)arg;
    if (ex == NULL) {
        return NULL;
    }
    rt_heap_accounting_set_current_cell(
        rt_heap_accounting_io_cell(rt_executor_heap_accounting(ex)));
    const int poll_slice_ms = 50;
    const int net_ready_drain_limit = 16;
    rt_control_lock(ex);
    for (;;) {
        if (rt_trace_dump_requested()) {
            rt_control_unlock(ex);
            rt_trace_drain_signal_dump();
            rt_control_lock(ex);
        }
        if (ex->shutdown) {
            break;
        }
        uint64_t deadline = 0;
        int have_timer = rt_next_sleep_deadline(ex, &deadline);
        size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
        int have_net = shard_count <= 1 && rt_net_has_waiters_on_shard(ex, 0);
        int idle = rt_sched_idle_sample_locked(ex);

        if (!have_net && (!have_timer || !idle)) {
            rt_io_wait_slice(ex);
            continue;
        }

        if (!have_net) {
            if (idle && have_timer && advance_time_to_next_timer(ex)) {
                continue;
            }
            rt_io_wait_slice(ex);
            continue;
        }

        int timeout_ms = poll_slice_ms;
        if (idle && have_timer) {
            uint64_t now = ex->now_ms;
            uint64_t diff = deadline > now ? deadline - now : 0;
            int timer_ms = diff > (uint64_t)INT_MAX ? INT_MAX : (int)diff;
            if (timer_ms < timeout_ms) {
                timeout_ms = timer_ms;
            }
        }
        if (timeout_ms < 0) {
            timeout_ms = poll_slice_ms;
        }
        if (!rt_net_begin_poll_on_shard(ex, 0)) {
            // Another poller owns the net wait. Its short slices never reach
            // the timer branch below, so due virtual timers must advance
            // here or a worker monopolizing the poll starves every sleeper.
            if (idle && have_timer && advance_time_to_next_timer(ex)) {
                continue;
            }
            rt_io_wait_slice(ex);
            continue;
        }
        if (rt_net_poll_waiters_owned_on_shard(ex, 0, timeout_ms)) {
            for (int i = 0; i < net_ready_drain_limit && !ex->shutdown; i++) {
                if (!rt_run_ready_one_nowait_locked(ex)) {
                    break;
                }
            }
            continue;
        }
        if (idle && have_timer) {
            if (advance_time_to_next_timer(ex)) {
                continue;
            }
        }
    }
    rt_control_unlock(ex);
    rt_heap_accounting_set_current_cell(NULL);
    return NULL;
}
