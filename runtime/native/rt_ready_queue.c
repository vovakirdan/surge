// Ready-queue ownership (RV2-DEBT-003 split): this module
// owns every mutation of a shard scheduler's ready queues (local deques +
// inject) and the worker pop policy. Lane contract: every queue mutation runs
// under the owning shard's rt_shard_lock; callers either already hold it
// (ready_push_task_locked) or this module acquires exactly that one shard
// lock (D2 order: control lane, if held, is acquired before any shard lock).
// Extracted verbatim from rt_async_state.c; no behavior change.

#include "rt_async_internal.h"
#include "rt_sync_point.h"

static int scheduler_runnable_is_empty(const rt_scheduler* scheduler) {
    if (scheduler == NULL) {
        return 1;
    }
    if (scheduler->inject.len > 0) {
        return 0;
    }
    if (scheduler->local_queues == NULL || scheduler->worker_count == 0) {
        return 1;
    }
    for (uint32_t i = 0; i < scheduler->worker_count; i++) {
        if (scheduler->local_queues[i].len > 0) {
            return 0;
        }
    }
    return 1;
}

// Claim `count` items as in flight. Called with the shard lock already held —
// the same critical section that took them out — because a batch that is
// removed and only then counted has a window of exactly the shape this
// counter exists to close.
void rt_sched_publishing_begin_locked(rt_shard* shard, size_t count) {
    if (shard == NULL || count == 0) {
        return;
    }
    rt_scheduler* scheduler = rt_shard_scheduler(shard);
    if (scheduler == NULL) {
        return;
    }
    scheduler->publishing_count += (uint32_t)count;
}

// Release the claim once the batch has been republished. Takes the lock itself
// rather than being called under it: republishing wakes tasks, and a wake takes
// the target's owner lock, which may be this same mutex.
void rt_sched_publishing_end(rt_shard* shard, size_t count) {
    if (shard == NULL || count == 0) {
        return;
    }
    rt_shard_lock(shard);
    rt_scheduler* scheduler = rt_shard_scheduler(shard);
    if (scheduler != NULL) {
        if (scheduler->publishing_count >= (uint32_t)count) {
            scheduler->publishing_count -= (uint32_t)count;
        } else {
            scheduler->publishing_count = 0;
        }
    }
    rt_shard_unlock(shard);
}

// Locked idle sample for control-lane callers (io thread, N=1 runner):
// reads each shard's queues, running count and in-flight batch under that
// shard's lock, one shard at a time. Cross-shard atomicity is not promised
// (spike D7); for SURGE_SHARDS=1 the single-shard sample is exact.
int rt_sched_idle_sample_locked(rt_executor* ex) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    size_t shard_count = rt_runtime_shard_count(runtime);
    for (size_t i = 0; i < shard_count; i++) {
        rt_shard* shard = rt_runtime_shard(runtime, i);
        if (shard == NULL) {
            continue;
        }
        rt_shard_lock(shard);
        const rt_scheduler* scheduler = rt_shard_scheduler_const(shard);
        // publishing_count is asked alongside the queues rather than instead of
        // them: a batch in flight is work that is about to be in a queue, and
        // an observer that reads only the queues concludes idle while it is in
        // neither.
        int busy =
            scheduler != NULL &&
            (scheduler->running_count > 0 || scheduler->publishing_count > 0 ||
             !scheduler_runnable_is_empty(scheduler) || rt_transport_inbound_len_locked(shard) > 0);
        rt_shard_unlock(shard);
        if (busy) {
            return 0;
        }
    }
    return 1;
}

static uint64_t sched_next_u64(rt_worker_ctx* ctx) {
    if (ctx == NULL) {
        return 0;
    }
    uint64_t z = (ctx->sched_rng += UINT64_C(0x9e3779b97f4a7c15));
    z = (z ^ (z >> 30)) * UINT64_C(0xbf58476d1ce4e5b9);
    z = (z ^ (z >> 27)) * UINT64_C(0x94d049bb133111eb);
    return z ^ (z >> 31);
}

rt_scheduler* current_worker_scheduler(const rt_executor* ex) {
    if (tls_worker_ctx == NULL || tls_worker_ctx->ex != ex) {
        return NULL;
    }
    return tls_worker_ctx->scheduler;
}

