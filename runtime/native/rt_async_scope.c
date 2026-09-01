#include "rt_async_internal.h"
#include "rt_scope_membership.h"
#include "rt_sync_point.h"

// Async runtime scope management.
//
// Scope owner lane (S5-Q7/Q8/Q9/Q10/Q11/Q14): scope object
// bookkeeping runs on the scope's PINNED owner shard lane instead of the
// control lane. A scope's owner shard is fixed at rt_scope_enter
// (scope->owner_shard_id) and every scope-object mutation plus the scope_key
// waiter store both serialize on that one shard lock, stable for the scope's
// whole life even if the owner task is later re-placed (F2 placement adoption).
//
// The waiter primitives (prepare_park / add_waiter / remove_waiter /
// wake_key_all) take the key's store shard lock INTERNALLY, so callers must
// hold NO shard lock across them. Scope object mutation therefore runs under
// the pinned shard lock and releases it before any park/wake/cancel, using two
// patterns already blessed elsewhere in this runtime: register-then-verify
// (rt_task_poll) for join_all's park, and mutate-then-wake / snapshot-release-
// walk (cancel_task's own children[] walk, ) for child-done and failfast.
//
// Cancel/failfast walks keep a COUNTED control fallback (S5-Q9);
// re-derivation): cancel_task reads each child's owner_shard_id, which F2
// self-replace (rt_task_replace_owner) writes under the control lane, so a
// control-free walk would race that write and break owner-lock
// invariant (rt_async_task.c). These walks are O(children) on rare
// cancellation, never the steady request completion path.

rt_shard* rt_scope_owner_shard(rt_executor* ex, const rt_scope* scope) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    if (scope == NULL) {
        return rt_runtime_shard0(runtime);
    }
    rt_shard* shard = rt_runtime_shard(runtime, scope->owner_shard_id);
    return shard != NULL ? shard : rt_runtime_shard0(runtime);
}

// Snapshot the two answers join_all gives -- how many children are still live
// and whether fail-fast fired -- under the pinned shard lock, TOGETHER. A
// function boundary here is deliberate: both fields are mutated concurrently
// under this lock by child-done on other threads, so a re-read after a lock
// release genuinely observes new values (the register-then-verify re-check
// below relies on it). Reading them apart is the RV2-DEBT-261 window:
// scope_on_child_done retires the cancelled child from the count in the same
// critical section that sets the flag, so a count read after it paired with a
// flag read before it says "drained, not fail-fast" -- and the @failfast
// block resolves Success after a child was cancelled.
static size_t scope_join_snapshot_locked(rt_shard* pinned, const rt_scope* scope, bool* failfast) {
    rt_shard_lock(pinned);
    size_t active = scope->active_children;
    if (failfast != NULL) {
        *failfast = scope->failfast_triggered ? true : false;
    }
    rt_shard_unlock(pinned);
    return active;
}

// Snapshot the scope's children under the pinned shard lock, then cancel each
// under the control lane (caller holds control). Never holds the pinned shard
// lock across cancel_task (which takes a child's own owner shard lock), so the
// lane order stays control -> at most one shard lock; never two shard locks.
void scope_cancel_children_controlled(rt_executor* ex, const rt_scope* scope) {
    if (ex == NULL || scope == NULL) {
        return;
    }
    rt_shard* pinned = rt_scope_owner_shard(ex, scope);
    uint64_t inline_children[8];
    uint64_t* children = inline_children;
    size_t count = 0;
    rt_shard_lock(pinned);
    count = scope->children_len;
    if (count > 8) {
        children = (uint64_t*)rt_alloc(count * sizeof(uint64_t), _Alignof(uint64_t));
        if (children == NULL) {
            rt_shard_unlock(pinned);
            fatal_oom_msg("async: scope cancel snapshot allocation failed");
            return;
        }
    }
    if (count > 0) {
        memcpy(children, scope->children, count * sizeof(uint64_t));
    }
    rt_shard_unlock(pinned);
    for (size_t i = 0; i < count; i++) {
        cancel_task(ex, children[i]);
    }
    if (children != inline_children) {
        rt_free((uint8_t*)children, count * sizeof(uint64_t), _Alignof(uint64_t));
    }
}

