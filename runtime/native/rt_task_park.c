#include "rt_async_internal.h"
#include "rt_sync_point.h"

// Task park/unpark primitive (extraction from rt_async_state.c).
// Owner concept: how a RUNNING task suspends itself behind a waker_key
// (park_current), how a parked task is made schedulable again (wake_task /
// wake_net_task / wake_key_all), and the leaf enqueue that consumes the park
// under the owner shard lock (wake_task_on_shard_locked). The load-bearing
// invariants this module owns moved here verbatim with their functions: the D5
// register-then-commit park/wake-token race and the WAITING+!enqueued park_key
// ownership gate (RV2-DEBT-019), and the generation-qualified deferred removal
// of a stale channel-key registration. The extraction changed no locking,
// ordering, or atomics semantics; the sole edited line swaps a direct read of
// the file-scope channel_wake_force_inject static for its existing
// channel_wake_force_inject_enabled() accessor (rt_async_state.c owns that
// static). See docs/runtime-v2-epics/08-tasks/13-large-file-and-quality-tranche.md.

// Leaf wake: caller holds the owner shard's lock. Clears the park fields,
// sets the token, and enqueues; returns the parked-on key so the caller can
// remove the stale store registration after releasing this lock (D5: never
// hold two shard locks; a concurrent pop of that stale entry produces at
// most one absorbed spurious wake).
int wake_task_on_shard_locked(const rt_executor* ex,
                              rt_shard* owner_shard,
                              rt_task* task,
                              int force_inject,
                              int front,
                              int signal_ready,
                              waker_key* out_stale_key) {
    if (out_stale_key != NULL) {
        *out_stale_key = waker_none();
    }
    if (ex == NULL || task == NULL) {
        return 0;
    }
    // Ownership invariant (RV2-DEBT-019): park_key/park_prepared belong to the
    // RUNNING task's own thread until it commits to WAITING under this owner
    // shard lock (the D5 register-then-commit path: prepare_park /
    // park_current write them unlocked while RUNNING, then set TASK_WAITING
    // under this lock). The wake path may therefore only read or clear those
    // fields once the task is provably parked (WAITING, not enqueued); for any
    // other status they are being written by the owning thread and must not be
    // touched here. The wake token is atomic and is set unconditionally: it is
    // the D5 signal that a park racing this wake must abort and re-run, and it
    // is valid to deliver whether or not the task is currently parked.
    (void)task_wake_token_exchange(task, 1);
    if (tls_worker_ctx != NULL && tls_worker_ctx->ex == ex &&
        tls_worker_ctx->shard != owner_shard) {
        rt_trace_cross_shard_wake();
    }
    uint8_t status = task_status_load(task);
    if (status == TASK_DONE || status == TASK_RUNNING || task_enqueued_load(task) != 0) {
        // Not parked: the wake token above still guarantees a task mid-park
        // aborts and re-runs. Any stale registration this task left is cleaned
        // by its own remove_waiter path (rt_task_poll's register-then-verify
        // DONE branch, rt_async_task.c; park_current's token-abort requeue),
        // not by an orphaned deferred removal from a wake that never owned the
        // park.
        return 0;
    }
    // Parked (WAITING, not enqueued): park_key is stable under this owner shard
    // lock, so this wake owns the park consume.
    if (out_stale_key != NULL && waker_valid(task->park_key)) {
        *out_stale_key = task->park_key;
    }
    task->park_key = waker_none();
    task->park_prepared = 0;
    return ready_push_task_locked(ex, owner_shard, task, force_inject, front, signal_ready);
}

