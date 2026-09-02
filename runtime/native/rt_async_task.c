#include "rt_async_internal.h"
#include "rt_remote_task.h"
#include "rt_scope_membership.h"
#include "rt_sync_point.h"

// Async runtime task API and task builtins.

static rt_task* spawn_checkpoint_task_locked(rt_executor* ex);
static void poll_ready_child_inline(rt_executor* ex, rt_task* current, rt_task* target);
static void publish_created_task(rt_executor* ex, rt_task* task, rt_task* parent);
static void rt_task_poll_adopt_placement(rt_executor* ex, rt_task* current, const rt_task* target);

void* __task_create( // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
    uint64_t poll_fn_id,
    void* state,
    const rt_value_ops* result_ops) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    uint64_t id = atomic_fetch_add_explicit(&ex->next_id, 1, memory_order_relaxed);
    if (rt_task_table_segment_missing(ex, id)) {
        // Segment growth is the one rare, amortized control-lane event left
        // on the create path (S5-Q1 realization B): once a segment exists,
        // every other id in it needs no control acquisition at all.
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_CREATE);
        ensure_task_cap(ex, id);
        rt_control_unlock(ex);
    }
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        fatal_oom_msg("async: task allocation failed");
        return NULL;
    }
    memset(task, 0, sizeof(rt_task));
    if (rt_value_cell_bind(&task->result, result_ops) != RT_SLOT_CONTROL_OK) {
        rt_free((uint8_t*)task, sizeof(rt_task), _Alignof(rt_task));
        panic_msg("async: a task's result storage could not be reserved");
        return NULL;
    }
    task->id = id;
    task->generation = id;
    task->poll_fn_id = (int64_t)poll_fn_id;
    task->state = state;
    task_status_store(task, TASK_READY);
    task->kind = TASK_KIND_USER;
    task_cancel_gate_init(task);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->remote_handle_state, 0, memory_order_relaxed);
    atomic_store_explicit(&task->far_task_result_state, 0, memory_order_relaxed);
    atomic_store_explicit(&task->far_task_result_lease, NULL, memory_order_relaxed);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    rt_task_entitlements_init(&task->entitlements);

    rt_task* parent = rt_current_task();
    publish_created_task(ex, task, parent);

    // Lane-aware compensation-worker check, mirroring wake_task's identical
    // pattern (rt_async_state.c): only sync-channel-blocked-worker scenarios
    // need this, so the common case (no compat workers parked) costs one
    // atomic load and no lock.
    const rt_channel_blocking_compat* compat = rt_executor_channel_blocking_compat_const(ex);
    if (compat != NULL &&
        atomic_load_explicit(&compat->channel_blocked_workers, memory_order_acquire) > 0) {
        int need_control = !rt_lane_holds_control();
        if (need_control) {
            rt_control_lock(ex);
        }
        maybe_start_compensation_worker_locked(ex);
        if (need_control) {
            rt_control_unlock(ex);
        }
    }
    return task;
}

