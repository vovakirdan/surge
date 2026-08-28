//go:build runtime_v2_pending

package vm_test

// lifecycleHarnessStandHelpers carries the two stand-writing helpers every
// lifecycle stand whose owner is HELD inside its own poll needs. It is
// concatenated into the shared lifecycle harness translation unit by
// buildRuntimeV2LifecycleHarnessWithFlags
// (runtime_v2_lifecycle_behavior_harness_test.go), after
// lifecycleHarnessCommon (which owns spawn_pinned, sleep_us and the wait_*
// probes these reuse) and before lifecycleHarnessMain.
const lifecycleHarnessStandHelpers = `
// --- Stand helpers: a child for a stand whose owner is held in its poll ---
//
// The defect class these close, observed live in a fail-fast join stand that
// held its owner at a sync point and then blamed the runtime: a stand whose
// owner is held inside its own poll -- at a sync point, or on any wait that
// does not return to the scheduler -- must NOT create its child with
// __task_create from that same poll. That push lands on the LOCAL deque of the
// worker running the poll (__task_create -> ready_push_task_locked with
// force_inject=0, rt_async_task.c / rt_ready_queue.c), and a single local entry
// signals nobody:
//
//     signal_ready_now = signal_ready && local->len > 1;   (rt_ready_queue.c)
//
// The pusher is the held worker, every other worker sits in
// pthread_cond_wait(&shard->worker_cv) with wake_pending == 0 (rt_worker_turn.c)
// and nothing wakes them, so nobody ever pops that child -- stealing needs an
// AWAKE thief. The owner is held, so it will not pop it either. The child never
// runs, its cancellation is never delivered, and the stand reports whatever it
// was waiting for ("cancelled child never completed") as if the runtime were at
// fault. It is not: the local queue is the pusher's own path (docs/RUNTIME_V2.md
// "No Hot-Path Stealing" / "Structured Concurrency" -- spawn is shard-local and
// the shard's own worker consumes it).
//
// The stand's answer is to spawn the child from the DRIVER thread. A driver has
// no worker TLS context, so current_local_queue returns NULL, the push goes to
// the shard's shared inject queue WITH the ready signal, and a parked worker is
// woken for it. The owner's poll then only REGISTERS the already-live task
// (rt_scope_register_child accepts a live, not-yet-DONE task).

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
              "the stand driver (a worker-local push signals nobody: rt_ready_queue.c)\n",
              stderr);
        return NULL;
    }
    return spawn_pinned(ex, poll_fn_id, shard);
}

// Require that a worker actually TOOK this child, and name the trap when none
// did instead of letting the stand hang until its harness timeout.
//
// What it observes: the pair (status, enqueued), and it accepts the child as
// soon as EITHER has left the value the spawn path wrote. Why that is the right
// pair -- a child that was pushed and never popped is frozen in exactly one
// state, and both halves of it are written by the push itself:
//
//   ready_push_task_locked: task_enqueued_store(task, 1); task_status_store(task, TASK_READY);
//
// From there, only pop_task_from_deque clears enqueued and only the worker turn
// stores TASK_RUNNING (rt_ready_queue.c, rt_worker_turn.c) -- both on the
// popping worker, after it has the task in hand. So an observation of
// enqueued == 0 or of a status other than TASK_READY PROVES a worker reached
// this task, and a child that never ran can never produce either one. There is
// no false "it ran".
//
// The disjunction is what makes it safe for a child that yields in a loop
// (POLL_SPIN_FOREVER and friends): such a child cycles
// [queued: READY,1] -> [popped: READY,0] -> [RUNNING,0] -> [queued again], so
// only the first phase of its cycle looks like the frozen state, and every
// sample in the other phases answers. A never-run child is checked
// timeout_ms times, one millisecond apart, before the diagnosis is printed.
int stand_require_child_running(rt_task* child, uint32_t timeout_ms);
int stand_require_child_running(rt_task* child, uint32_t timeout_ms) {
    for (uint32_t i = 0; child != NULL && i < timeout_ms; i++) {
        if (task_status_load(child) != TASK_READY || task_enqueued_load(child) == 0) {
            return 1;
        }
        sleep_us(1000);
    }
    fputs("stand: child never ran -- spawned from a held poll? "
          "(a worker-local push signals nobody: rt_ready_queue.c)\n",
          stderr);
    fprintf(stderr,
            "stand: child id=%llu is still READY and enqueued after %ums; spawn it with "
            "spawn_child_for_stand from the driver and let the held owner only register it\n",
            child != NULL ? (unsigned long long)child->id : 0ULL, (unsigned)timeout_ms);
    return 0;
}
`