static void wake_task_with_policy(rt_executor* ex,
                                  uint64_t id,
                                  int remove_waiter_flag,
                                  int force_inject,
                                  int front,
                                  int signal_ready) {
    // Caller holds the control lock and no shard lock; wake_token handles a
    // wake that races with park_current.
    if (ex == NULL) {
        return;
    }
    rt_trace_wake_called();
    rt_task* task = get_task(ex, id);
    if (task == NULL || task_status_load(task) == TASK_DONE) {
        rt_trace_wake_ignored_completed();
        return;
    }
    rt_shard* owner_shard = rt_task_owner_shard(ex, task);
    if (owner_shard == NULL) {
        return;
    }
    waker_key stale_key = waker_none();
    uint32_t stale_seq = 0;
    rt_shard_lock(owner_shard);
    // Capture the park generation with the key: the deferred removal below
    // runs after this lock is released, and the woken task can re-register
    // the same channel key in that window; the generation confines the
    // removal to the entry this wake actually orphaned. Only a parked (WAITING,
    // not enqueued) task has a stable park_key/park_seq under this lock; a
    // RUNNING task owns those fields on its own thread (RV2-DEBT-019), so
    // reading them for a non-parked task would race its unlocked writes. The
    // same WAITING gate governs wake_task_on_shard_locked's park_key clear.
    if (task_status_load(task) == TASK_WAITING && task_enqueued_load(task) == 0 &&
        (task->park_key.kind == WAKER_CHAN_SEND || task->park_key.kind == WAKER_CHAN_RECV)) {
        stale_seq = task->park_seq;
    }
    int pushed = wake_task_on_shard_locked(
        ex, owner_shard, task, force_inject, front, signal_ready, &stale_key);
    rt_shard_unlock(owner_shard);
    // Join keys are exempt from the deferred removal (RV2-DEBT-046, toggle in
    // rt_sync_point.h): their entries carry no park generation (seq 0), so the
    // removal below is unqualified and sweeps EVERY (key, task) match —
    // including a fresh registration the woken task made after re-polling and
    // re-parking on the same join key in this window. That eaten registration
    // strands the joiner forever: join wakes are store-driven (mark_done ->
    // wake_key_all pops the join store), so a missing entry means no wake ever
    // comes (the async/02 fan-in hang). Leaving the stale entry is safe and
    // self-cleaning: the join target completes exactly once, its completion
    // drain pops every entry for the key, and a stale pop is absorbed as one
    // spurious wake by the wake token. Timer keys stay removable (their wake
    // is by-id from the sleep deadline index, never store-driven) and channel
    // keys stay removable because their entries carry the park generation
    // (stale_seq above).
    if (remove_waiter_flag && waker_valid(stale_key)) {
        RT_SYNC_POINT(SP_WAKE_BEFORE_STALE_REMOVAL);
        if (RT_DEBT046_STALE_KEY_REMOVABLE(stale_key)) {
            remove_waiter_generation(ex, stale_key, id, stale_seq);
        }
    }
    const rt_channel_blocking_compat* compat = rt_executor_channel_blocking_compat_const(ex);
    int compat_active = compat != NULL && atomic_load_explicit(&compat->channel_blocked_workers,
                                                               memory_order_acquire) > 0;
    if (pushed) {
        rt_trace_wake_enqueued();
        if (compat_active) {
            // Compensation bookkeeping is control-lane state; a control-free
            // (worker) wake takes the lane only when compat workers are
            // actually parked.
            int need_control = !rt_lane_holds_control();
            if (need_control) {
                rt_control_lock(ex);
            }
            maybe_start_compensation_worker_locked(ex);
            if (need_control) {
                rt_control_unlock(ex);
            }
        }
    } else if (compat_active) {
        // The woken task is RUNNING inside a sync-channel compat wait: the
        // OS worker sleeps on compat_cv under the control lock, so this
        // broadcast is its only wake path (the wake_token is already set).
        // It must hold the control lock or it can land in the waiter's
        // check-to-wait window and be lost.
        int need_control = !rt_lane_holds_control();
        if (need_control) {
            rt_control_lock(ex);
        }
        pthread_cond_broadcast(&ex->compat_cv);
        if (need_control) {
            rt_control_unlock(ex);
        }
    }
}

void wake_task(rt_executor* ex, uint64_t id, int remove_waiter_flag) {
    wake_task_with_policy(ex, id, remove_waiter_flag, 0, 0, 1);
}

void wake_net_task(rt_executor* ex, uint64_t id) {
    // Net readiness wakes go through the inject queue: a worker completing
    // its own shard's readiness would otherwise stack continuations on its
    // local LIFO, and under sustained readiness the oldest connection
    // starves (the same reason yielded tasks force-inject).
    wake_task_with_policy(ex, id, 0, 1, 0, 1);
}