static void publish_created_task(rt_executor* ex, rt_task* task, rt_task* parent) {
    if (parent != NULL) {
        rt_task_inherit_placement(task, parent);
    }
    task->creation_scope_key = parent != NULL ? parent->active_scope_key : waker_none();
    // A scoped GENERIC child runs on the scope's shard, so that its whole
    // lifetime -- membership, completion, cancellation -- stays on one lane. A
    // CONNECTION child does not move: it was placed on the shard that owns its
    // fd, and a task that touches a connection from any other shard is refused
    // (non_owner_conn_denied). Re-placing it here put every HTTP handler
    // spawned inside a scope on the wrong shard at SURGE_SHARDS>1, and its
    // client's read timed out; the accept-ownership ruling is that the fd owner
    // decides, and no hot path re-places a connection task.
    if (waker_valid(task->creation_scope_key) &&
        task->owner_shard_id != task->creation_scope_key.owner_shard_id &&
        task->placement_class != TASK_PLACEMENT_CONNECTION) {
        rt_task_set_placement(task, task->creation_scope_key.owner_shard_id, task->placement_class);
    }
    rt_task_assign_spawn_owner(task);

    // Creation seals provenance and publishes scope membership/count before
    // slot or ready publication. The scope's records live on the scope's own
    // shard, which is the task's shard for a generic child and may not be for
    // a connection child; the separate parent children[] relation is protected
    // by the parent's current owner shard. Each lane is taken on its own and
    // released before the next (never two shard locks), and membership is
    // published before the task is, whichever lane it lands on.
    //
    // The parent-lane snapshot relies on spawn being a synchronous action of
    // the RUNNING parent. A self-replace is sequenced with this read; today's
    // cross-thread replace targets only a WAITING task, which cannot be in this
    // call. cancel_task snapshots children[] under the same parent lane. If a
    // future path can re-place a RUNNING task from another thread, this lock
    // choice must be re-derived.
    rt_shard* owner_shard = rt_task_owner_shard(ex, task);
    rt_shard* parent_shard = parent != NULL ? rt_task_owner_shard(ex, parent) : NULL;
    rt_shard* scope_shard = owner_shard;
    if (waker_valid(task->creation_scope_key)) {
        rt_shard* by_key =
            rt_runtime_shard(rt_executor_runtime(ex), task->creation_scope_key.owner_shard_id);
        if (by_key != NULL) {
            scope_shard = by_key;
        }
    }
    if (parent != NULL && parent_shard != owner_shard) {
        rt_shard_lock(parent_shard);
        task_add_child(parent, task->id);
        rt_shard_unlock(parent_shard);
    }
    if (scope_shard != owner_shard) {
        rt_shard_lock(scope_shard);
        rt_scope_publish_creation_locked(ex, task);
        rt_shard_unlock(scope_shard);
    }
    rt_shard_lock(owner_shard);
    if (scope_shard == owner_shard) {
        rt_scope_publish_creation_locked(ex, task);
    }
    RT_SYNC_POINT(SP_SCOPE_MEMBERSHIP_DECIDED_BEFORE_PUBLISH);
    rt_task_slot_store(ex, task->id, task);
    if (parent != NULL && parent_shard == owner_shard) {
        task_add_child(parent, task->id);
    }
    (void)ready_push_task_locked(ex, owner_shard, task, 0, 0, 1);
    rt_shard_unlock(owner_shard);
}

void* __task_state(void) { // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
    rt_task* task = rt_current_task();
    if (task == NULL) {
        panic_msg("async: __task_state without current task");
        return NULL;
    }
    void* state = task->state;
    task->state = NULL;
    return state;
}

void rt_task_wake(void* task) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    rt_task* target = task_from_handle(task);
    if (target == NULL || task_status_load(target) == TASK_DONE) {
        return;
    }
    // Wake is scheduling only. Scope provenance was sealed by creation before
    // publication; in particular, waking a foreign task never adopts it.
    const rt_task* current = rt_current_task();
    if (current != NULL && waker_valid(current->active_scope_key)) {
        RT_SCOPE_WAKE_PROVENANCE(target, current->active_scope_key);
    }
    wake_task(ex, target->id, 1);
}

