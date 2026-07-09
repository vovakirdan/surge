#include "rt_async_internal.h"
#include "rt_sync_point.h"

// Async runtime task API and task builtins.

static rt_task* spawn_checkpoint_task_locked(rt_executor* ex);
static void poll_ready_child_inline(rt_executor* ex, rt_task* current, rt_task* target);
static void rt_task_poll_adopt_placement(rt_executor* ex, rt_task* current, const rt_task* target);

void* __task_create(
    uint64_t poll_fn_id,
    void* state) { // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
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
        panic_msg("async: task allocation failed");
        return NULL;
    }
    memset(task, 0, sizeof(rt_task));
    task->id = id;
    task->generation = id;
    task->poll_fn_id = (int64_t)poll_fn_id;
    task->state = state;
    task_status_store(task, TASK_READY);
    task->kind = TASK_KIND_USER;
    task_cancelled_store(task, 0);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);

    rt_task* parent = rt_current_task();
    if (parent != NULL) {
        rt_task_inherit_placement(task, parent);
    }
    rt_task_assign_spawn_owner(task);

    // Spike-proven order: assign owner -> shard-lock -> slot-store(release)
    // -> ready-push -> unlock. The parent's children[] append is nested in
    // the SAME critical section: a fresh child's owner shard always equals
    // its parent's (rt_task_inherit_placement, just above), so this is the
    // identical lock cancel_task now snapshots children[] under (see
    // cancel_task, rt_async_state.c) - no extra lock.
    //
    // Invariant this relies on (write this down precisely - a future
    // rt_task_replace_owner call site, e.g. the join-consume placement adoption
    // path, must fit case (a) below without breaking this):
    // (a) task_add_child always runs on the parent's own executing thread -
    //     spawning a child is a synchronous call the parent makes from
    //     within its own poll, never issued on the parent's behalf by
    //     another thread. So every append to a given parent's children[]
    //     is program-order sequenced with anything else that thread does
    //     to itself, INCLUDING a self-replace of its own owner_shard_id
    //     (today: rt_task_poll_adopt_placement plus the accept
    //     rt_net_place_current_task_on_owner self-place path).
    // (b) this append takes the CHILD's owner shard lock, which
    //     rt_task_inherit_placement (just above) set to the parent's
    //     CURRENT owner_shard_id at this exact spawn - read fresh, not
    //     cached.
    // (c) a reader (cancel_task, rt_async_state.c) holds control plus the
    //     parent's CURRENT owner shard lock. Every rt_task_replace_owner
    //     call today is either a self-replace on the owning thread (case
    //     (a): program order carries prior appends forward to whatever
    //     lock that thread acquires next - happens-before is the
    //     transitive closure of sequenced-before and synchronizes-with, so
    //     even a parent that changes its own owner BETWEEN two of its own
    //     children's spawns leaves both appends visible to a canceller
    //     locking the parent's current owner shard), or a cross-thread
    //     replace targeting a task popped from a waiter store, i.e.
    //     TASK_WAITING - parked, not mid-spawn
    //     (rt_executor_wake_net_waiters_for_key_on_owner, rt_async_waiter.c,
    //     the only such call shape; rt_task_poll_adopt_placement,
    //     net_task_set_ready_accept, and rt_net_place_current_task_on_owner
    //     are the current self-replace shapes - grep-verified in
    //     rt_async_task.c, rt_net_accept_group.c, and rt_async_waiter.c).
    // This does NOT claim a running parent's owner_shard_id never changes
    // (it can, per (a)) - only that no path lets a DIFFERENT thread rewrite
    // it while the owning thread might be mid-append. If a future change
    // ever lets a parent spawn while parked, or lets another thread replace
    // a task's owner while that task is unambiguously running (not
    // self-driven, not popped-from-a-store), this invariant breaks and
    // must be re-derived before relying on it.
    rt_shard* owner_shard = rt_task_owner_shard(ex, task);
    rt_shard_lock(owner_shard);
    rt_task_slot_store(ex, id, task);
    if (parent != NULL) {
        task_add_child(parent, id);
    }
    (void)ready_push_task_locked(ex, owner_shard, task, 0, 0, 1);
    rt_shard_unlock(owner_shard);

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
    // S5-Q2: the wake itself is owner-shard work (wake_task manages its own
    // shard locking, already called control-free elsewhere in the tree); only
    // the rare scope-adoption write stays behind a control fallback. The peek
    // reads only current's own scope_id (thread-local while current is
    // RUNNING, safe without a lock) - target->parent_scope_id is read only
    // under the lock, never as an unsynchronized peek, since Task 9 still has
    // rt_scope_register_child writing it under control.
    const rt_task* current = rt_current_task();
    if (current != NULL && current->scope_id != 0) {
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_HANDLE);
        rt_trace_control_lock_handle_site(RT_CTRL_HANDLE_WAKE);
        if (target->parent_scope_id == 0) {
            const rt_scope* scope = get_scope(ex, current->scope_id);
            if (scope != NULL) {
                target->parent_scope_id = scope->id;
            }
        }
        rt_control_unlock(ex);
    }
    wake_task(ex, target->id, 1);
}

