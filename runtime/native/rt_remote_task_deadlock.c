#include "rt_remote_task_internal.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// Remote-channel self-deadlock detection. A caller suspended on an execute
// reply cannot be woken by anything if the body it shipped is parked on a
// channel waiter while the whole executor is quiescent: every shard has no
// running or ready task, no inbound transport work, no pending timers, no
// net waiters, and no blocking work is queued or running. At that point no
// future event exists that could free the channel, so the runtime reports
// the deadlock with an actionable message instead of hanging silently — the
// remote analog of the local external-await "async deadlock" panic.
//
// The check runs at a worker's idle-park edge with NO shard lock held (the
// caller re-acquires its shard lock afterwards; a wake landing in that
// window bumps wake_pending under the shard lock, so the sleep guard cannot
// lose it). Each shard's signals are read under that shard's own lock, one
// shard at a time — the lane discipline forbids holding two shard locks.
// The scan is double-checked: quiescence, suspect, then quiescence and the
// same suspect again. Any activity between the passes either shows up as a
// running/ready task or inbound work under some shard lock, or it woke the
// suspect — and a woken suspect is no longer TASK_WAITING on a channel key
// at the second look. The blocking pool's dec-before-wake window is
// absorbed the same way: the wake makes some shard non-quiescent.
//
// Coverage note: the single-shard single-worker configuration starts no
// worker threads — the awaiting driver thread polls tasks itself — so this
// park-edge check never runs there. That mode is covered by the driver-side
// "async deadlock" panic (rt_async_poll.c) which fires when an external
// await finds nothing runnable.
//
// Blind spot (shared with the driver-side panic): a non-runtime thread that
// touches a channel through FFI is invisible to the quiescence scan.
// Embedders whose external threads legitimately feed or drain channels can
// opt out with SURGE_REMOTE_DEADLOCK_DETECT=0; the default is on in every
// build.

static int deadlock_detect_enabled(void) {
    // -1 = not yet parsed. Racing initializers compute the same value from
    // the same environment, so a duplicated store is harmless.
    static _Atomic int enabled = -1;
    int value = atomic_load_explicit(&enabled, memory_order_relaxed);
    if (value < 0) {
        const char* env = getenv("SURGE_REMOTE_DEADLOCK_DETECT");
        value = env == NULL || strcmp(env, "0") != 0;
        atomic_store_explicit(&enabled, value, memory_order_relaxed);
    }
    return value;
}