uint8_t rt_task_poll(void* task, void* out_dst) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return 2;
    }
    rt_task* target = task_from_handle(task);
    if (target == NULL) {
        return 2;
    }
    // S5-Q3/rule 2 (+ DEBT-020): the join register +
    // result read run on the target task's join-owner shard route, no control.
    // add/remove/pop for WAKER_JOIN resolve join_owner_shard_id, lock that
    // shard, and revalidate the route under the lock. mark_done's completion
    // drain pops that same routed store under that same shard lock.
    if (rt_current_task_id() == 0) {
        panic_msg("async poll outside task");
        return 2;
    }
    if (rt_current_task_id() == target->id) {
        panic_msg("task cannot await itself");
        return 2;
    }
    rt_task* current = rt_current_task();
    if (current == NULL) {
        panic_msg("async: missing current task");
        return 2;
    }
    // ready_claim_current_local_tail both takes the child off this worker's
    // local tail and claims it for the poll below, in one critical section, so
    // a true return means this thread already owns it: RUNNING, unqueued, wake
    // token consumed.
    if (target->kind == TASK_KIND_USER && task_status_load(target) == TASK_READY &&
        task_enqueued_load(target) != 0 && ready_claim_current_local_tail(ex, target->id)) {
        poll_ready_child_inline(ex, current, target);
    }
    if (task_status_load(target) != TASK_WAITING && task_status_load(target) != TASK_DONE) {
        wake_task(ex, target->id, 1);
    }
    if (task_status_load(target) == TASK_DONE) {
        uint8_t kind = rt_far_task_take_result(target, current, out_dst);
        if (kind == 0) {
            // The last asker, with a reader still copying out of the slot: park
            // on the join key -- the reader that retires last wakes it -- and
            // ask once more after registering, so a reader that retired in
            // between cannot strand the park.
            waker_key key = join_key(target->id);
            prepare_park(ex, current, key, 0);
            pending_key = key;
            kind = rt_far_task_take_result(target, current, out_dst);
            if (kind == 0) {
                return 0;
            }
            remove_waiter(ex, key, current->id);
            current->park_prepared = 0;
            current->park_key = waker_none();
            pending_key = waker_none();
        }
        // F2 (net-fairness fix): read placement before
        // release, which may free target.
        rt_task_poll_adopt_placement(ex, current, target);
        task_release_lane_aware(ex, target);
        return kind;
    }
    // A cancelled awaiter is answered AFTER the target has been asked, not
    // before. Asking "am I cancelled?" first loses a result that was already
    // sitting in the target: the awaiter unwinds without ever collecting it,
    // and the arm that would have read it never runs. The VM has no such check
    // in its poll at all (pollTask, internal/vm/async_runtime.go) - it delivers
    // a DONE target unconditionally and lets the SUSPENSION carry the
    // cancellation instead (execTermAsyncYield, internal/vm/vm_terminator.go).
    //
    // The check stays here rather than going away entirely because what it
    // guards is the registration below: a cancelled task must not leave a join
    // waiter behind, since it will never be woken to remove it.
    if (current_task_cancelled(ex)) {
        return 0;
    }
    // Every kind registers, checkpoints included. Leaving `pending_key`
    // invalid is not "no park needed" -- it is the yield path: the caller
    // branches to PendBB on a 0 return (emit_async.go:190), rt_async_yield
    // finds no valid key and returns POLL_YIELDED (rt_async_poll.c:304), and
    // apply_poll_outcome pushes the awaiter straight back onto the inject
    // queue (rt_task_complete.c:484). So an awaiter that does not register
    // does not wait for its target -- it re-enters the ready queue on every
    // turn and asks again, which is what the worker's outer loop was measured
    // doing 99,000 times a second while the row made no progress.
    //
    // Nothing about a checkpoint target needed the exemption: mark_done ends
    // every completion with wake_key_all_with_policy(join_key(id))
    // (rt_task_complete.c:379) with no test on kind, so a join waiter left
    // against a checkpoint is woken by exactly the same drain as any other.
    waker_key key = join_key(target->id);
    prepare_park(ex, current, key, 0);
    pending_key = key;
    RT_SYNC_POINT(SP_TASK_POLL_AFTER_JOIN_REGISTER);
    // Register-then-verify: the target may complete on its own shard
    // between the DONE check above and the registration; its completion
    // drain and this insert serialize on the target owner's store lock,
    // so re-checking after registering closes the stranded-entry race.
    if (task_status_load(target) == TASK_DONE) {
        uint8_t kind = rt_far_task_take_result(target, current, out_dst);
        if (kind == 0) {
            // The join waiter is already registered: stay parked on it
            // until the reader that retires last wakes this asker.
            return 0;
        }
        remove_waiter(ex, key, current->id);
        current->park_prepared = 0;
        current->park_key = waker_none();
        pending_key = waker_none();
        rt_task_poll_adopt_placement(ex, current, target);
        task_release_lane_aware(ex, target);
        return kind;
    }
    return 0;
}