// Abort/self-requeue after a park lost to the wake token: caller holds the
// owner shard lock.
static void
park_requeue_locked(const rt_executor* ex, rt_shard* owner_shard, rt_task* task, waker_key key) {
    rt_trace_spurious_wake_absorbed();
    waker_kind kind = (waker_kind)key.kind;
    int force_inject =
        channel_wake_force_inject_enabled() && (kind == WAKER_CHAN_SEND || kind == WAKER_CHAN_RECV);
    task->park_key = waker_none();
    task->park_prepared = 0;
    task_status_store(task, TASK_READY);
    (void)ready_push_task_locked(ex, owner_shard, task, force_inject, 0, 1);
}

void wake_key_all_with_policy(rt_executor* ex, waker_key key, int front) {
    if (ex == NULL || !waker_valid(key)) {
        return;
    }
    // Collect-then-wake (D5): pop matches under the store's lane, release,
    // then wake each task under its owner's lock. Waking under a held store
    // lock would self-deadlock the same-shard case (same mutex).
    uint64_t inline_batch[16];
    uint64_t* batch = inline_batch;
    size_t batch_cap = sizeof(inline_batch) / sizeof(inline_batch[0]);
    size_t batch_len = 0;
    if (key.kind == WAKER_JOIN) {
        batch_len = rt_waiter_collect_join_waiters(ex, key, &batch, &batch_cap, inline_batch);
    } else {
        rt_shard* store_shard = rt_waiter_key_shard(ex, key);
        if (store_shard != NULL) {
            rt_shard_lock(store_shard);
        }
        rt_waiter_store* store = rt_waiter_store_for_key(ex, key);
        if (store != NULL && store->len > 0) {
            size_t out = 0;
            for (size_t i = 0; i < store->len; i++) {
                waiter w = store->entries[i];
                if (w.key.kind == key.kind && w.key.id == key.id) {
                    if (batch_len == batch_cap) {
                        size_t next_cap = batch_cap * 2;
                        uint64_t* next = (uint64_t*)rt_alloc(
                            (uint64_t)(next_cap * sizeof(uint64_t)), _Alignof(uint64_t));
                        if (next == NULL) {
                            panic_msg("async: wake batch allocation failed");
                            break;
                        }
                        memcpy(next, batch, batch_len * sizeof(uint64_t));
                        if (batch != inline_batch) {
                            rt_free((uint8_t*)batch,
                                    (uint64_t)(batch_cap * sizeof(uint64_t)),
                                    _Alignof(uint64_t));
                        }
                        batch = next;
                        batch_cap = next_cap;
                    }
                    batch[batch_len++] = w.task_id;
                    continue;
                }
                store->entries[out++] = w;
            }
            // No net-key producer exists for this path (scope/blocking keys
            // only here; join keys use the route-lock helper above); net_len
            // and fd-registry bookkeeping stay owned by rt_async_waiter.c
            // removal paths.
            store->len = out;
            // No channel-key producer either -- channel wakes are candidate
            // pops, not key drains. The retire is called anyway so that "every
            // removal retires the pin" holds by enumeration rather than by an
            // argument about who calls this, which is what would have to be
            // re-derived the first time somebody adds a caller.
            rt_channel_key_retired(key, batch_len);
        }
        if (store_shard != NULL) {
            rt_shard_unlock(store_shard);
        }
    }
    if (batch_len > 0) {
        rt_trace_collect_wake_batch();
    }
    for (size_t i = 0; i < batch_len; i++) {
        wake_task_with_policy(ex, batch[i], 0, 0, front, 1);
    }
    if (batch != inline_batch) {
        rt_free((uint8_t*)batch, (uint64_t)(batch_cap * sizeof(uint64_t)), _Alignof(uint64_t));
    }
}

void wake_key_all(rt_executor* ex, waker_key key) {
    wake_key_all_with_policy(ex, key, 0);
}