// All signals that could produce future progress on this shard, read under
// the shard's own lock: transport park state, running and ready tasks,
// undrained inbound work, pending timers, and net waiters.
static int shard_quiescent_locked(const rt_executor* ex, rt_shard* shard) {
    if (atomic_load_explicit(&shard->transport.park_state, memory_order_seq_cst) !=
        RT_TRANSPORT_SHARD_PARKED) {
        return 0;
    }
    const rt_scheduler* scheduler = rt_shard_scheduler(shard);
    if (scheduler == NULL || scheduler->running_count != 0 || scheduler->inject.len != 0) {
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
    if (rt_sleep_store_min(&shard->sleep_store) != UINT64_MAX) {
        return 0;
    }
    if (rt_net_has_waiters_on_shard(ex, shard->shard_id)) {
        return 0;
    }
    return 1;
}

static int all_shards_quiescent(rt_executor* ex) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    size_t shard_count = rt_runtime_shard_count(runtime);
    for (size_t i = 0; i < shard_count; i++) {
        rt_shard* shard = rt_runtime_shard(runtime, i);
        if (shard == NULL) {
            return 0;
        }
        rt_shard_lock(shard);
        int quiescent = shard_quiescent_locked(ex, shard);
        rt_shard_unlock(shard);
        if (!quiescent) {
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

// Returns the id of a parked-on-channel body task of a suspended execute
// pending, or 0. Two phases keep the lock order acyclic: candidate bodies
// are collected and retained under `state->lock` — `owner_registered` is
// checked under that lock because the flag is set right after the
// registration takes its counted body reference and cleared under the same
// lock by whichever path will drop that reference, so a registered pending
// guarantees the body memory is alive for the `task_add_ref` here (inbound
// dispatch nests `state->lock` under a shard lock, so this function must
// never take a shard lock inside it). Each body's status and park key are
// then read
// under its own owner shard lock with no other lock held. Batches walk the
// whole list by qualifying position; at the quiescence this runs under the
// list cannot mutate between batches, and any churn that could shift
// positions also fails the caller's re-verify. When `expect_task_id` is
// nonzero, only that body counts — the verify pass must see the same
// suspect still parked, not a new one.
static uint64_t find_channel_parked_body(rt_executor* ex,
                                         uint64_t expect_task_id,
                                         const char** op_name,
                                         uint32_t* owner_shard_id) {
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (state == NULL) {
        return 0;
    }
    enum { RT_DEADLOCK_CANDIDATE_BATCH = 8 };
    size_t skip = 0;
    for (;;) {
        rt_task* bodies[RT_DEADLOCK_CANDIDATE_BATCH];
        size_t body_count = 0;
        size_t seen = 0;
        pthread_mutex_lock(&state->lock);
        for (rt_remote_task_pending* it = state->pending_head; it != NULL; it = it->next) {
            if (it->status != RT_REMOTE_TASK_STATUS_PENDING) {
                continue;
            }
            if (it->op != RT_REMOTE_TASK_OP_EXECUTE &&
                it->op != RT_REMOTE_TASK_OP_EXECUTE_ANCHORED) {
                continue;
            }
            if (it->handle.task_id == 0 || it->owner_registered == 0) {
                continue;
            }
            if (expect_task_id != 0 && it->handle.task_id != expect_task_id) {
                continue;
            }
            if (seen++ < skip) {
                continue;
            }
            rt_task* body = get_task(ex, it->handle.task_id);
            if (body == NULL) {
                continue;
            }
            task_add_ref(body);
            bodies[body_count++] = body;
            if (body_count == RT_DEADLOCK_CANDIDATE_BATCH) {
                break;
            }
        }
        pthread_mutex_unlock(&state->lock);
        uint64_t suspect_id = 0;
        for (size_t i = 0; i < body_count; i++) {
            rt_task* body = bodies[i];
            if (suspect_id == 0) {
                rt_shard* owner = rt_task_owner_shard(ex, body);
                if (owner != NULL) {
                    rt_shard_lock(owner);
                    int waiting = task_status_load(body) == TASK_WAITING;
                    uint8_t kind = body->park_key.kind;
                    rt_shard_unlock(owner);
                    if (waiting && (kind == WAKER_CHAN_SEND || kind == WAKER_CHAN_RECV)) {
                        suspect_id = body->id;
                        *op_name = kind == WAKER_CHAN_SEND ? "send" : "recv";
                        if (owner_shard_id != NULL) {
                            *owner_shard_id = body->owner_shard_id;
                        }
                    }
                }
            }
            task_release_lane_aware(ex, body);
        }
        if (suspect_id != 0) {
            return suspect_id;
        }
        if (body_count < RT_DEADLOCK_CANDIDATE_BATCH) {
            return 0;
        }
        skip += body_count;
    }
}

void rt_remote_task_deadlock_check(rt_executor* ex) {
    if (ex == NULL || atomic_load_explicit(&ex->shutdown, memory_order_acquire) != 0) {
        return;
    }
    if (!deadlock_detect_enabled()) {
        return;
    }
    if (!all_shards_quiescent(ex)) {
        return;
    }
    const char* op_name = "";
    uint32_t suspect_shard = 0;
    uint64_t suspect_id = find_channel_parked_body(ex, 0, &op_name, &suspect_shard);
    if (suspect_id == 0) {
        return;
    }
    if (!all_shards_quiescent(ex)) {
        return;
    }
    if (find_channel_parked_body(ex, suspect_id, &op_name, &suspect_shard) != suspect_id) {
        return;
    }
    static char message[256];
    (void)snprintf(message,
                   sizeof(message),
                   "remote channel deadlock: an anchored block is parked on channel %s "
                   "(body task %llu, shard %u) while every shard is idle; "
                   "nothing can wake it — the channel's consumer is the suspended caller",
                   op_name,
                   (unsigned long long)suspect_id,
                   (unsigned)suspect_shard);
    panic_msg(message);
}