// F2 (net-fairness fix, folded into ): a task consuming
// a DONE child carrying TASK_PLACEMENT_CONNECTION adopts the child's
// placement, so the durable request pipeline follows the accepting shard
// instead of staying at the parent's spawn-time owner (see
// docs/runtime-v2-epics/08-tasks/07-join-poll-and-handle-lifetime.md).
//
// Invariant (R1, review requirement): the caller must hold NO shard lock
// here. rt_task_replace_owner takes the control lane, and control-after-
// shard is a lane-order violation rt_lane.c asserts against; both call sites
// in rt_task_poll reach this only after any shard lock taken earlier in that
// branch (inside add_waiter/remove_waiter) has already been released by the
// callee.
//
// current is the task RUNNING on this thread - reading its placement_class/
// owner_shard_id and then having rt_task_replace_owner write those same
// fields is a self-read/self-write from the task's own executing thread,
// the same "self-replace on RUNNING task" shape __task_create's invariant
// comment above already anticipated. target's placement fields are read
// only after observing TASK_DONE (acquire-loaded by the caller): no code
// path calls rt_task_replace_owner on a DONE task (the two self-replace
// sites act on the calling thread's own RUNNING task; the one cross-thread
// site targets a task popped from a waiter store, i.e. TASK_WAITING), so
// target's placement is frozen from the moment it went DONE.
static void rt_task_poll_adopt_placement(rt_executor* ex, rt_task* current, const rt_task* target) {
    if (current == NULL || target == NULL || target->placement_class != TASK_PLACEMENT_CONNECTION ||
        target->owner_shard_valid == 0) {
        return;
    }
    if (current->placement_class == TASK_PLACEMENT_CONNECTION &&
        current->owner_shard_id == target->owner_shard_id) {
        return;
    }
    // Hard-constraint arm (1) from the F2 spec: an explicit control
    // fallback, counted under a named site, permitted because adoption is
    // O(connections) (once per accept/bootstrap), never per-request steady
    // state once parent and child already share placement (the guard above).
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_JOIN_POLL);
    }
    rt_task_replace_owner(ex, current, target->owner_shard_id, TASK_PLACEMENT_CONNECTION);
    rt_trace_placement_adoption();
    if (need_control) {
        rt_control_unlock(ex);
    }
}

// S5-Q4: runs entirely on the child's owner shard lane, no
// control. The only eligible child is the fresh, just-created child popped
// off the CURRENT WORKER'S OWN local queue tail (ready_claim_current_local_tail,
// guarded at the call site above); by construction (rt_task_inherit_placement
// copies the parent's owner shard before publish, ) that child's owner
// shard equals this worker's shard, and it is reachable from no other
// queue - no other thread can be concurrently running or inline-polling it.
// If a future change ever lets a child be inline-polled off a queue other
// than the popping worker's own local tail, this invariant must be
// re-derived before relying on it.
static void poll_ready_child_inline(rt_executor* ex, rt_task* current, rt_task* target) {
    if (ex == NULL || current == NULL || target == NULL) {
        return;
    }
    rt_shard* owner_shard = rt_task_owner_shard(ex, target);
    rt_scheduler* scheduler = rt_shard_scheduler(owner_shard);
    if (owner_shard == NULL || scheduler == NULL) {
        return;
    }
    // The claim already happened, under the lock that took this child off the
    // queue (ready_claim_current_local_tail, rt_ready_queue.c): the child is
    // RUNNING, unqueued and its wake token consumed before this thread got
    // here, so nothing between the take and this poll can hand it to a second
    // worker.
    rt_shard_lock(owner_shard);
    scheduler->running_count++;
    rt_shard_unlock(owner_shard);
    rt_set_current_task(target);

    task_polling_enter(target, POLL_SITE_INLINE_CHILD);
    poll_outcome outcome = poll_task(ex, target);
    task_polling_exit(target);

    // Publish before dropping the count: apply_poll_outcome is what enqueues
    // this turn's successor, and an idle sample taken in between reads the
    // executor as idle and jumps the virtual clock.
    apply_poll_outcome(ex, target, outcome);
    rt_shard_lock(owner_shard);
    if (scheduler->running_count > 0) {
        scheduler->running_count--;
    }
    rt_shard_unlock(owner_shard);
    rt_set_current_task(current);
}

