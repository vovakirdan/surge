//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// standHelperTrapDiagnosis is the sentence stand_require_child_running prints
// when no worker ever took the child. The stand below exists to make the tree
// notice if that sentence stops arriving.
const standHelperTrapDiagnosis = "stand: child never ran -- spawned from a held poll? " +
	"(a worker-local push signals nobody: rt_ready_queue.c)"

// TestRuntimeV2LifecycleStandHelperHeldPollTrap walks into the trap the helper
// exists to name: an owner held inside its own poll, its child created there by
// __task_create, so the push lands on the held worker's local deque and
// signal_ready_now stays 0 (rt_ready_queue.c). The stand must FAIL -- that is
// the point -- but fail in seconds with the helper's diagnosis, not hang until
// the gate's timeout while blaming a cancellation that was never delivered.
//
// The second assertion is what keeps the first from being vacuous: under the
// SAME hold, on the SAME workers, a child spawned by the driver
// (spawn_child_for_stand: inject queue plus ready signal) does run. So what
// stripped the first child of a worker is the local push, not the held owner.
//
// SURGE_SHARDS=1: every worker shares shard 0, so a worker that is awake MAY
// steal the trapped child (rt_task_can_steal_from_shard) -- the configuration
// that could most easily disprove the trap is the one the stand runs in. What
// keeps the child stranded is that the other workers are parked on worker_cv
// with wake_pending == 0 and this push signals nobody.
func TestRuntimeV2LifecycleStandHelperHeldPollTrap(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessStandHelper(t, false)
	for _, threads := range []string{"2", "4"} {
		t.Run("threads-"+threads, func(t *testing.T) {
			env := lifecycleEnv(
				"SURGE_SHARDS=1", "SURGE_THREADS="+threads, "SURGE_BLOCKING_THREADS=1")
			stdout, stderr, exitCode := runLifecycleHarness(
				t, binPath, "stand-helper-held-poll-trap", env)
			if exitCode == 0 {
				t.Fatalf("stand-helper trap stand unexpectedly passed at SURGE_THREADS=%s\nstdout:\n%s\nstderr:\n%s",
					threads, stdout, stderr)
			}
			if !strings.Contains(stderr, standHelperTrapDiagnosis) {
				t.Fatalf("stand-helper trap stand failed without naming the trap at SURGE_THREADS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					threads, exitCode, stdout, stderr)
			}
			const control = "stand-helper trap stand: control child ran under the same hold"
			if !strings.Contains(stderr, control) {
				t.Fatalf("stand-helper trap stand never showed that a driver-spawned child runs under the same hold at SURGE_THREADS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					threads, exitCode, stdout, stderr)
			}
		})
	}
}

// TestRuntimeV2LifecycleStandHelperHeldPollTrapNegativeControl builds the same
// stand with the helper taken out of it, leaving the shape every stand of this
// kind has today: cancel the child and wait the harness's standard 4s budget
// for it to drain. It must spend that whole budget and then report the wrong
// culprit -- "cancelled child never completed", the sentence that once got read
// as a runtime defect -- and it must never print the helper's diagnosis, which
// is not in that build. This is what the helper buys, measured in the same run.
func TestRuntimeV2LifecycleStandHelperHeldPollTrapNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessStandHelper(t, true)
	env := lifecycleEnv("SURGE_SHARDS=1", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "stand-helper-held-poll-trap", env)
	if exitCode == 0 {
		t.Fatalf("stand-helper negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	const want = "stand-helper trap stand: cancelled child never completed"
	if !strings.Contains(stderr, want) {
		t.Fatalf("stand-helper negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	if strings.Contains(stderr, standHelperTrapDiagnosis) {
		t.Fatalf("stand-helper negative control named the trap without the helper (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

func buildRuntimeV2LifecycleHarnessStandHelper(t *testing.T, negativeControl bool) string {
	t.Helper()
	name := "lifecycle_harness_stand_helper"
	var flags []string
	if negativeControl {
		name += "_negative"
		flags = append(flags, "-DRV2_STAND_HELPER_NEGATIVE_CONTROL")
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

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
// woken for it. Such a task is deliberately outside any worker-owned scope;
// callers must not mistake the helper for scope membership or late adoption.

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

// --- The falsifier: a stand that walks into the trap on purpose ---

#define POLL_STAND_TRAP_OWNER 4039

static _Atomic(void*) g_stand_trap_child;
static _Atomic uint32_t g_stand_trap_release;

// The wrong shape, deliberately: this owner creates its child with
// __task_create from INSIDE its own poll -- so the push lands on this worker's
// local deque -- and then never returns to the scheduler until the driver
// releases it. A plain hold is used rather than a runtime sync point because
// the trap is about the worker being inside __surge_poll_call, not about which
// line of the runtime holds it there; this way the stand needs no
// RT_TEST_SYNC_POINTS build.
static void poll_stand_trap_owner(void) {
    if (atomic_load_explicit(&g_stand_trap_child, memory_order_acquire) == NULL) {
        void* child = __task_create(POLL_SPIN_FOREVER, NULL, rt_channel_opaque_word_ops());
        atomic_store_explicit(&g_stand_trap_child, child, memory_order_release);
    }
    while (atomic_load_explicit(&g_stand_trap_release, memory_order_acquire) == 0) {
        sleep_us(1000);
    }
    rt_async_return(NULL, &(uint64_t){0});
}

// This stand FAILS by design: it reproduces the trap, and what it proves is
// that the failure arrives in seconds with the helper's diagnosis instead of
// hanging until the harness timeout with the wrong culprit named.
//
// Release before shutdown, on every path: rt_executor_request_shutdown joins
// the workers, and one of them is inside the held poll.
static int mode_stand_helper_held_poll_trap(rt_executor* ex) {
    atomic_store_explicit(&g_stand_trap_child, NULL, memory_order_release);
    atomic_store_explicit(&g_stand_trap_release, 0, memory_order_release);

    // Quiesce first. The trap only exists while the other workers are ASLEEP:
    // an awake worker steals from a peer's local deque (worker_next_ready,
    // rt_ready_queue.c) and would run the child. One task driven to completion
    // through the driver path, then a pause, leaves every worker parked on
    // worker_cv with wake_pending == 0.
    rt_task* warm = spawn_child_for_stand(ex, POLL_JOIN_TARGET_QUICK, 0);
    if (warm == NULL || !wait_task_status(warm, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("stand-helper trap stand: warm-up child never completed");
    }
    sleep_us(100000);

    rt_task* owner = spawn_child_for_stand(ex, POLL_STAND_TRAP_OWNER, 0);
    if (owner == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("stand-helper trap stand: owner allocation failed");
    }
    if (!wait_ptr(&g_stand_trap_child, 4000)) {
        atomic_store_explicit(&g_stand_trap_release, 1, memory_order_release);
        (void)rt_executor_request_shutdown(ex);
        return fail("stand-helper trap stand: the held owner never created its child");
    }
    rt_task* trapped = (rt_task*)atomic_load_explicit(&g_stand_trap_child, memory_order_acquire);
#ifdef RV2_STAND_HELPER_NEGATIVE_CONTROL
    // Without the helper: what every stand of this shape does today. Cancel the
    // child, spend the harness's standard budget waiting for it to drain, and
    // then name the wrong culprit -- the child never ran, so the cancellation
    // it is waiting for can never be delivered.
    rt_task_cancel(trapped);
    if (!wait_task_status(trapped, TASK_DONE, 4000)) {
        atomic_store_explicit(&g_stand_trap_release, 1, memory_order_release);
        (void)rt_executor_request_shutdown(ex);
        return fail("stand-helper trap stand: cancelled child never completed");
    }
    atomic_store_explicit(&g_stand_trap_release, 1, memory_order_release);
    (void)rt_executor_request_shutdown(ex);
    return fail("stand-helper trap stand: the trap did not reproduce under the negative control");
#else
    if (stand_require_child_running(trapped, 2000)) {
        atomic_store_explicit(&g_stand_trap_release, 1, memory_order_release);
        (void)rt_executor_request_shutdown(ex);
        return fail("stand-helper trap stand: the poll-spawned child ran; the trap did not reproduce");
    }
    // Non-vacuity, and the whole discrimination: the SAME held owner, the SAME
    // workers, a child spawned by the DRIVER -- and it runs. What stops the
    // first child is the local push, not the hold.
    rt_task* control = spawn_child_for_stand(ex, POLL_SPIN_FOREVER, 0);
    if (control == NULL || !stand_require_child_running(control, 4000)) {
        atomic_store_explicit(&g_stand_trap_release, 1, memory_order_release);
        (void)rt_executor_request_shutdown(ex);
        return fail("stand-helper trap stand: the driver-spawned control child did not run either");
    }
    fputs("stand-helper trap stand: control child ran under the same hold\n", stderr);
    atomic_store_explicit(&g_stand_trap_release, 1, memory_order_release);
    (void)rt_executor_request_shutdown(ex);
    return 1;
#endif
}
`