static rt_deque* current_local_queue(const rt_executor* ex, rt_scheduler* scheduler) {
    if (scheduler == NULL || scheduler->local_queues == NULL || scheduler->worker_count == 0) {
        return NULL;
    }
    if (current_worker_scheduler(ex) != scheduler || tls_worker_ctx == NULL) {
        return NULL;
    }
    if (tls_worker_ctx->worker_id >= scheduler->worker_count) {
        return NULL;
    }
    return &scheduler->local_queues[tls_worker_ctx->worker_id];
}

static int pop_task_from_deque(rt_executor* ex,
                               rt_deque* dq,
                               int lifo,
                               uint64_t* out_id,
                               rt_trace_sched_source source,
                               uint32_t stealer_shard_id) {
    if (ex == NULL || dq == NULL) {
        return 0;
    }
    while (dq->len > 0) {
        uint64_t id = 0;
        if (lifo) {
            if (!deque_pop_tail(dq, &id)) {
                return 0;
            }
        } else {
            if (!deque_pop_head(dq, &id)) {
                return 0;
            }
        }
        rt_task* task = get_task(ex, id);
        uint8_t status = task_status_load(task);
        if (task == NULL || status == TASK_DONE || status == TASK_RUNNING) {
            if (task != NULL) {
                // Clear stale enqueue flags for discarded entries (e.g., duplicates).
                task_enqueued_store(task, 0);
            }
            continue;
        }
        if (source == RT_TRACE_SCHED_SRC_STEAL &&
            !rt_task_can_steal_from_shard_or_trace_denied(task, stealer_shard_id)) {
            int ok = lifo ? deque_push_tail(dq,
                                            id,
                                            "async: local queue overflow",
                                            "async: local queue allocation failed")
                          : deque_push_head(dq,
                                            id,
                                            "async: local queue overflow",
                                            "async: local queue allocation failed");
            (void)ok;
            return 0;
        }
        task_enqueued_store(task, 0);
        if (task->owner_shard_valid != 0 && task->placement_class == TASK_PLACEMENT_CONNECTION) {
            rt_trace_sched_connection_owner_run(task->owner_shard_id, stealer_shard_id);
        }
        rt_trace_sched_record(source, id);
        if (out_id != NULL) {
            *out_id = id;
        }
        return 1;
    }
    return 0;
}

// Leaf enqueue: caller holds the owner shard's lock and has already
// validated the task (owner == shard, not DONE/RUNNING, not enqueued). The
// wake token for the shard's worker_cv is bumped and signaled under the
// same lock hold, so a sleeping worker cannot miss the push.
int ready_push_task_locked(const rt_executor* ex,
                           rt_shard* owner_shard,
                           rt_task* task,
                           int force_inject,
                           int front,
                           int signal_ready) {
    rt_scheduler* scheduler = rt_shard_scheduler(owner_shard);
    if (ex == NULL || task == NULL || scheduler == NULL) {
        return 0;
    }
    // Injection policy:
    // - Worker thread: enqueue locally (LIFO pop) to keep cache locality.
    // - Non-worker thread (main/I/O/external): enqueue on the global injection queue.
    // No last-worker affinity is tracked; wake/spawn follows the current thread.
    rt_deque* local = NULL;
    if (!force_inject) {
        local = current_local_queue(ex, scheduler);
    }
    int signal_ready_now = signal_ready;
    if (local != NULL) {
        // Local queues are popped from the tail, so tail insertion is the local priority path.
        int ok = deque_push_tail(
            local, task->id, "async: local queue overflow", "async: local queue allocation failed");
        if (!ok) {
            return 0;
        }
        // A single local continuation is usually consumed by the current worker on its
        // next scheduler turn; waking another worker often just creates steal/sleep churn.
        signal_ready_now = signal_ready && local->len > 1;
    } else {
        int ok = front ? deque_push_head(&scheduler->inject,
                                         task->id,
                                         "async: inject queue overflow",
                                         "async: inject queue allocation failed")
                       : deque_push_tail(&scheduler->inject,
                                         task->id,
                                         "async: inject queue overflow",
                                         "async: inject queue allocation failed");
        if (!ok) {
            return 0;
        }
    }
    task_enqueued_store(task, 1);
    task_status_store(task, TASK_READY);
    if (signal_ready_now) {
        if (scheduler->wake_pending < UINT32_MAX) {
            scheduler->wake_pending++;
        }
        pthread_cond_signal(&owner_shard->worker_cv);
    }
    return 1;
}