void* rt_scope_enter(bool failfast) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    rt_task* owner = rt_current_task();
    if (owner == NULL || rt_current_task_id() == 0) {
        panic_msg("rt_scope_enter without current task");
        return NULL;
    }
    // Owner-lane publish (S5-Q7 realization B, mirroring __task_create): id is a
    // lock-free fetch_add; the segmented scope table only takes control on rare
    // segment growth; the slot publish is a release store into a never-moved
    // slot and owner->scope_id is a thread-own write on the RUNNING owner. No
    // control lock on the steady path.
    uint64_t id = atomic_fetch_add_explicit(&ex->next_scope_id, 1, memory_order_relaxed);
    if (rt_scope_table_segment_missing(ex, id)) {
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_SCOPE);
        ensure_scope_cap(ex, id);
        rt_control_unlock(ex);
    }
    rt_scope* scope = (rt_scope*)rt_alloc(sizeof(rt_scope), _Alignof(rt_scope));
    if (scope == NULL) {
        fatal_oom_msg("async: scope allocation failed");
        return NULL;
    }
    memset(scope, 0, sizeof(rt_scope));
    scope->id = id;
    scope->owner = owner->id;
    // Pin the scope to the entering task's owner shard for its whole life.
    scope->owner_shard_id = owner->owner_shard_valid != 0 ? owner->owner_shard_id : 0;
    scope->failfast = failfast ? 1 : 0;
    rt_scope_slot_store(ex, id, scope);
    owner->scope_id = id;
    return (void*)(uintptr_t)id;
}

void rt_scope_register_child(const void* scope_handle, void* task) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        return;
    }
    uint64_t child_id = task_id_from_handle(task);
    rt_task* child = get_task(ex, child_id);
    if (child == NULL) {
        return;
    }
    rt_shard* pinned = rt_scope_owner_shard(ex, scope);
    // Steady path (S5-Q8): a live child registers under the pinned shard lock.
    // The child inherits the scope owner's placement at spawn and this is a
    // synchronous call from the owner's own poll right after __task_create, so
    // the child's owner shard == the scope's pinned shard here (it cannot have
    // re-placed yet). That makes the pinned shard lock == the child's owner
    // shard lock, so the scope's accounting and the child's own mark_done
    // child-done serialize on ONE lock, with no control lane.
    //
    // The lock is NOT what decides membership, though, and it cannot be: the
    // completion has to read this child's scope identity before it knows which
    // lock to take, so it reads it holding none. The claim below is what
    // decides, in one read-modify-write against the completion's own -- whoever
    // runs second sees the first. Losing it means the child completed before
    // this registration reached it, which is the late path further down: the
    // scope must never count a child whose completion has already been and gone.
    rt_shard_lock(pinned);
    if (child->scope_registered) {
        rt_shard_unlock(pinned);
        return;
    }
    int claimed = RT_SCOPE_MEMBERSHIP_CLAIM(child, scope_id);
    RT_SYNC_POINT(SP_SCOPE_MEMBERSHIP_DECIDED_BEFORE_PUBLISH);
    if (claimed) {
        scope_add_child(scope, child_id);
        RT_SCOPE_MEMBERSHIP_PUBLISH(child, scope_id);
        child->scope_registered = 1;
        scope->active_children++;
        rt_shard_unlock(pinned);
        return;
    }
    rt_shard_unlock(pinned);
    // Rare late-registration (S5-Q9): the child completed before this claim
    // reached it, so it is not a member and nothing will retire it. Only a
    // cancelled child in a failfast scope acts - it triggers failfast and
    // cancels the already-registered siblings. The walk needs the control lane
    // (see file header re-derivation). Reading the child's committed result
    // here is ordered by the claim itself: the completion's read-modify-write
    // released everything it had written, and the acquire on this failed claim
    // is what makes it visible.
    if (child->result_kind != TASK_RESULT_CANCELLED || !scope->failfast) {
        return;
    }
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_SCOPE);
    }
    uint64_t owner_to_wake = 0;
    rt_shard_lock(pinned);
    if (!scope->failfast_triggered) {
        scope->failfast_triggered = 1;
        scope->failfast_child = child_id;
        owner_to_wake = scope->owner;
    }
    rt_shard_unlock(pinned);
    if (owner_to_wake != 0) {
        scope_cancel_children_controlled(ex, scope);
        wake_task(ex, owner_to_wake, 1);
    }
    if (need_control) {
        rt_control_unlock(ex);
    }
}

