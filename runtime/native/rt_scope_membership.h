#ifndef SURGE_RUNTIME_NATIVE_RT_SCOPE_MEMBERSHIP_H
#define SURGE_RUNTIME_NATIVE_RT_SCOPE_MEMBERSHIP_H
// Which scope a task is a child of, and who gets to decide it.
//
// The word is rt_task::parent_scope_id and it holds one of three things:
//
//   RT_SCOPE_CLAIM_NONE       nothing has claimed this task
//   a scope id                this task is that scope's child
//   RT_SCOPE_CLAIM_COMPLETED  this task completed before any scope claimed it,
//                             and nothing may claim it now
//
// Two threads race to decide it. A registration (rt_scope_register_child) runs
// under the scope's pinned shard lock; the task's own completion
// (scope_on_child_done) must read the word BEFORE it takes any lock, because
// the word is what tells it which scope's lock to take. They share no lock, and
// a test-and-store on each side is not enough to order them: the completion's
// "publish DONE, then read the claim" against the registration's "read the
// status, then store the claim" has an execution in which each misses the
// other -- not store-buffering, which sequential consistency forbids, but a
// load-then-store against a store-then-load, which it permits. Both sides then
// believe the other was not there: the completion skips the scope, raising no
// fail-fast and retiring nothing, and the registration counts a child that has
// already finished and that nothing will ever retire, so the scope never drains
// and the `@failfast` block it belongs to never resolves.
//
// So each side moves the word with ONE read-modify-write instead. Read-modify-
// writes on one object are totally ordered by that object's modification order
// and each reads the value written by the modification immediately before it,
// so whichever runs second sees the first and exactly one of "the child is a
// member" and "the child completed unclaimed" is true. The cost is one
// uncontended compare-and-swap on the completing task's own line, per
// completion.
#include "rt_async_internal.h"

#include <stdatomic.h>
#include <stdint.h>

#define RT_SCOPE_CLAIM_NONE ((uint64_t)0)
#define RT_SCOPE_CLAIM_COMPLETED UINT64_MAX

// The registration's half of the claim: NONE -> scope_id. Answers whether the
// task is now this scope's member AND still uncounted, so the caller that is
// told no knows the task completed first and that the scope must be told by the
// late path instead of counting a child nobody will retire.
//
// Losing to this scope's OWN id is not losing, and reading it as a loss is what
// stopped a scope from joining anything. `spawn` lowers to a wake followed by a
// registration, and the wake adopts a task whose word is still NONE into the
// waking task's scope -- which, for an ordinary spawn inside a scope body, is
// the very scope the registration that follows is for. That adoption makes the
// task a member and counts nothing, so the registration is still the caller
// that must count it. Answering no there left `active_children` at zero for
// every child of every scope, and a scope with no children to wait for answers
// immediately: the block resolved while the work it had started was still
// running.
//
// Counting twice is prevented by the caller's own `scope_registered` flag under
// the same lock, not by this answer. A DIFFERENT scope's id, or the completed
// seal, is a real loss and still reads as one.
static inline int rt_scope_claim_membership(rt_task* task, uint64_t scope_id) {
    if (task == NULL) {
        return 0;
    }
    uint64_t expected = RT_SCOPE_CLAIM_NONE;
    if (atomic_compare_exchange_strong_explicit(&task->parent_scope_id,
                                                &expected,
                                                scope_id,
                                                memory_order_acq_rel,
                                                memory_order_acquire)) {
        return 1;
    }
    return expected == scope_id ? 1 : 0;
}

// The completion's half: NONE -> COMPLETED. Answers the scope that had claimed
// this task, or NONE when none had -- and in that case the word is left sealed,
// so a registration still in flight loses its claim and handles the already
// completed child itself. The acquire on failure is what publishes the
// registration's writes to this completion, and the release on success is what
// publishes this task's committed result to the registration.
static inline uint64_t rt_scope_take_membership(rt_task* task) {
    if (task == NULL) {
        return RT_SCOPE_CLAIM_NONE;
    }
    uint64_t expected = RT_SCOPE_CLAIM_NONE;
    if (atomic_compare_exchange_strong_explicit(&task->parent_scope_id,
                                                &expected,
                                                RT_SCOPE_CLAIM_COMPLETED,
                                                memory_order_acq_rel,
                                                memory_order_acquire)) {
        return RT_SCOPE_CLAIM_NONE;
    }
    // A task completes exactly once, so its own completion cannot read the
    // sealed state here; answering NONE for it keeps a second reader harmless
    // rather than sending it to look up a scope id that is not one.
    return expected == RT_SCOPE_CLAIM_COMPLETED ? RT_SCOPE_CLAIM_NONE : expected;
}

#endif // SURGE_RUNTIME_NATIVE_RT_SCOPE_MEMBERSHIP_H
