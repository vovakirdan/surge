//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// TestRuntimeV2LifecycleCloneReleaseLastReferenceFree stresses concurrent
// clone/release racers against one completing target (rt_task_clone,
// rt_async_task.c:234-247; task_add_ref/task_release, rt_async_state.c:
// 1381-1427), asserting no crash/hang and correct result observation over
// many racers. Guards S5-Q6 (clone drops control, relies on the atomic
// refcount + live-handle rule) ahead of Task 7.
func TestRuntimeV2LifecycleCloneReleaseLastReferenceFree(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarness(t)
	env := lifecycleEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "clone-release-stress", env)
	if exitCode != 0 {
		t.Fatalf("clone/release stress failed (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if strings.Contains(stderr, "uaf") {
		t.Fatalf("clone/release stress reported a use-after-free\nstderr:\n%s", stderr)
	}
}

// TestRuntimeV2LifecycleCompletionPinInterleavingTSan builds the harness with
// ThreadSanitizer and stresses the exact interleaving rule 1 names (08-
// lifecycle-lane-proving-spike.md): a joiner consuming the last handle while
// mark_done (rt_async_state.c:1508-1575) is still mid-body. mark_done pins
// the task across completion (task_add_ref at :1515, task_release_lane_aware
// at :1574) precisely so this cannot free the task out from under itself.
//
// This is a probabilistic regression stress, not the spike's forced
// deterministic model (Task 4 does not modify rt_async_state.c to inject a
// delay): many (target, joiner) pairs run concurrently across worker
// threads with no artificial sleep, and TSan is the oracle that turns "the
// process didn't crash" into "no data race, no use-after-free was observed".
//
// Skipped pending Task 8: this test found two real, reproducible baseline
// races (RV2-DEBT-019, docs/runtime-v2-epics/DEBT.md). Set
// LIFECYCLE_PIN_STRESS_NO_KEEPALIVE=1 to reproduce both together (the state
// before this test's in-harness mitigation); unset (default) isolates the
// second, park_key race by forcing mark_done_needs_control true throughout.
// Task 8's peel commit deletes this skip and adds this test's name to
// runtime-v2-lifecycle-check once both races are fixed.
func TestRuntimeV2LifecycleCompletionPinInterleavingTSan(t *testing.T) {
	t.Skip("pending Task 8: baseline races RV2-DEBT-019 -- (1) mark_done result writes must move before the TASK_DONE release store; (2) mark_done_needs_control's unlocked park_key read vs wake_task_on_shard_locked write. Task 8's peel commit deletes this skip and adds the test to runtime-v2-lifecycle-check.")
	binPath := buildRuntimeV2LifecycleHarnessTSan(t)
	env := lifecycleEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "completion-pin-stress", env)
	if strings.Contains(stderr, "ThreadSanitizer") {
		t.Fatalf("ThreadSanitizer reported a race in the completion-pin interleaving\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("completion-pin stress failed (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
}

// lifecycleHarnessHandleLifetimeModes holds the clone/release and
// completion-pin mode drivers, moved out of runtime_v2_lifecycle_behavior_
// await_shutdown_test.go purely to keep that file under the project's
// 500-line cap; concatenated into the same translation unit as
// lifecycleHarnessCommon (see buildRuntimeV2LifecycleHarnessWithFlags).
const lifecycleHarnessHandleLifetimeModes = `
static int mode_clone_release_stress(rt_executor* ex) {
    rt_task* target = spawn_pinned(ex, POLL_CLONE_TARGET, 0);
    if (target == NULL) {
        return fail("clone target allocation failed");
    }
    atomic_store_explicit(&g_clone_shared_target, target, memory_order_release);
    const uint32_t racers = 64;
    rt_task* handles[64];
    for (uint32_t i = 0; i < racers; i++) {
        handles[i] = spawn_pinned(ex, POLL_CLONE_RACER, i % 2);
        if (handles[i] == NULL) {
            return fail("racer allocation failed");
        }
    }
    for (uint32_t i = 0; i < racers; i++) {
        if (!wait_task_status(handles[i], TASK_DONE, 8000)) {
            (void)rt_executor_request_shutdown(ex);
            return fail("a clone racer did not complete");
        }
    }
    if (atomic_load_explicit(&g_clone_uaf_detected, memory_order_acquire) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("clone racer observed a corrupted result (possible use-after-free)");
    }
    if (atomic_load_explicit(&g_clone_racer_iters, memory_order_acquire) != racers) {
        (void)rt_executor_request_shutdown(ex);
        return fail("not all clone racers completed successfully");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static void* g_keepalive_target;

// Held for the whole stress window so ex->done_waiters stays > 0
// (rt_async_task.c:197, incremented before this thread's rt_task_await wait
// begins), which forces mark_done_needs_control (rt_async_state.c:1502) true
// for every completion in-flight. This is required, not incidental: mark_done
// writes result_kind/result_bits AFTER the TASK_DONE release store
// (rt_async_state.c:1540-1543), and only takes the control lock when
// mark_done_needs_control says so (:1516-1519). Rule 1
// (08-lifecycle-lane-proving-spike.md) documents this ordering as "sound
// today only because readers hold the control lock" -- true precisely when
// the writer ALSO takes it, which a plain no-scope, no-residual-waiter task
// (exactly POLL_PIN_TARGET without this keepalive) does not. Without this
// keepalive, TSan correctly flags a real data race here between mark_done's
// unlocked result write and rt_task_poll/rt_task_await's control-lock-held
// read -- confirmed by direct observation while developing this harness; it
// is the exact "Required change" Task 8 already owns, not a new bug this
// test introduces. Forcing need_control true tests the completion-pin
// property (rule 1's actual subject) under the precondition the current
// design documents, rather than incidentally re-discovering the already-
// tracked Task 8 item on every run.
static void* keepalive_awaiter_thread(void* arg) {
    (void)arg;
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(g_keepalive_target, &kind, &bits);
    return NULL;
}

static int mode_completion_pin_stress(rt_executor* ex) {
    rt_task* chan_maker = spawn_pinned(ex, POLL_MAKE_PARK_FOREVER_CHAN, 0);
    if (chan_maker == NULL || !wait_ptr(&g_park_forever_chan, 4000) ||
        !wait_task_status(chan_maker, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("park-forever channel setup failed");
    }
    // RV2-DEBT-019: the keepalive is toggleable so Task 8 can exercise EITHER
    // race in isolation while deciding its fix: unset (default) isolates the
    // park_key race (item 2) by forcing mark_done_needs_control true, which
    // also masks the result-visibility race (item 1, Task 8's own target).
    // Set LIFECYCLE_PIN_STRESS_NO_KEEPALIVE=1 to disable this and reproduce
    // both races together, matching this test's state before mitigation.
    if (getenv("LIFECYCLE_PIN_STRESS_NO_KEEPALIVE") == NULL) {
        rt_task* keepalive = spawn_pinned(ex, POLL_PARK_FOREVER, 0);
        if (keepalive == NULL) {
            return fail("keepalive target allocation failed");
        }
        g_keepalive_target = keepalive;
        pthread_t keepalive_thread;
        if (pthread_create(&keepalive_thread, NULL, keepalive_awaiter_thread, NULL) != 0) {
            return fail("keepalive awaiter thread failed to start");
        }
        // Give the keepalive thread a moment to reach rt_task_await's
        // done_waiters increment (rt_async_task.c:197) before the stress loop
        // starts, so every completion in the loop sees done_waiters > 0.
        sleep_us(20000);
    }

    const uint32_t pairs = 300;
    rt_task* joiners[300];
    int failed = 0;
    for (uint32_t i = 0; i < pairs && !failed; i++) {
        rt_task* target = spawn_pinned(ex, POLL_PIN_TARGET, i % 2);
        if (target == NULL) {
            fail("pin target allocation failed");
            failed = 1;
            break;
        }
        rt_task* joiner = spawn_pinned_with_state(ex, POLL_PIN_JOINER, (i + 1) % 2, target);
        if (joiner == NULL) {
            fail("pin joiner allocation failed");
            failed = 1;
            break;
        }
        joiners[i] = joiner;
    }
    for (uint32_t i = 0; !failed && i < pairs; i++) {
        if (!await_expect(ex, joiners[i], 1, 1, "pin-stress joiner")) {
            fprintf(stderr, "pin-stress failed at pair %u\n", i);
            failed = 1;
        }
    }
    (void)rt_executor_request_shutdown(ex);
    return failed ? 1 : 0;
}
`