uint8_t rt_task_poll(void* task, uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return 2;
    }
    rt_task* target = task_from_handle(task);
    if (target == NULL) {
        return 2;
    }
    // S5-Q3/rule 2 (Epic 8 Task 7 + Epic 9 DEBT-020): the join register +
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
    if (current_task_cancelled(ex)) {
        return 0;
    }
    if (target->kind == TASK_KIND_USER && task_status_load(target) == TASK_READY &&
        task_enqueued_load(target) != 0 && ready_take_current_local_tail(ex, target->id)) {
        poll_ready_child_inline(ex, current, target);
    }
    if (task_status_load(target) != TASK_WAITING && task_status_load(target) != TASK_DONE) {
        wake_task(ex, target->id, 1);
    }
    if (task_status_load(target) == TASK_DONE) {
        uint8_t kind = target->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
        if (out_bits != NULL) {
            *out_bits = target->result_bits;
        }
        // F2 (Epic 8 Task 11 net-fairness fix): read placement before
        // release, which may free target.
        rt_task_poll_adopt_placement(ex, current, target);
        task_release_lane_aware(ex, target);
        return kind;
    }
    if (target->kind != TASK_KIND_CHECKPOINT) {
        waker_key key = join_key(target->id);
        prepare_park(ex, current, key, 0);
        pending_key = key;
        // Register-then-verify: the target may complete on its own shard
        // between the DONE check above and the registration; its completion
        // drain and this insert serialize on the target owner's store lock,
        // so re-checking after registering closes the stranded-entry race.
        if (task_status_load(target) == TASK_DONE) {
            remove_waiter(ex, key, current->id);
            current->park_prepared = 0;
            current->park_key = waker_none();
            pending_key = waker_none();
            uint8_t kind = target->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
            if (out_bits != NULL) {
                *out_bits = target->result_bits;
            }
            rt_task_poll_adopt_placement(ex, current, target);
            task_release_lane_aware(ex, target);
            return kind;
        }
    }
    return 0;
}

// F2 (Epic 8 Task 11 net-fairness fix, folded into Task 7): a task consuming
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
    // Hard-constraint arm (1) from the Task 11 F2 spec: an explicit control
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

// S5-Q4 (Epic 8 Task 7): runs entirely on the child's owner shard lane, no
// control. The only eligible child is the fresh, just-created child popped
// off the CURRENT WORKER'S OWN local queue tail (ready_take_current_local_tail,
// guarded at the call site above); by construction (rt_task_inherit_placement
// copies the parent's owner shard before publish, Task 6) that child's owner
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
    task_enqueued_store(target, 0);
    task_status_store(target, TASK_RUNNING);
    (void)task_wake_token_exchange(target, 0);
    rt_shard_lock(owner_shard);
    scheduler->running_count++;
    rt_shard_unlock(owner_shard);
    rt_set_current_task(target);

    task_polling_enter(target);
    poll_outcome outcome = poll_task(ex, target);
    task_polling_exit(target);

    rt_shard_lock(owner_shard);
    if (scheduler->running_count > 0) {
        scheduler->running_count--;
    }
    rt_shard_unlock(owner_shard);
    apply_poll_outcome(ex, target, outcome);
    rt_set_current_task(current);
}

void rt_task_await(void* task, uint8_t* out_kind, uint64_t* out_bits) {
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
        rt_done_waiters_decrement_for_external_await(ex);
        if (out_kind != NULL) {
            *out_kind = target->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
        }
        if (out_bits != NULL) {
            *out_bits = target->result_bits;
        }
        task_release(ex, target);
        rt_control_unlock(ex);
        return;
    }
    rt_task* current = rt_current_task();
    run_until_done(ex, target, out_kind, out_bits);
    rt_set_current_task(current);
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

void* rt_task_clone(void* task) {
    rt_task* target = task_from_handle(task);
    if (target == NULL) {
        return NULL;
    }
    // S5-Q6 (Epic 8 Task 7): drops control unconditionally, not a rare
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
        panic_msg("async: task allocation failed");
        return NULL;
    }
    memset(task, 0, sizeof(rt_task));
    task->id = id;
    task->generation = id;
    task_status_store(task, TASK_READY);
    task->kind = kind;
    task->sleep_delay = sleep_delay;
    task_cancelled_store(task, 0);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
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
