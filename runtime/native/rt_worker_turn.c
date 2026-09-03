#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_async_internal.h"
#include "rt_remote_spawn.h"
#include "rt_remote_task.h"
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
        task_polling_enter(task, POLL_SITE_NOWAIT_RUNNER_SYSTEM);
        poll_outcome outcome = poll_task(ex, task);
        task_polling_exit(task);
        // running_count stays up across apply_poll_outcome: that call is what
        // PUBLISHES this turn's successor work (mark_done -> join drain ->
        // ready_push). Dropping the count first leaves a window in which the
        // finished task is in no queue, its successor is not yet enqueued, and
        // rt_sched_idle_sample_locked therefore reads the executor as idle -
        // which is the predicate advance_time_to_next_timer uses to jump the
        // virtual clock straight to the next deadline.
        apply_poll_outcome(ex, task, outcome);
        rt_shard_lock(owner_shard);
        scheduler->running_count--;
        rt_shard_unlock(owner_shard);
        rt_set_current_task(NULL);
        return 1;
    }

    rt_control_unlock(ex);
    task_polling_enter(task, POLL_SITE_NOWAIT_RUNNER_USER);
    poll_outcome outcome = poll_task(ex, task);
    task_polling_exit(task);
    RT_SYNC_POINT_IF(outcome.kind == POLL_PARKED, SP_PARK_BEFORE_WAITING);
    rt_control_lock(ex);

    // Publish before dropping the count (see the system-task branch above).
    apply_poll_outcome(ex, task, outcome);
    rt_shard_lock(owner_shard);
    scheduler->running_count--;
    rt_shard_unlock(owner_shard);
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