void park_current(rt_executor* ex, waker_key key) {
    if (ex == NULL || !waker_valid(key) || rt_current_task_id() == 0) {
        return;
    }
    rt_task* task = rt_current_task();
    if (task == NULL || task_status_load(task) == TASK_DONE) {
        return;
    }
    rt_trace_park_attempt();
    rt_shard* owner_shard = rt_task_owner_shard(ex, task);
    if (task_wake_token_exchange(task, 0) != 0) {
        // This park aborts BEFORE the add_waiter below, so the only
        // registration in flight is the one channel_park_prepare_locked
        // (rt_channel_lane.h) appended while it held the channel owner's lock
        // - and that entry carries this park's generation, stamped nonzero.
        // park_requeue_locked clears park_key and park_prepared but not
        // park_seq, so read the generation here, on the parking task's own
        // thread, before the requeue can hand the task to another worker.
        // Without this the entry outlives the park: on the unwinding paths
        // nothing re-prepares that key after the requeue, and mark_done then
        // finds a cleared park_key and has nothing to remove (RV2-DEBT-201).
        uint32_t abort_seq = RT_DEBT201_ABORT_SEQ(task, key);
        rt_shard_lock(owner_shard);
        park_requeue_locked(ex, owner_shard, task, key);
        rt_shard_unlock(owner_shard);
        // After the owner lock is released (never two shard locks), and only
        // for a NONZERO generation. Zero would be an UNQUALIFIED sweep -
        // remove_waiter_from_store_seq treats seq 0 as "match any" - and the
        // prepare_park registrations that also reach this branch (join, scope,
        // remote reply keys) all sit at seq 0, so sweeping them here would be
        // the RV2-DEBT-046 lost-wake shape documented above. A generation-
        // qualified removal cannot eat a fresh registration either: the
        // requeued task can only re-register at a LATER generation.
        // cppcheck-suppress knownConditionTrueFalse
        // Always false in ONE configuration only: the RV2_DEBT_201 negative
        // control defines RT_DEBT201_ABORT_SEQ to a literal zero, which is how
        // it makes this retirement inert. cppcheck enumerates that
        // configuration alongside the real one, where the macro yields
        // task->park_seq for a channel key.
        if (abort_seq != 0) {
            remove_waiter_generation(ex, key, task->id, abort_seq);
        }
        return;
    }
    // Register-then-commit (D5): the registration may fire from here on; the
    // token double-check under the owner lock closes the window. park_key is
    // thread-own while the task is RUNNING on this poller.
    if (!(task->park_prepared && task->park_key.kind == key.kind && task->park_key.id == key.id)) {
        task->park_key = key;
        add_waiter(ex, key, task->id);
    }
    task->park_prepared = 0;
    // This park's generation, read on the parking task's own thread before
    // the requeue can hand the task to another worker.
    uint32_t abort_seq = 0;
    if (key.kind == WAKER_CHAN_SEND || key.kind == WAKER_CHAN_RECV) {
        abort_seq = task->park_seq;
    }
    rt_shard_lock(owner_shard);
    task_status_store(task, TASK_WAITING);
    if (task_wake_token_exchange(task, 0) != 0) {
        park_requeue_locked(ex, owner_shard, task, key);
        rt_shard_unlock(owner_shard);
        // Abort removal happens after the owner lock is released (never two
        // shard locks); a concurrent pop of this entry is absorbed as one
        // spurious wake by the token it re-sets. The removal is qualified by
        // this park's generation: the requeued task can re-register the same
        // channel key on another worker before this lands, and an
        // unqualified removal would eat that fresh registration.
        //
        // And for a key with NO generation it is not attempted at all, for
        // the same reason the first abort branch above refuses it: a join,
        // scope or remote-reply registration sits at seq 0, so the removal
        // would be the unqualified sweep, and the requeued task can already be
        // polling on another worker and have registered the same join key
        // again. Sweeping that fresh entry strands the joiner for good -- its
        // target completes once, the completion drain finds nothing, and no
        // wake ever comes (measured: one hang in ~15 runs of a four-asker
        // cohort on four workers). The stale entry is self-cleaning instead:
        // the drain pops it and the token absorbs the spurious wake.
        if (abort_seq != 0) {
            remove_waiter_generation(ex, key, task->id, abort_seq);
        }
        return;
    }
    rt_shard_unlock(owner_shard);
    rt_trace_park_committed();
    if (waker_is_net(key)) {
        (void)rt_net_wake_poll_for_task_wait_keys(ex, task, key);
    }
    rt_io_poll_nudge(ex);
}
