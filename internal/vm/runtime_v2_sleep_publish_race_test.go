//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// The deterministic reproduction of the false-idle window that let the virtual
// clock run past a deadline (RV2-DEBT-190).
//
// The rate this defect showed up at is why it needs a proof rather than a
// sweep: the first fix took a select fixture from 19 hangs in 240 runs to none
// in 840, which was read as a closure and was not one — a re-measurement at
// more workers still found about 4 hangs in 6900. At that rate a clean run of a
// few hundred means nothing, so the window is held open on purpose here and the
// question is asked directly.
//
// The window: rt_sleep_fire_due_on_shard pops the due batch under the shard
// lock and wakes outside it, because waking takes the target's owner lock and
// that can be the same mutex. Between the two the sleeper is in no sleep store,
// no ready queue and no running count. Idleness is not just a report in this
// runtime — it is the predicate advance_time_to_next_timer moves the clock on —
// so a sample landing in that gap jumps time past the very deadline that fired.
func TestRuntimeV2LifecycleSleepFiredBatchIsNotIdleProof(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessSleepPublish(t, false)
	// Two workers and up. At SURGE_THREADS=1 the window cannot be built at all
	// — the harness reports that no sleeper ever reached it — because the
	// single-runner path fires the batch on the same thread that would sample,
	// so there is no concurrent observer to catch in the gap. That agrees with
	// how the defect measured in the first place: the select fixture hung 0
	// times in 20 at one worker and roughly one run in six at two.
	for _, threads := range []string{"2", "4", "8"} {
		threads := threads
		t.Run("threads-"+threads, func(t *testing.T) {
			env := lifecycleEnv(
				"SURGE_SHARDS=1",
				"SURGE_THREADS="+threads,
				"SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_SLEEP_FIRED_BEFORE_WAKE:block")
			stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "sleep-fired-idle-sample", env)
			if exitCode != 0 {
				t.Fatalf("sleep-publish positive proof failed at SURGE_THREADS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					threads, exitCode, stdout, stderr)
			}
		})
	}
}

// Non-vacuity. With the in-flight claim made inert the same held window must be
// reported idle — otherwise the positive test above proves nothing about the
// counter and would keep passing if it were deleted.
func TestRuntimeV2LifecycleSleepFiredBatchIsNotIdleNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessSleepPublish(t, true)
	env := lifecycleEnv(
		"SURGE_SHARDS=1",
		"SURGE_THREADS=2",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_SLEEP_FIRED_BEFORE_WAKE:block")
	stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "sleep-fired-idle-sample", env)
	if exitCode == 0 {
		t.Fatalf("sleep-publish negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if !strings.Contains(stderr, "idle while a fired sleeper was in flight") {
		t.Fatalf("sleep-publish negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

func buildRuntimeV2LifecycleHarnessSleepPublish(t *testing.T, negativeControl bool) string {
	t.Helper()
	name := "lifecycle_harness_sleep_publish"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if negativeControl {
		name += "_negative"
		flags = append(flags, "-DRV2_DEBT_190_NEGATIVE_CONTROL")
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

const lifecycleHarnessSleepPublishModes = `
#ifdef RT_TEST_SYNC_POINTS
// Hold the thread that just popped a due sleeper, between the pop and the wake,
// and ask the executor whether it is idle. It is not: the sleeper is in flight.
// Answering yes is what let the clock advance past a deadline already due.
static int mode_sleep_fired_idle_sample(rt_executor* ex) {
    unsigned before = rt_sync_point_reached_count(RT_SYNC_POINT_SP_SLEEP_FIRED_BEFORE_WAKE);
    void* sleeper = rt_sleep(5);
    if (sleeper == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("sleep task allocation failed");
    }
    // Nothing else has work, so the io thread advances the clock to the
    // deadline and fires the sleeper - which is what walks into the window.
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_SLEEP_FIRED_BEFORE_WAKE, before, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("no sleeper reached the fired-before-wake window");
    }
    // The firing thread is parked at the window holding no shard lock, so this
    // is exactly the sample the io thread takes before it advances time.
    int idle = rt_sched_idle_sample_locked(ex);
    rt_sync_point_open();
    if (idle != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("executor reported idle while a fired sleeper was in flight");
    }
    if (!wait_task_status((rt_task*)sleeper, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("released sleeper never completed");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
#endif
`
