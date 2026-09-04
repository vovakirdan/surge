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
//
// Owner ruling 2026-09-02 (Р6): the pinned owner shard lock is the ONLY
// serializer of a scope's count, fail-fast flag and child list, and every
// write of them runs on the owner lane. A completion on another shard never
// takes that lock from its own lane and never goes through the control lane
// to reach the scope: it publishes a scope event into the owner shard's
// inbound control lane (rt_scope_event.c) and the owner lane applies it in
// the same one critical section a same-shard completion uses.

rt_shard* rt_scope_owner_shard(rt_executor* ex, const rt_scope* scope) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    if (scope == NULL) {
        return rt_runtime_shard0(runtime);
    }
    rt_shard* shard = rt_runtime_shard(runtime, scope->owner_shard_id);
    return shard != NULL ? shard : rt_runtime_shard0(runtime);
}

// Caller holds the shard carried by key. Re-resolving after that acquisition
// makes slot presence, identity and owner stamp one serialized observation.
rt_scope* rt_scope_resolve_key_locked(rt_executor* ex, waker_key key) {
    if (ex == NULL || key.kind != WAKER_SCOPE || key.id == 0) {
        return NULL;
    }
    rt_scope* scope = get_scope(ex, key.id);
    if (scope == NULL || scope->id != key.id || scope->owner_shard_id != key.owner_shard_id) {
        return NULL;
    }
    return scope;
}

static waker_key current_scope_key(uint64_t scope_id) {
    const rt_task* current = rt_current_task();
    if (current == NULL || current->active_scope_key.kind != WAKER_SCOPE ||
        current->active_scope_key.id != scope_id) {
        return waker_none();
    }
    return current->active_scope_key;
}

// Read both join answers together, initially and after waiter registration.
static size_t scope_join_snapshot(rt_executor* ex, waker_key key, bool* failfast) {
    rt_shard* pinned = rt_waiter_key_shard(ex, key);
    rt_shard_lock(pinned);
    const rt_scope* scope = rt_scope_resolve_key_locked(ex, key);
    size_t active = scope != NULL ? scope->active_children : 0;
    if (failfast != NULL) {
        *failfast = scope != NULL && scope->failfast_triggered ? true : false;
    }
    rt_shard_unlock(pinned);
    return active;
}

// Snapshot the scope's children under the pinned shard lock, then cancel each
// under the control lane (caller holds control). Never holds the pinned shard
// lock across cancel_task (which takes a child's own owner shard lock), so the
// lane order stays control -> at most one shard lock; never two shard locks.
void scope_cancel_children_controlled(rt_executor* ex, waker_key key) {
    if (ex == NULL || !waker_valid(key)) {
        return;
    }
    rt_shard* pinned = rt_waiter_key_shard(ex, key);
    uint64_t inline_children[8];
    uint64_t* children = inline_children;
    size_t count = 0;
    rt_shard_lock(pinned);
    const rt_scope* scope = rt_scope_resolve_key_locked(ex, key);
    if (scope == NULL) {
        rt_shard_unlock(pinned);
        return;
    }
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
    // slot and owner->active_scope_key is a thread-own write on the RUNNING
    // owner. No control lock on the steady path.
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
    owner->active_scope_key = scope_key(id, scope->owner_shard_id);
    return (void*)(uintptr_t)id;
}

void rt_scope_register_child(const void* scope_handle, void* task) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    waker_key key = current_scope_key(scope_id);
    if (!waker_valid(key)) {
        return;
    }
    uint64_t child_id = task_id_from_handle(task);
    const rt_task* child = get_task(ex, child_id);
    if (child == NULL) {
        return;
    }
    // Creation is the sole writer of a task's scope identity, so this call
    // adopts nothing and rewrites nothing. What it does is REFUSE: a task that
    // was not created in this scope is not this scope's child, and saying so
    // here is the whole reason the entry point still exists. It used to compute
    // the comparison and discard it, which is a check that checks nothing --
    // the stands that once relied on it to adopt a driver's child then ran
    // with a scope that counted nobody, and their fail-fast joins resolved
    // against an empty scope.
    if (!rt_scope_key_equal(child->creation_scope_key, key)) {
        panic_msg("async: a task registered with a scope that did not create it");
        return;
    }
}

void rt_scope_cancel_all(const void* scope_handle) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    waker_key key = current_scope_key(scope_id);
    if (!waker_valid(key)) {
        return;
    }
    // Cross-owner cancel walk needs the control lane (re-derivation, file
    // header). Rare: explicit scope cancellation, not the steady path.
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_SCOPE);
    }
    scope_cancel_children_controlled(ex, key);
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
    waker_key key = current_scope_key(scope_id);
    if (!waker_valid(key)) {
        return true;
    }
    size_t active = scope_join_snapshot(ex, key, failfast);
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
    prepare_park(ex, current, key, 0);
    pending_key = key;
    RT_SYNC_POINT(SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY);
    size_t active_after = scope_join_snapshot(ex, key, RT_DEBT261_VERIFY_FAILFAST_OUT(failfast));
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
    waker_key key = current_scope_key(scope_id);
    if (!waker_valid(key)) {
        return;
    }
    rt_shard* pinned = rt_waiter_key_shard(ex, key);
    rt_shard_lock(pinned);
    rt_scope* scope = rt_scope_resolve_key_locked(ex, key);
    if (scope == NULL) {
        rt_shard_unlock(pinned);
        return;
    }
    if (scope->active_children > 0) {
        rt_shard_unlock(pinned);
        panic_msg("async: scope exit with active children");
        return;
    }
    scope_exit_locked(ex, scope);
    rt_shard_unlock(pinned);
}