#ifndef RV2_D46_SHUTDOWN_NEGATIVE_CONTROL
// An exiting carrier cancels every task pinned to it that it never got to
// poll, and then RUNS each one so the cancel can complete it (RUNTIME_V2.md,
// Section 10's shutdown sentence: the exiting carrier runs or cancels every
// task pinned to it). Nobody else may run them -- that is what the pin means
// -- so left in the deque they would hold their awaiters forever; and a
// cancel alone is not a completion: cancel_task seals the gate and wakes the
// target, and the target unwinds on its next poll, which only its carrier can
// give it. So the carrier gives it: this pops one pinned entry for the turn
// below to poll, cancelled, and the loop comes back here until its deque
// holds no pinned entry. Unpinned entries are left where they were; shutdown
// treats them as it always has.
//
// Called with the shard lock held and returns with it held. cancel_task wants
// the control lock, which nests OUTSIDE the shard's, so the ids are collected
// first and cancelled with the shard released; the wake a cancel issues to an
// already-queued target changes nothing (rt_task_park.c: not parked, nothing
// enqueued), so no entry is duplicated.
static int worker_drain_pinned_on_exit(rt_worker_ctx* ctx,
                                       rt_shard* shard,
                                       rt_scheduler* scheduler,
                                       uint64_t* out_id) {
    rt_executor* ex = ctx != NULL ? ctx->ex : NULL;
    if (ex == NULL || shard == NULL || scheduler == NULL || scheduler->local_queues == NULL ||
        ctx->worker_id >= scheduler->worker_count) {
        return 0;
    }
    rt_deque* dq = &scheduler->local_queues[ctx->worker_id];
    size_t len = dq->len;
    if (len == 0) {
        return 0;
    }
    // The ids are collected on the stack for the deque a carrier ordinarily
    // exits with; only a deque longer than the inline run pays an allocation,
    // the way scope_cancel_children_controlled already does. The exit sweep
    // is one of the three allocations the paired benchmark's exact budget
    // read on top of Wave D (RV2-DEBT-329).
    uint64_t inline_pinned[64];
    uint64_t* pinned = inline_pinned;
    if (len > 64) {
        pinned = (uint64_t*)rt_alloc((uint64_t)len * sizeof(uint64_t), _Alignof(uint64_t));
        if (pinned == NULL) {
            return 0;
        }
    }
    size_t pinned_len = 0;
    uint64_t first = 0;
    for (size_t i = 0; i < len; i++) {
        uint64_t id = 0;
        if (!deque_pop_head(dq, &id)) {
            break;
        }
        rt_task* task = get_task(ex, id);
        int mine = task != NULL && task_status_load(task) != TASK_DONE &&
                   task->carrier_valid != 0 && task->carrier_worker_id == ctx->worker_id;
        if (mine && pinned_len == 0) {
            // The one this turn will poll: off the deque for good.
            task_enqueued_store(task, 0);
            first = id;
        } else if (task == NULL || task_status_load(task) == TASK_DONE) {
            if (task != NULL) {
                task_enqueued_store(task, 0);
            }
            continue;
        } else {
            (void)deque_push_tail(
                dq, id, "async: local queue overflow", "async: local queue allocation failed");
        }
        if (mine) {
            pinned[pinned_len++] = id;
        }
    }
    if (pinned_len == 0) {
        if (pinned != inline_pinned) {
            rt_free((uint8_t*)pinned, (uint64_t)len * sizeof(uint64_t), _Alignof(uint64_t));
        }
        return 0;
    }
    rt_shard_unlock(shard);
    rt_control_lock(ex);
    for (size_t i = 0; i < pinned_len; i++) {
        const rt_task* task = get_task(ex, pinned[i]);
        if (task != NULL && task_cancelled_load(task) == 0) {
            cancel_task(ex, pinned[i]);
            rt_trace_sched_carrier_shutdown_cancelled();
        }
    }
    rt_control_unlock(ex);
    if (pinned != inline_pinned) {
        rt_free((uint8_t*)pinned, (uint64_t)len * sizeof(uint64_t), _Alignof(uint64_t));
    }
    rt_shard_lock(shard);
    *out_id = first;
    return 1;
}
#endif

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
        (void)rt_remote_spawn_drain_inbound_locked(ex, shard, RT_TRANSPORT_DRAIN_TURN_LIMIT);
        while (!ex->shutdown && !worker_next_ready(ctx, &id)) {
            if (rt_remote_spawn_drain_inbound_locked(ex, shard, 0) > 0) {
                continue;
            }
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
            // checked under the shard lock. Transport then publishes PARKED
            // under the same lock and re-checks inbound before worker_cv wait.
            if (rt_transport_prepare_shard_park(shard) == RT_TRANSPORT_STATUS_UNAVAILABLE) {
                continue;
            }
            rt_debug_assert_no_parked_with_work(ex, ctx->shard_id, ctx->worker_id);
            // The deadlock scan takes every shard's lock one at a time, so
            // it runs with this shard's lock released; a wake landing in
            // the gap bumps wake_pending under the lock and the sleep
            // guard below cannot lose it.
            rt_shard_unlock(shard);
            rt_remote_task_deadlock_check(ex);
            rt_shard_lock(shard);
            rt_trace_worker_sleep();
            // Two credits can name this worker: the shard's, which any worker
            // may consume, and its own, which a carrier-affine publication
            // addressed to it and nobody else can take. A broadcast on the
            // shard condvar wakes every sleeper; the ones with neither credit
            // re-check and sleep on, so an addressed wake never lands on the
            // wrong worker and the shard credit is never spent on its behalf.
            uint32_t* own_credit =
                scheduler->worker_wake_pending != NULL && ctx->worker_id < scheduler->worker_count
                    ? &scheduler->worker_wake_pending[ctx->worker_id]
                    : NULL;
            while (scheduler->wake_pending == 0 && (own_credit == NULL || *own_credit == 0) &&
                   !ex->shutdown) {
                pthread_cond_wait(&shard->worker_cv, &shard->lock);
            }
            if (own_credit != NULL && *own_credit > 0) {
                (*own_credit)--;
            } else if (scheduler->wake_pending > 0) {
                scheduler->wake_pending--;
            }
            rt_transport_mark_shard_running(shard);
            rt_trace_worker_wake();
        }
        if (ex->shutdown) {
            // An exiting carrier runs its cancelled pinned tasks to completion
            // first (worker_drain_pinned_on_exit). RV2_D46_SHUTDOWN_NEGATIVE_
            // CONTROL leaves them in the deque, which the carrier-affine
            // shutdown stand must catch.
#ifndef RV2_D46_SHUTDOWN_NEGATIVE_CONTROL
            if (!worker_drain_pinned_on_exit(ctx, shard, scheduler, &id)) {
                rt_shard_unlock(shard);
                break;
            }
#else
            rt_shard_unlock(shard);
            break;
#endif
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

        task_polling_enter(task, POLL_SITE_WORKER_LOOP);
        poll_outcome outcome = poll_task(ex, task);
        task_polling_exit(task);
        RT_SYNC_POINT_IF(task->kind == TASK_KIND_USER && outcome.kind == POLL_PARKED,
                         SP_PARK_BEFORE_WAITING);

        // Publish this turn's successor work BEFORE this worker stops counting
        // as busy; the reverse order lets an idle sample land in the gap and
        // jump the virtual clock past a pending deadline.
        apply_poll_outcome(ex, task, outcome);
        rt_shard_lock(shard);
        if (scheduler->running_count > 0) {
            scheduler->running_count--;
        }
        rt_shard_unlock(shard);
        rt_set_current_task(NULL);
    }
    rt_set_current_task(NULL);
    // Anything this thread deferred because it was holding a lock: the loop
    // above ended, so it holds none, and nobody else can run this thread's
    // queue. Without this a straggler deferred by the last turn is never
    // reclaimed at all -- the thread that owed it is gone.
    rt_lane_run_deferred_now();
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
