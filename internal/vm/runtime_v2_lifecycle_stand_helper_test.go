//go:build runtime_v2_pending

package vm_test

// lifecycleHarnessStandHelpers carries the helper every lifecycle stand that
// needs a driver-spawned child uses. It is concatenated into the shared
// lifecycle harness translation unit by buildRuntimeV2LifecycleHarnessWithFlags
// (runtime_v2_lifecycle_behavior_harness_test.go), after
// lifecycleHarnessCommon (which owns spawn_pinned) and before
// lifecycleHarnessMain.
//
// This file used to carry a falsifier as well,
// TestRuntimeV2LifecycleStandHelperHeldPollTrap and its negative control, for a
// trap that no longer exists: an owner held inside its own poll that created a
// child with __task_create left the child on its local deque with no worker
// signalled (`signal_ready_now = signal_ready && local->len > 1`). The
// first-local-task peer wake in rt_ready_queue.c signals a peer whenever the
// scheduler has one, so such a child now runs on the woken peer. That rule is
// proved, with a negative control that restores the old guard, by
// TestRuntimeV2LifecycleLocalPeerWake{Proof,NegativeControl}. A stand that
// still demanded the trap was asserting the replaced rule (RULES.md, Global
// Rule 15), so it was removed rather than bent to pass.
const lifecycleHarnessStandHelpers = `
// --- Stand helper: a child spawned by the driver ---
//
// A stand that needs a child outside any worker-owned scope spawns it from the
// DRIVER thread. A driver has no worker TLS context, so current_local_queue
// returns NULL, the push goes to the shard's shared inject queue WITH the ready
// signal, and a parked worker is woken for it. Such a task is deliberately
// outside any worker-owned scope; callers must not mistake the helper for scope
// membership or late adoption.
//
// A push from inside a poll (__task_create -> ready_push_task_locked with
// force_inject=0, rt_async_task.c / rt_ready_queue.c) lands on the LOCAL deque
// of the worker running that poll instead, and requests a peer wake whenever
// the scheduler has more than one worker. A stand that needs such a child to
// stay with its creator has to keep every peer busy itself; the inline-claim
// stand does exactly that.

// Spawn a child the way a stand driver must: inject queue plus ready signal.
// Refuses, loudly, when it is called from a worker thread -- there the push
// would go to that worker's local deque and this helper would be a lie.
// current_worker_scheduler (rt_ready_queue.c) is the exact predicate
// ready_push_task_locked itself uses to pick the local path.
//
// Deliberately external linkage, not static: the helper must be able to sit in
// the harness translation unit before any stand calls it, and -Wall -Werror
// rejects an unused static function.
rt_task* spawn_child_for_stand(rt_executor* ex, int64_t poll_fn_id, uint32_t shard);
rt_task* spawn_child_for_stand(rt_executor* ex, int64_t poll_fn_id, uint32_t shard) {
    if (current_worker_scheduler(ex) != NULL) {
        fputs("stand: spawn_child_for_stand ran on a worker thread -- spawn the child from "
              "the stand driver (a worker-local push lands on that worker's deque: rt_ready_queue.c)\n",
              stderr);
        return NULL;
    }
    return spawn_pinned(ex, poll_fn_id, shard);
}
`