void rt_scope_cancel_all(const void* scope_handle) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    // Cross-owner cancel walk needs the control lane (re-derivation, file
    // header). Rare: explicit scope cancellation, not the steady path.
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_SCOPE);
    }
    const rt_scope* scope = get_scope(ex, scope_id);
    if (scope != NULL) {
        scope_cancel_children_controlled(ex, scope);
    }
    if (need_control) {
        rt_control_unlock(ex);
    }
}

bool rt_scope_join_all(const void* scope_handle, uint64_t* pending, bool* failfast) {
    // Both answers are written before anything can return, so no exit can leave
    // either one holding what the caller's stack happened to contain. The two
    // early exits below say "drained" about a scope that is gone, and a scope
    // that is gone has no fail-fast outstanding and nothing pending.
    if (pending != NULL) {
        *pending = 0;
    }
    if (failfast != NULL) {
        *failfast = false;
    }
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return true;
    }
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    const rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        return true;
    }
    rt_shard* pinned = rt_scope_owner_shard(ex, scope);
    size_t active = scope_join_snapshot_locked(pinned, scope, failfast);
    if (pending != NULL) {
        *pending = 0;
    }
    if (active == 0) {
        return true;
    }
    rt_task* current = rt_current_task();
    if (current == NULL) {
        return false;
    }
    // Register-then-verify (S5-Q10, mirroring rt_task_poll's join register):
    // park on scope_key (routed to this pinned shard's store), then re-check
    // active_children under the pinned lock. prepare_park takes the pinned
    // shard lock internally, so it must run with no shard lock held. A
    // child-done that drives active to 0 between the read above and this
    // registration is caught by the re-check; one that lands after wakes the
    // registration. A RUNNING owner double-woken here is absorbed by
    // wake_task_on_shard_locked's status gate.
    //
    // The verify re-reads the fail-fast flag WITH the count (RV2-DEBT-261):
    // the child-done it catches may be the cancelled one that fired fail-fast,
    // and its answer is only whole when both fields come from after it. The
    // negative-control toggle drops the flag from this read and restores the
    // window the sync point below holds open.
    waker_key key = scope_key(scope_id);
    prepare_park(ex, current, key, 0);
    pending_key = key;
    RT_SYNC_POINT(SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY);
    size_t active_after =
        scope_join_snapshot_locked(pinned, scope, RT_DEBT261_VERIFY_FAILFAST_OUT(failfast));
    if (active_after == 0) {
        remove_waiter(ex, key, current->id);
        current->park_prepared = 0;
        current->park_key = waker_none();
        pending_key = waker_none();
        return true;
    }
    return false;
}

void rt_scope_exit(const void* scope_handle) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        return;
    }
    rt_shard* pinned = rt_scope_owner_shard(ex, scope);
    rt_shard_lock(pinned);
    if (scope->active_children > 0) {
        rt_shard_unlock(pinned);
        panic_msg("async: scope exit with active children");
        return;
    }
    scope_exit_locked(ex, scope);
    rt_shard_unlock(pinned);
}