static int ready_push_with_policy(
    rt_executor* ex, uint64_t id, int force_inject, int front, int signal_ready) {
    // Caller holds the control lock and no shard lock; the owner shard lock
    // nests here around the queue mutation (D2 order).
    if (ex == NULL) {
        return 0;
    }
    rt_task* task = get_task(ex, id);
    uint8_t status = task_status_load(task);
    if (task == NULL || status == TASK_DONE) {
        return 0;
    }
    if (status == TASK_RUNNING) {
        return 0;
    }
    if (task_enqueued_load(task) != 0) {
        return 0;
    }
    rt_shard* owner_shard = rt_task_owner_shard(ex, task);
    if (owner_shard == NULL) {
        return 0;
    }
    RT_SYNC_POINT_IF(force_inject, SP_READY_REQUEUE_BEFORE_LOCK);
    rt_shard_lock(owner_shard);
    // Re-validate under the owner shard lock: the unlocked checks above can
    // race a wake that enqueues this task and a worker that pops that entry
    // and stores RUNNING (pops and RUNNING stores serialize on this same
    // lock). Pushing blindly after losing that race would insert a duplicate
    // queue entry and ready_push_task_locked's READY store would overwrite
    // RUNNING under a live poll — a second worker then takes the duplicate
    // and double-polls the task. RV2_DEBT_027_NEGATIVE_CONTROL restores the
    // unvalidated push, which MUST double-poll the deterministic requeue-race
    // proof (the non-vacuity check).
#ifndef RV2_DEBT_027_NEGATIVE_CONTROL
    status = task_status_load(task);
    if (status == TASK_DONE || status == TASK_RUNNING || task_enqueued_load(task) != 0) {
        rt_shard_unlock(owner_shard);
        return 0;
    }
#endif
    int pushed = ready_push_task_locked(ex, owner_shard, task, force_inject, front, signal_ready);
    rt_shard_unlock(owner_shard);
    if (!pushed) {
        return 0;
    }
    const rt_channel_blocking_compat* compat = rt_executor_channel_blocking_compat_const(ex);
    if (compat != NULL && compat->channel_blocked_workers > 0) {
        maybe_start_compensation_worker_locked(ex);
    }
    return 1;
}

static int ready_push_inner(rt_executor* ex, uint64_t id, int force_inject) {
    return ready_push_with_policy(ex, id, force_inject, 0, 1);
}

void ready_push(rt_executor* ex, uint64_t id) {
    // Caller holds ex->lock.
    (void)ready_push_inner(ex, id, 0);
}

// Claim a task this thread has just taken out of a queue: it belongs to this
// thread's poll now, and these three stores are how every other thread is told
// so. enqueued says "a queue entry names me", the status says "someone is
// running me", and the wake token is the signal a park racing a wake must
// abort on -- the poll about to run consumes it, exactly as the worker turn
// consumes it after its own pop (rt_worker_turn.c).
static void claim_task_off_queue(rt_task* task) {
    task_enqueued_store(task, 0);
    task_status_store(task, TASK_RUNNING);
    (void)task_wake_token_exchange(task, 0);
}

