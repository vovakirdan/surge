#ifndef SURGE_RUNTIME_NATIVE_RT_SCOPE_MEMBERSHIP_H
#define SURGE_RUNTIME_NATIVE_RT_SCOPE_MEMBERSHIP_H

#include "rt_async_internal.h"
#include "rt_scope_provenance_trace.h"

#if defined(RV2_SCOPE_MEMBERSHIP_CLAIM_NEGATIVE_CONTROL) &&                                        \
    !defined(RV2_SCOPE_PROVENANCE_NEGATIVE_CONTROL)
#define RV2_SCOPE_PROVENANCE_NEGATIVE_CONTROL
#endif

// Creation is the sole writer of a task's scope identity. The creator already
// owns the scope's pinned shard lane, so count, child-list insertion and task
// publication are one critical section. A task with WAKER_NONE is forever a
// non-member; wake and the legacy registration intrinsic cannot adopt it.
static inline int rt_scope_key_equal(waker_key left, waker_key right) {
    return left.kind == right.kind && left.id == right.id &&
           left.owner_shard_id == right.owner_shard_id;
}

static inline void rt_scope_publish_creation_locked(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL || !waker_valid(task->creation_scope_key)) {
        return;
    }
    if (!rt_lane_holds_shard(task->creation_scope_key.owner_shard_id)) {
        panic_msg("async: scope membership published outside its owner lane");
        return;
    }
    rt_scope* scope = rt_scope_resolve_key_locked(ex, task->creation_scope_key);
    if (scope == NULL || task->scope_registered) {
        panic_msg("async: scope membership could not be published at task creation");
        return;
    }
    scope_add_child(scope, task->id);
    task->scope_registered = 1;
    scope->active_children++;
}

#ifdef RV2_SCOPE_PROVENANCE_NEGATIVE_CONTROL
#define RT_SCOPE_WAKE_PROVENANCE(target, key)                                                      \
    do {                                                                                           \
        if (!waker_valid((target)->creation_scope_key)) {                                          \
            (target)->creation_scope_key = (key);                                                  \
            rt_scope_trace_identity_rewritten();                                                   \
        }                                                                                          \
    } while (0)
#define RT_SCOPE_MEMBER_MAY_FAILFAST(task) (1)
#else
#define RT_SCOPE_WAKE_PROVENANCE(target, key) ((void)(target), (void)(key))
#define RT_SCOPE_MEMBER_MAY_FAILFAST(task) ((task)->scope_registered != 0)
#endif

#endif // SURGE_RUNTIME_NATIVE_RT_SCOPE_MEMBERSHIP_H