// Caller holds the scope's pinned owner shard lock (rt_scope_exit) or the
// control lane over that shard (apply_poll_outcome cancelled teardown). Frees
// only after active_children == 0. The slot is cleared (release NULL) before
// the struct is freed; scope ids are monotonic and never reused, so a late
// get_scope for this id resolves to NULL rather than a recycled scope, and by
// the scope lifetime rule no scope_key routing references it after this point.
void scope_exit_locked(rt_executor* ex, rt_scope* scope) {
    if (ex == NULL || scope == NULL) {
        return;
    }
    uint64_t scope_id = scope->id;
    if (scope->owner != 0) {
        rt_task* owner = get_task(ex, scope->owner);
        if (owner != NULL && owner->scope_id == scope_id) {
            owner->scope_id = 0;
        }
    }
    rt_scope_slot_store(ex, scope_id, NULL);
    if (scope->children != NULL && scope->children_cap > 0) {
        rt_free((uint8_t*)scope->children,
                (uint64_t)scope->children_cap * (uint64_t)sizeof(uint64_t),
                _Alignof(uint64_t));
    }
    rt_free((uint8_t*)scope, sizeof(rt_scope), _Alignof(rt_scope));
}

// Completion-side scope bookkeeping (S5-Q8), called from mark_done after the
// TASK_DONE store. task is the completing child. Same-owner non-failfast
// child-done runs control-free under the pinned shard lock (== the child's own
// owner shard here); cross-owner (re-placed child) or failfast-triggering
// completions take the counted control lane (file header re-derivation).
void scope_on_child_done(rt_executor* ex, rt_task* task, uint8_t result_kind) {
    if (ex == NULL || task == NULL) {
        return;
    }
    // Take this task out of the membership race before anything else, and take
    // it with one read-modify-write. This read cannot be made under the scope's
    // lock -- its answer is which scope's lock to take -- so it is the claim
    // word itself that has to be the serializer. A task that answers NONE here
    // never belonged to a scope, and the word is now sealed: a registration
    // still in flight for it loses its claim and reports the completion itself,
    // instead of counting a child that has already finished. Reading and
    // returning on a plain zero here was a lost fail-fast and a scope that
    // never drains.
    uint64_t psid = RT_SCOPE_MEMBERSHIP_TAKE(task);
    RT_SYNC_POINT(SP_SCOPE_CHILD_DONE_AFTER_MEMBERSHIP_TAKE);
    if (psid == RT_SCOPE_CLAIM_NONE) {
        return;
    }
    rt_shard* child_shard = rt_task_owner_shard(ex, task);
    rt_shard_lock(child_shard);
    rt_scope* scope = get_scope(ex, psid);
    if (scope == NULL) {
        rt_shard_unlock(child_shard);
        return;
    }
    int same_owner = scope->owner_shard_id == task->owner_shard_id;
    int may_failfast =
        result_kind == TASK_RESULT_CANCELLED && scope->failfast && !scope->failfast_triggered;
    if (same_owner && !may_failfast) {
        int wake_owner = 0;
        if (task->scope_registered) {
            (void)scope_remove_child(scope, task->id);
            if (scope->active_children > 0) {
                scope->active_children--;
            }
            wake_owner = scope->active_children == 0;
            task->scope_registered = 0;
        }
        rt_shard_unlock(child_shard);
        if (wake_owner) {
            wake_key_all(ex, scope_key(scope->id));
        }
        return;
    }
    rt_shard_unlock(child_shard);
    // Rare: cross-owner (re-placed child) or failfast-triggering completion.
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_SCOPE);
    }
    scope = get_scope(ex, psid);
    if (scope != NULL) {
        rt_shard* pinned = rt_scope_owner_shard(ex, scope);
        uint64_t owner_to_wake = 0;
        int wake_owner = 0;
        rt_shard_lock(pinned);
        if (result_kind == TASK_RESULT_CANCELLED && scope->failfast && !scope->failfast_triggered) {
            scope->failfast_triggered = 1;
            scope->failfast_child = task->id;
            owner_to_wake = scope->owner;
        }
        if (task->scope_registered) {
            (void)scope_remove_child(scope, task->id);
            if (scope->active_children > 0) {
                scope->active_children--;
            }
            wake_owner = scope->active_children == 0;
            task->scope_registered = 0;
        }
        rt_shard_unlock(pinned);
        if (owner_to_wake != 0) {
            scope_cancel_children_controlled(ex, scope);
            wake_task(ex, owner_to_wake, 1);
        }
        if (wake_owner) {
            wake_key_all(ex, scope_key(scope->id));
        }
    }
    if (need_control) {
        rt_control_unlock(ex);
    }
}