// Caller holds the pinned owner shard lock and observed active_children == 0.
// The slot is cleared before free; copied scope keys remain routable because
// they carry that same pinned shard and never dereference the cleared slot.
void scope_exit_locked(rt_executor* ex, rt_scope* scope) {
    if (ex == NULL || scope == NULL) {
        return;
    }
    uint64_t scope_id = scope->id;
    if (scope->owner != 0) {
        rt_task* owner = get_task(ex, scope->owner);
        if (owner != NULL && owner->active_scope_key.kind == WAKER_SCOPE &&
            owner->active_scope_key.id == scope_id) {
            owner->active_scope_key = waker_none();
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

// Caller holds the scope's pinned owner shard lock. One critical section
// retires the child and, when its committed kind is a cancellation in a
// fail-fast scope, raises the flag -- so no join, whose two answers come from
// one observation under this same lock, sees the set drained and fail-fast
// not fired when the draining child is the one that raised it. child_registered
// is the child's own membership answer, taken on its lane before the
// completion travelled; the scope never re-derives it.
rt_scope_child_done_effects rt_scope_take_child_done_locked(
    rt_executor* ex, waker_key key, uint64_t child_id, uint8_t result_kind, int child_registered) {
    rt_scope_child_done_effects fx = {0, 0};
    rt_scope* scope = rt_scope_resolve_key_locked(ex, key);
    if (scope == NULL) {
        return fx;
    }
    if (result_kind == TASK_RESULT_CANCELLED && scope->failfast && !scope->failfast_triggered) {
        if (!child_registered) {
            rt_scope_trace_failfast_after_drained_answer();
        }
        scope->failfast_triggered = 1;
        scope->failfast_child = child_id;
        fx.owner_to_wake = scope->owner;
    }
    if (child_registered) {
        (void)scope_remove_child(scope, child_id);
        if (scope->active_children > 0) {
            scope->active_children--;
        }
        fx.drained = scope->active_children == 0;
    }
    return fx;
}

// Runs with NO shard lock held, after the serializer released what the
// completion decided. A raised fail-fast EXECUTES its cancel walk under the
// control lane -- cancel_task routes each child by an owner word F2
// self-replace writes there -- which is a cancellation being carried out, not
// scope accounting being read; nothing about the scope is decided here.
void rt_scope_child_done_effects_apply(rt_executor* ex,
                                       waker_key key,
                                       rt_scope_child_done_effects fx) {
    if (fx.owner_to_wake != 0) {
        int need_control = !rt_lane_holds_control();
        if (need_control) {
            rt_control_lock(ex);
            rt_trace_control_lock_site(RT_CTRL_SITE_SCOPE);
        }
        scope_cancel_children_controlled(ex, key);
        wake_task(ex, fx.owner_to_wake, 1);
        if (need_control) {
            rt_control_unlock(ex);
        }
    }
    if (fx.drained) {
        wake_key_all(ex, key);
    }
}

// Completion-side scope bookkeeping (S5-Q8), called from mark_done after the
// TASK_DONE store; task is the completing child. Every write to the scope's
// accounting happens on the scope's owner lane in one critical section (owner
// ruling 2026-09-02). A child that shares the scope's shard is already on that
// lane and takes it directly; a child owned elsewhere (re-placed, F2) does not
// reach across -- it publishes what it knows (which scope, which child, which
// kind, whether it was counted) as an event into the owner shard's inbound
// control lane, and the owner lane applies it (rt_scope_event.c). The
// process-wide control lane is never taken to decide anything about a scope.
void scope_on_child_done(rt_executor* ex, rt_task* task, uint8_t result_kind) {
    if (ex == NULL || task == NULL) {
        return;
    }
    waker_key key = task->creation_scope_key;
    RT_SYNC_POINT(SP_SCOPE_CHILD_DONE_AFTER_MEMBERSHIP_TAKE);
    if (!waker_valid(key) || !RT_SCOPE_MEMBER_MAY_FAILFAST(task)) {
        return;
    }
    // The child's own membership word, read and retired on the child's lane:
    // the one fact the event carries that the owner lane cannot re-derive.
    int registered = task->scope_registered != 0;
    task->scope_registered = 0;
    if (key.owner_shard_id != task->owner_shard_id) {
        rt_scope_publish_child_done(ex, key, task, result_kind, registered);
        return;
    }
    rt_shard* pinned = rt_waiter_key_shard(ex, key);
    rt_shard_lock(pinned);
    rt_scope_child_done_effects fx =
        rt_scope_take_child_done_locked(ex, key, task->id, result_kind, registered);
    rt_shard_unlock(pinned);
    rt_scope_child_done_effects_apply(ex, key, fx);
}