void rt_task_await(void* task, uint8_t* out_kind, void* out_dst) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    rt_task* target = task_from_handle(task);
    if (target == NULL) {
        return;
    }
    if (rt_worker_count() > 1) {
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_AWAIT_COMPAT);
        if (task_status_load(target) != TASK_WAITING && task_status_load(target) != TASK_DONE) {
            wake_task(ex, target->id, 1);
        }
        rt_done_waiters_increment_for_external_await(ex);
        RT_SYNC_POINT(SP_AWAIT_AFTER_INCREMENT);
        while (rt_task_status_load_for_external_await(target) != TASK_DONE) {
            RT_SYNC_POINT(SP_AWAIT_BEFORE_DONECV_WAIT);
            pthread_cond_wait(&ex->done_cv, &ex->lock);
        }
        rt_control_unlock(ex);
        // The take MOVES or CLONES the value, and both run generated code that
        // may not run under a runtime lock -- a user __clone under control is
        // the §8 P2 failure, and the detached helpers refuse it rather than
        // letting it deadlock. The target is still alive across the gap: this
        // caller holds a handle reference, released below.
        //
        // A take answered 0 is the last asker finding a reader still out. The
        // done_waiters count taken above is still held, so the reader's
        // retirement broadcasts done_cv under control; checking the reader
        // count under the same lock, and only then waiting, is what keeps that
        // broadcast from slipping past this thread.
        uint8_t kind = rt_far_task_take_result(target, rt_current_task(), out_dst);
        while (kind == 0) {
            rt_control_lock(ex);
            while (!rt_task_entitlement_move_ready(target)) {
                pthread_cond_wait(&ex->done_cv, &ex->lock);
            }
            rt_control_unlock(ex);
            kind = rt_far_task_take_result(target, rt_current_task(), out_dst);
        }
        if (out_kind != NULL) {
            *out_kind = kind;
        }
        rt_control_lock(ex);
        rt_done_waiters_decrement_for_external_await(ex);
        task_release(ex, target);
        rt_control_unlock(ex);
        return;
    }
    rt_task* current = rt_current_task();
    run_until_done(ex, target, out_kind);
    rt_set_current_task(current);
    uint8_t kind = rt_far_task_take_result(target, current, out_dst);
    if (kind == 0) {
        // One thread polls every task to completion here, and a reader
        // duplicates without yielding, so a reader cannot be out.
        panic_msg("async: a single-threaded await found a clone reader out");
        return;
    }
    if (out_kind != NULL) {
        *out_kind = kind;
    }
    rt_control_lock(ex);
    task_release(ex, target);
    rt_control_unlock(ex);
}

void rt_task_cancel(void* task) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    const rt_task* target = task_from_handle(task);
    if (target == NULL) {
        return;
    }
    rt_control_lock(ex);
    rt_trace_control_lock_site(RT_CTRL_SITE_HANDLE);
    rt_trace_control_lock_handle_site(RT_CTRL_HANDLE_CANCEL);
    cancel_task(ex, target->id);
    rt_control_unlock(ex);
}

void* rt_task_clone(void* task, rt_value_clone_init_fn duplicate) {
    rt_task* target = task_from_handle(task);
    if (target == NULL) {
        return NULL;
    }
    // One more handle that can ask, recorded under the owner lock together
    // with the recipe a second asker is served by. A far-carried result keeps
    // its once-only lease answer, which refuses a second asker rather than
    // reclaiming under one.
    rt_task_entitlement_clone(ensure_exec(), target, duplicate);
    // S5-Q6: drops control unconditionally, not a rare
    // fallback - task_add_ref is a relaxed atomic increment, and the caller
    // already holds a live handle to target (the handle being cloned), so
    // handle_refs >= 1 before this call; the free rule (free only at
    // refs 1->0 && TASK_DONE) can never race a live-handle clone to zero.
    task_add_ref(target);
    return target;
}

static rt_task* spawn_internal_task_locked(rt_executor* ex, uint8_t kind, uint64_t sleep_delay) {
    if (ex == NULL) {
        return NULL;
    }
    uint64_t id = ex->next_id++;
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        fatal_oom_msg("async: task allocation failed");
        return NULL;
    }
    memset(task, 0, sizeof(rt_task));
    task->id = id;
    task->generation = id;
    task_status_store(task, TASK_READY);
    task->kind = kind;
    task->sleep_delay = sleep_delay;
    task_cancel_gate_init(task);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->remote_handle_state, 0, memory_order_relaxed);
    atomic_store_explicit(&task->far_task_result_state, 0, memory_order_relaxed);
    atomic_store_explicit(&task->far_task_result_lease, NULL, memory_order_relaxed);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    rt_task_entitlements_init(&task->entitlements);
    rt_task_inherit_placement(task, rt_current_task());
    rt_task_assign_spawn_owner(task);
    rt_task_slot_store(ex, id, task);
    ready_push(ex, id);
    return task;
}

static rt_task* spawn_checkpoint_task_locked(rt_executor* ex) {
    return spawn_internal_task_locked(ex, TASK_KIND_CHECKPOINT, 0);
}

rt_task* rt_spawn_sleep_task_locked(rt_executor* ex, uint64_t delay) {
    return spawn_internal_task_locked(ex, TASK_KIND_SLEEP, delay);
}

void* checkpoint(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    rt_control_lock(ex);
    rt_task* task = spawn_checkpoint_task_locked(ex);
    rt_control_unlock(ex);
    return task;
}

void* rt_sleep(uint64_t ms) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    rt_control_lock(ex);
    rt_task* task = rt_spawn_sleep_task_locked(ex, ms);
    rt_control_unlock(ex);
    return task;
}
