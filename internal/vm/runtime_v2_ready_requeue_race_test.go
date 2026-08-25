//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// The deterministic reproduction of the yielded-requeue-vs-wake double poll
// (RV2-DEBT-027): a worker's requeue push is held between its unlocked
// status/enqueued pre-checks and the owner shard lock, a wake enqueues the
// task in that window and a second worker starts polling it, and the
// released push must refuse the duplicate (the locked re-validation) instead
// of overwriting RUNNING with READY under the live poll.
func TestRuntimeV2LifecycleReadyRequeueWakeRaceProof(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessReadyRequeue(t, false)
	for _, threads := range []string{"2", "4"} {
		threads := threads
		t.Run("threads-"+threads, func(t *testing.T) {
			env := lifecycleEnv(
				"SURGE_SHARDS=1",
				"SURGE_THREADS="+threads,
				"SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_READY_REQUEUE_BEFORE_LOCK:block")
			stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "ready-requeue-wake-race", env)
			if exitCode != 0 {
				t.Fatalf("requeue-race positive proof failed at SURGE_THREADS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					threads, exitCode, stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2LifecycleReadyRequeueWakeRaceNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessReadyRequeue(t, true)
	env := lifecycleEnv(
		"SURGE_SHARDS=1",
		"SURGE_THREADS=2",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_READY_REQUEUE_BEFORE_LOCK:block")
	stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "ready-requeue-wake-race", env)
	if exitCode == 0 {
		t.Fatalf("requeue-race negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if !strings.Contains(stderr, "async: double poll") {
		t.Fatalf("requeue-race negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

func buildRuntimeV2LifecycleHarnessReadyRequeue(t *testing.T, negativeControl bool) string {
	t.Helper()
	name := "lifecycle_harness_ready_requeue"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if negativeControl {
		name += "_negative"
		flags = append(flags, "-DRV2_DEBT_027_NEGATIVE_CONTROL")
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

const lifecycleHarnessReadyRequeueModes = `
#ifdef RT_TEST_SYNC_POINTS
#define POLL_READY_REQUEUE_PROBE 4033

static _Atomic uint32_t g_requeue_probe_steps;
static _Atomic uint32_t g_requeue_probe_release;

// Yields once (so its requeue goes through the force-inject requeue path and
// reaches SP_READY_REQUEUE_BEFORE_LOCK), then holds its worker inside poll
// until the driver releases it - the "second poller is live" half of the
// requeue-vs-wake race.
static void poll_ready_requeue_probe(void) {
    uint32_t step = atomic_fetch_add_explicit(&g_requeue_probe_steps, 1, memory_order_acq_rel);
    if (step == 0) {
        rt_async_yield(NULL, 0);
        return;
    }
    while (atomic_load_explicit(&g_requeue_probe_release, memory_order_acquire) == 0) {
        sleep_us(1000);
    }
    rt_async_return(NULL, &(uint64_t){42});
}

static int mode_ready_requeue_wake_race(rt_executor* ex) {
    unsigned window_before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_READY_REQUEUE_BEFORE_LOCK);
    atomic_store_explicit(&g_requeue_probe_steps, 0, memory_order_release);
    atomic_store_explicit(&g_requeue_probe_release, 0, memory_order_release);
    rt_task* probe = spawn_pinned(ex, POLL_READY_REQUEUE_PROBE, 0);
    if (probe == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("requeue-race probe allocation failed");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_READY_REQUEUE_BEFORE_LOCK, window_before, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("requeue-race probe did not reach the requeue window");
    }
    // The yielding worker is held between its unlocked pre-checks and the
    // owner shard lock; the probe reads READY and not-enqueued, exactly the
    // state the wake gate accepts.
    if (task_status_load(probe) != TASK_READY || task_enqueued_load(probe) != 0) {
        (void)rt_executor_request_shutdown(ex);
        rt_sync_point_open();
        return fail("requeue-race probe was not READY/unqueued at the window");
    }
    rt_control_lock(ex);
    wake_task(ex, probe->id, 0);
    rt_control_unlock(ex);
    if (!wait_task_status(probe, TASK_RUNNING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        rt_sync_point_open();
        return fail("requeue-race wake did not hand the probe to a second worker");
    }
    rt_sync_point_open();
    // Give the released push time to act on the window: with the locked
    // re-validation it refuses the duplicate; the negative control pushes it
    // and a worker double-polls the probe (abort) before the release below.
    sleep_us(100000);
    atomic_store_explicit(&g_requeue_probe_release, 1, memory_order_release);
    if (!await_expect(ex, probe, 1, 42, "requeue-race probe")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
#endif
`
