//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// A polled task must outlive the application of its own poll outcome
// (RV2-DEBT-291, first half).
//
// What keeps a task addressable during its turn is its RUNNING status, not a
// reference: a release frees only a task that has completed, so nothing may
// reclaim a RUNNING one under its poller. apply_poll_outcome ENDS that
// protection in every arm and then goes on using the pointer
// (rt_task_complete.c). The yielded arm is where it bites: it stores TASK_READY
// and re-pushes, and ready_push_with_policy resolves the owner shard, BLOCKS on
// that shard's lock, and re-reads the task's status on the far side
// (rt_ready_queue.c). Everything the task needs to be freed can happen inside
// that block.
//
// The drive holds the re-push at SP_READY_REQUEUE_BEFORE_LOCK -- after the
// unlocked pre-checks, before the lock -- and then does, from the driver, the
// two things an AWAITING POLL does to such a task in a real program, in order:
//
//	rt_async_task.c:220   a target that is neither WAITING nor DONE is woken,
//	                      so a READY, not-yet-enqueued task is enqueued and
//	                      handed to another worker, which polls it to DONE;
//	rt_async_task.c:244   the awaiter that then finds it DONE takes the result
//	                      and drops its handle -- the last one.
//
// Both were read off a live AddressSanitizer report of this defect (the freeing
// stack is rt_task_poll -> task_release_lane_aware -> reclaim_task on a
// checkpoint task), so the stand reproduces the observed shape rather than an
// invented one.
//
// With the pin, the poller's own reference is what stands between that drop and
// the free, so the drop leaves the task alive and the held re-push re-reads a
// live task, sees TASK_DONE and refuses the duplicate push.
// RT_POLL_OUTCOME_PIN_NEGATIVE_CONTROL removes the pin and MUST be seen reading
// freed memory AT that re-read.
//
// Why this replaces the campaign it came from: the old row asked for the same
// race to happen BY ITSELF in 96 oversubscribed processes, and the interleaving
// it needed -- an awaiter polling a child that is mid-yield-re-push -- is one
// the cancel-ordering fix (a cancel now reaches the leaves before it makes
// anyone runnable again, rt_task_complete.c) makes rarer: a cancelled awaiter is
// re-polled after its children are DONE, so its poll no longer meets one of them
// in the window. The control then reported "the row proved nothing" in about
// 4 of 10 aggregates. A schedule is not the rule; the rule is that the poller
// holds a reference. This stand asserts the rule.
func TestRuntimeV2LifecyclePollOutcomePinProof(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessPollOutcomePin(t, false)
	// SURGE_SHARDS=1 with two workers and up: one worker is held inside the
	// re-push, so the probe needs ANOTHER worker on the SAME shard to be woken
	// onto and polled to completion.
	for _, threads := range []string{"2", "4"} {
		t.Run("threads-"+threads, func(t *testing.T) {
			stdout, stderr, exitCode := runPollOutcomePinStand(t, binPath, threads)
			if exitCode != 0 {
				t.Fatalf("poll-outcome pin proof failed at SURGE_THREADS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					threads, exitCode, stdout, stderr)
			}
			assertPollOutcomePinWindow(t, stdout, stderr, exitCode)
			// The rule itself, read at the instant it matters: the last handle
			// has just been dropped on a task that HAS completed, and the count
			// is still not empty -- what is left is the poller's own reference.
			// The negative control prints refs_after_drop=0 here.
			if !strings.Contains(stderr, "debt291 drop: completed=1 refs_after_drop=1") {
				t.Fatalf("poll-outcome pin proof did not show the poller's reference outliving the last handle drop\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
			if strings.Contains(stderr, "AddressSanitizer") {
				t.Fatalf("poll-outcome pin proof reported a sanitizer error\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2LifecyclePollOutcomePinNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessPollOutcomePin(t, true)
	stdout, stderr, exitCode := runPollOutcomePinStand(t, binPath, "2")
	if exitCode == 0 {
		t.Fatalf("poll-outcome pin negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	// Same window, same drive: what differs is only the pin.
	assertPollOutcomePinWindow(t, stdout, stderr, exitCode)
	if !strings.Contains(stderr, "debt291 drop: completed=1 refs_after_drop=0") {
		t.Fatalf("poll-outcome pin negative control did not free the task at the drop (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	// And it must fail for the right reason, at the right line: the re-read on
	// the far side of the owner shard lock, inside the re-push the yielded arm
	// issued. Any other sanitizer report is a different finding.
	if !strings.Contains(stderr, "heap-use-after-free") {
		t.Fatalf("poll-outcome pin negative control failed without a use-after-free (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	faulting := pollOutcomePinFaultingStack(stderr)
	if !strings.Contains(faulting, "in ready_push_with_policy") {
		t.Fatalf("poll-outcome pin negative control read freed memory somewhere other than the re-push (code=%d)\nfaulting stack:\n%s\nstdout:\n%s\nstderr:\n%s",
			exitCode, faulting, stdout, stderr)
	}
	if !strings.Contains(faulting, "in apply_poll_outcome") {
		t.Fatalf("poll-outcome pin negative control's freed read did not come from the poll-outcome arm (code=%d)\nfaulting stack:\n%s\nstdout:\n%s\nstderr:\n%s",
			exitCode, faulting, stdout, stderr)
	}
}

// The three sentences that say the drive built the window it claims to build,
// asserted on BOTH arms so the two runs are known to differ only in the pin.
func assertPollOutcomePinWindow(t *testing.T, stdout, stderr string, exitCode int) {
	t.Helper()
	for _, want := range []string{
		// The re-push is held with the task in exactly the state the wake
		// path's gate accepts (rt_task_park.c): READY and not enqueued.
		"debt291 window: status=0 enqueued=0",
		// A second worker took it and ran it to completion INSIDE the window.
		"debt291 completed in window: done=1",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("poll-outcome pin stand did not build the window -- missing %q (code=%d)\nstdout:\n%s\nstderr:\n%s",
				want, exitCode, stdout, stderr)
		}
	}
}

// The part of a sanitizer report that names where the bad access happened: the
// free path appears in the FREEING stack of every report about a task, so
// matching the whole text would let any of them satisfy the assertion.
func pollOutcomePinFaultingStack(report string) string {
	if idx := strings.Index(report, " is located "); idx >= 0 {
		return report[:idx]
	}
	return report
}

func runPollOutcomePinStand(t *testing.T, binPath, threads string) (string, string, int) {
	t.Helper()
	env := lifecycleEnv(
		"SURGE_SHARDS=1",
		"SURGE_THREADS="+threads,
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_READY_REQUEUE_BEFORE_LOCK:block",
		"ASAN_OPTIONS=detect_leaks=0:halt_on_error=1:abort_on_error=0")
	return runLifecycleHarness(t, binPath, "debt291-poll-outcome-pin", env)
}

// AddressSanitizer is the oracle for "this read touched freed memory"; the sync
// point is what makes the read happen every run instead of once in a campaign.
func buildRuntimeV2LifecycleHarnessPollOutcomePin(t *testing.T, negativeControl bool) string {
	t.Helper()
	name := "lifecycle_harness_poll_outcome_pin"
	flags := []string{
		"-DRT_TEST_SYNC_POINTS",
		"-fsanitize=address", "-g", "-O1", "-fno-omit-frame-pointer",
	}
	if negativeControl {
		name += "_negative"
		flags = append(flags, "-DRT_POLL_OUTCOME_PIN_NEGATIVE_CONTROL")
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

// lifecycleHarnessPollOutcomePinModes is concatenated into the shared lifecycle
// harness translation unit by buildRuntimeV2LifecycleHarnessWithFlags, after
// lifecycleHarnessSyncPointModes (whose wait_sync_point_count it uses) and
// lifecycleHarnessStandHelpers (spawn_child_for_stand), and before
// lifecycleHarnessMain.
const lifecycleHarnessPollOutcomePinModes = `
#ifdef RT_TEST_SYNC_POINTS
#include "rt_task_refs.h"

#define POLL_DEBT291_PIN_PROBE 4045

static _Atomic uint32_t g_debt291_probe_steps;

// Yields once -- so its requeue goes through the force-inject path and reaches
// SP_READY_REQUEUE_BEFORE_LOCK -- and completes on its next poll. Nothing holds
// it there: the whole point is that the SECOND poll runs, on another worker,
// while the first worker is still inside the re-push of the FIRST one.
static void poll_debt291_pin_probe(void) {
    uint32_t step = atomic_fetch_add_explicit(&g_debt291_probe_steps, 1, memory_order_acq_rel);
    if (step == 0) {
        rt_async_yield(NULL, 0);
        return;
    }
    rt_async_return(NULL, &(uint64_t){42});
}

static int mode_debt291_poll_outcome_pin(rt_executor* ex) {
    unsigned before = rt_sync_point_reached_count(RT_SYNC_POINT_SP_READY_REQUEUE_BEFORE_LOCK);
    atomic_store_explicit(&g_debt291_probe_steps, 0, memory_order_release);

    // Quiesce first, so the only wake the probe can get is the one below.
    rt_task* warm = spawn_child_for_stand(ex, POLL_JOIN_TARGET_QUICK, 0);
    if (warm == NULL || !wait_task_status(warm, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt291 stand: warm-up child never completed");
    }
    sleep_us(100000);

    rt_task* probe = spawn_child_for_stand(ex, POLL_DEBT291_PIN_PROBE, 0);
    if (probe == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt291 stand: probe allocation failed");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_READY_REQUEUE_BEFORE_LOCK, before, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt291 stand: the probe never reached the requeue window");
    }

    // The window. The yielded arm has already published TASK_READY and the
    // re-push is held between its unlocked pre-checks and the owner shard lock,
    // so the probe reads exactly the pair the wake path's gate accepts
    // (rt_task_park.c): READY (0) and not enqueued.
    fprintf(stderr, "debt291 window: status=%u enqueued=%u\n",
            (unsigned)task_status_load(probe), (unsigned)task_enqueued_load(probe));
    if (task_status_load(probe) != TASK_READY || task_enqueued_load(probe) != 0) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("debt291 stand: the probe was not READY/unqueued at the window");
    }

    // What an awaiting poll does first (rt_task_poll, rt_async_task.c:220): a
    // target that is neither WAITING nor DONE is woken. Here that hands the
    // probe to the other worker, which polls it to completion -- all while the
    // first worker is still waiting for the shard lock.
    rt_control_lock(ex);
    wake_task(ex, probe->id, 0);
    rt_control_unlock(ex);
    int done_in_window = wait_task_status(probe, TASK_DONE, 4000);
    fprintf(stderr, "debt291 completed in window: done=%d\n", done_in_window);
    if (!done_in_window) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("debt291 stand: the woken probe was not completed inside the window");
    }

    // And what it does second (rt_async_task.c:244): the awaiter that finds the
    // target DONE takes its result and drops its handle. This is the last one.
    // Nothing may read *probe after this call -- in the negative control it is
    // freed here -- so the id is taken now.
    uint64_t probe_id = probe->id;
    task_release_lane_aware(ex, probe);

    // The rule, stated as a count. The task has completed and its last HANDLE
    // is gone; a reference still outstanding can only be the poller's own pin.
    // Read through the task table, not through the pointer: get_task answers
    // NULL for a task whose slot free_task cleared, so this reads no freed
    // memory in either build.
    rt_task* still_there = get_task(ex, probe_id);
    unsigned refs_after_drop = 0;
    unsigned completed = 0;
    if (still_there != NULL) {
        uint32_t word = atomic_load_explicit(&still_there->handle_refs, memory_order_acquire);
        refs_after_drop = (unsigned)(word & RT_TASK_REFS_COUNT_MASK);
        completed = (word & RT_TASK_REFS_COMPLETED) != 0 ? 1u : 0u;
    } else {
        // Freed: the slot is cleared by free_task itself, so "gone from the
        // table" is the free, and the completion is what the drop decided on.
        completed = 1;
    }
    fprintf(stderr, "debt291 drop: completed=%u refs_after_drop=%u\n", completed, refs_after_drop);

    // Release the held re-push. It now re-reads the task's status on the far
    // side of the owner shard lock (rt_ready_queue.c): a live task with the pin,
    // freed memory without it.
    rt_sync_point_open();
    // Long enough for that re-read to have happened before the executor is torn
    // down; the sanitizer halts the process at it in the negative control.
    sleep_us(200000);
    (void)rt_executor_request_shutdown(ex);
    if (refs_after_drop == 0) {
        return fail("debt291 stand: the last handle drop freed a task whose poll outcome was "
                    "still being applied -- the poller held no reference");
    }
    return 0;
}
#endif
`