int ready_claim_current_local_tail(rt_executor* ex, uint64_t id) {
    // Serialized by the owner shard lock taken below, NOT the control lock:
    // rt_task_poll (the sole caller, rt_async_task.c) reaches here control-free.
    // The take here, the worker's own next_ready pop, and any steal all run
    // under this shard's rt_shard_lock (rt_worker_turn.c), so that lock is the
    // local queue's serializer. Intentionally narrow: it only removes the fresh
    // child __task_create just pushed onto the current worker, so the queue is
    // this shard's own and its lock nests here.
    //
    // The take and the claim are ONE observation, under that one lock, because
    // every gate the wake path has is a test of exactly these two fields:
    //
    //     if (status == TASK_DONE || status == TASK_RUNNING ||
    //         task_enqueued_load(task) != 0) return 0;   (rt_task_park.c)
    //
    // Removing the task and claiming it apart leaves an instant when neither
    // half of that test is true and the task is nonetheless in no queue: it
    // reads READY with enqueued already cleared. A wake arriving then passes
    // the gate, pushes, and a second worker takes a task this thread is about
    // to poll -- and ready_push_task_locked's READY store lands under the live
    // poll. That is the same duplicate-entry shape ready_push_with_policy
    // re-validates against under this very lock, and its re-validation cannot
    // see a claim made outside it. rt_worker_turn.c's own claim is inside its
    // pop's critical section for this reason; this is that discipline.
    rt_task* task = get_task(ex, id);
    rt_shard* owner_shard = rt_task_owner_shard(ex, task);
    rt_scheduler* scheduler = rt_shard_scheduler(owner_shard);
    if (task == NULL || owner_shard == NULL) {
        return 0;
    }
    rt_shard_lock(owner_shard);
    rt_deque* local = current_local_queue(ex, scheduler);
    int taken = 0;
    if (local != NULL && local->len > 0 && local->buf != NULL) {
        size_t idx = local->head + local->len - 1;
        if (local->buf[idx] == id) {
            local->len--;
            if (local->len == 0) {
                local->head = 0;
            }
            taken = 1;
        }
    }
    if (taken) {
        RT_INLINE_CLAIM_UNDER_LOCK(task);
    }
    rt_shard_unlock(owner_shard);
    if (taken) {
        RT_INLINE_CLAIM_SPLIT_FIRST(task);
        RT_SYNC_POINT(SP_INLINE_CHILD_TAKEN_OFF_QUEUE);
        RT_INLINE_CLAIM_SPLIT_REST(task);
    }
    return taken;
}

int ready_push_yielded_task(rt_executor* ex, uint64_t id) {
    // A yielding worker immediately re-enters the scheduler loop, so waking another
    // worker here mostly creates condvar churn for task-to-task handoffs.
    return ready_push_with_policy(ex, id, 1, 0, 0);
}

int ready_pop(rt_executor* ex, uint64_t* out_id) {
    // Control-lane single-runner pop (N=1 runner and io drain): the queue is
    // shard 0 state, so the pop nests shard 0's lock under the control lock.
    rt_shard* shard0 = rt_runtime_shard0(rt_executor_runtime(ex));
    rt_scheduler* scheduler = rt_shard_scheduler(shard0);
    if (shard0 == NULL || scheduler == NULL) {
        return 0;
    }
    rt_shard_lock(shard0);
    int popped =
        pop_task_from_deque(ex, &scheduler->inject, 0, out_id, RT_TRACE_SCHED_SRC_INJECT, 0);
    rt_shard_unlock(shard0);
    return popped;
}

