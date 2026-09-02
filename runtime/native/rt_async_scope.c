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
    // Kept as an ABI-compatible validator while existing MIR call sites remain.
    // Membership was already published at creation; this call never adopts or
    // rewrites a task, including one created in another scope.
    (void)rt_scope_key_equal(child->creation_scope_key, key);
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

// Completion-side scope bookkeeping (S5-Q8), called from mark_done after the
// TASK_DONE store. task is the completing child. Same-owner non-failfast
// child-done runs control-free under the pinned shard lock (== the child's own
// owner shard here); cross-owner (re-placed child) or failfast-triggering
// completions take the counted control lane (file header re-derivation).
void scope_on_child_done(rt_executor* ex, rt_task* task, uint8_t result_kind) {
    if (ex == NULL || task == NULL) {
        return;
    }
    waker_key key = task->creation_scope_key;
    RT_SYNC_POINT(SP_SCOPE_CHILD_DONE_AFTER_MEMBERSHIP_TAKE);
    if (!waker_valid(key) || !RT_SCOPE_MEMBER_MAY_FAILFAST(task)) {
        return;
    }
    rt_shard* child_shard = rt_task_owner_shard(ex, task);
    int same_owner = key.owner_shard_id == task->owner_shard_id;
    if (same_owner) {
        rt_shard_lock(child_shard);
        rt_scope* scope = rt_scope_resolve_key_locked(ex, key);
        if (scope == NULL) {
            rt_shard_unlock(child_shard);
            return;
        }
        int may_failfast = result_kind == TASK_RESULT_CANCELLED && scope->failfast &&
                           !scope->failfast_triggered && RT_SCOPE_MEMBER_MAY_FAILFAST(task);
        if (may_failfast) {
            rt_shard_unlock(child_shard);
        } else {
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
                wake_key_all(ex, key);
            }
            return;
        }
    }
    // Rare: cross-owner (re-placed child) or failfast-triggering completion.
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_SCOPE);
    }
    rt_shard* pinned = rt_waiter_key_shard(ex, key);
    uint64_t owner_to_wake = 0;
    int wake_owner = 0;
    rt_shard_lock(pinned);
    rt_scope* scope = rt_scope_resolve_key_locked(ex, key);
    if (scope != NULL) {
        if (result_kind == TASK_RESULT_CANCELLED && scope->failfast && !scope->failfast_triggered &&
            RT_SCOPE_MEMBER_MAY_FAILFAST(task)) {
            if (!task->scope_registered) {
                rt_scope_trace_failfast_after_drained_answer();
            }
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
    }
    rt_shard_unlock(pinned);
    if (owner_to_wake != 0) {
        scope_cancel_children_controlled(ex, key);
        wake_task(ex, owner_to_wake, 1);
    }
    if (wake_owner) {
        wake_key_all(ex, key);
    }
    if (need_control) {
        rt_control_unlock(ex);
    }
}