int worker_next_ready(rt_worker_ctx* ctx, uint64_t* out_id) {
    rt_executor* ex = ctx != NULL ? ctx->ex : NULL;
    rt_scheduler* scheduler = ctx != NULL ? ctx->scheduler : NULL;
    uint32_t worker_id = ctx != NULL ? ctx->worker_id : 0;
    uint32_t shard_id = ctx != NULL ? ctx->shard_id : 0;
    if (ex == NULL || scheduler == NULL) {
        return 0;
    }
    if (scheduler->sched_mode == SCHED_SEEDED) {
        rt_deque* local = scheduler->local_queues != NULL && worker_id < scheduler->worker_count
                              ? &scheduler->local_queues[worker_id]
                              : NULL;
        int local_has = local != NULL && local->len > 0;
        int inject_has = scheduler->inject.len > 0;
        int others_have = 0;
        if (scheduler->local_queues != NULL && scheduler->worker_count > 1) {
            for (uint32_t i = 0; i < scheduler->worker_count; i++) {
                if (i == worker_id) {
                    continue;
                }
                if (scheduler->local_queues[i].len > 0) {
                    others_have = 1;
                    break;
                }
            }
        }
        if (local_has && inject_has) {
            if ((sched_next_u64(ctx) & 1U) == 0U) {
                if (pop_task_from_deque(ex, local, 1, out_id, RT_TRACE_SCHED_SRC_LOCAL, shard_id)) {
                    return 1;
                }
                if (pop_task_from_deque(
                        ex, &scheduler->inject, 0, out_id, RT_TRACE_SCHED_SRC_INJECT, shard_id)) {
                    return 1;
                }
            } else {
                if (pop_task_from_deque(
                        ex, &scheduler->inject, 0, out_id, RT_TRACE_SCHED_SRC_INJECT, shard_id)) {
                    return 1;
                }
                if (pop_task_from_deque(ex, local, 1, out_id, RT_TRACE_SCHED_SRC_LOCAL, shard_id)) {
                    return 1;
                }
            }
        } else if (local_has) {
            if (pop_task_from_deque(ex, local, 1, out_id, RT_TRACE_SCHED_SRC_LOCAL, shard_id)) {
                return 1;
            }
        } else if (inject_has) {
            if (others_have && (sched_next_u64(ctx) & 1U) != 0U) {
                if (scheduler->worker_count > 1) {
                    uint32_t span = scheduler->worker_count - 1;
                    uint32_t start = (worker_id + 1 + (uint32_t)(sched_next_u64(ctx) % span)) %
                                     scheduler->worker_count;
                    for (uint32_t offset = 0; offset < span; offset++) {
                        uint32_t victim = start + offset;
                        if (victim >= scheduler->worker_count) {
                            victim -= scheduler->worker_count;
                        }
                        if (victim == worker_id) {
                            continue;
                        }
                        if (pop_task_from_deque(ex,
                                                &scheduler->local_queues[victim],
                                                0,
                                                out_id,
                                                RT_TRACE_SCHED_SRC_STEAL,
                                                shard_id)) {
                            return 1;
                        }
                    }
                }
            }
            if (pop_task_from_deque(
                    ex, &scheduler->inject, 0, out_id, RT_TRACE_SCHED_SRC_INJECT, shard_id)) {
                return 1;
            }
        }
        if (scheduler->local_queues == NULL || scheduler->worker_count <= 1) {
            return 0;
        }
        uint32_t span = scheduler->worker_count - 1;
        uint32_t start =
            (worker_id + 1 + (uint32_t)(sched_next_u64(ctx) % span)) % scheduler->worker_count;
        for (uint32_t offset = 0; offset < span; offset++) {
            uint32_t victim = start + offset;
            if (victim >= scheduler->worker_count) {
                victim -= scheduler->worker_count;
            }
            if (victim == worker_id) {
                continue;
            }
            if (pop_task_from_deque(ex,
                                    &scheduler->local_queues[victim],
                                    0,
                                    out_id,
                                    RT_TRACE_SCHED_SRC_STEAL,
                                    shard_id)) {
                return 1;
            }
        }
        return 0;
    }
    if (ctx != NULL && ++ctx->pop_tick % 61U == 0U &&
        pop_task_from_deque(
            ex, &scheduler->inject, 0, out_id, RT_TRACE_SCHED_SRC_INJECT, shard_id)) {
        return 1;
    }
    if (scheduler->local_queues != NULL && worker_id < scheduler->worker_count) {
        if (pop_task_from_deque(ex,
                                &scheduler->local_queues[worker_id],
                                1,
                                out_id,
                                RT_TRACE_SCHED_SRC_LOCAL,
                                shard_id)) {
            return 1;
        }
    }
    if (pop_task_from_deque(
            ex, &scheduler->inject, 0, out_id, RT_TRACE_SCHED_SRC_INJECT, shard_id)) {
        return 1;
    }
    if (scheduler->local_queues == NULL || scheduler->worker_count <= 1) {
        return 0;
    }
    for (uint32_t offset = 1; offset < scheduler->worker_count; offset++) {
        uint32_t victim = (worker_id + offset) % scheduler->worker_count;
        if (victim == worker_id) {
            continue;
        }
        if (pop_task_from_deque(ex,
                                &scheduler->local_queues[victim],
                                0,
                                out_id,
                                RT_TRACE_SCHED_SRC_STEAL,
                                shard_id)) {
            return 1;
        }
    }
    return 0;
}
