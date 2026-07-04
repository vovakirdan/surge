//go:build runtime_v2_pending

package vm_test

import (
	"testing"
)

// TestRuntimeV2LifecycleOwnerLocalCreateAndReadyPublication proves the
// create -> owner-shard assign -> ready-push -> run path stays on the
// assigned owner shard today (rt_async_task.c:8-44 __task_create,
// rt_task_assign_spawn_owner:40, ready_push:41), across SURGE_SHARDS=1,2,8.
// This guards S5-Q1 (08-lifecycle-lane-proving-spike.md): Task 6 may move
// slot-publish + ready-push to the owner shard lock, but the observable
// contract -- a task placed on shard N runs on shard N's worker -- must not
// change.
func TestRuntimeV2LifecycleOwnerLocalCreateAndReadyPublication(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarness(t)
	runLifecycleModeAcrossShards(t, binPath, "owner-local-create")
}

// TestRuntimeV2LifecycleJoinPollResultObservation proves rt_task_poll
// (rt_async_task.c:79-149) observes the correct result_kind/result_bits for
// both a same-shard and a cross-shard join, across SURGE_SHARDS=1,2,8. This
// is the direct regression guard for rule 2 (join result visibility) and
// S5-Q3 (join register-then-verify under the target-owner store lock alone),
// which Task 7 implements against.
func TestRuntimeV2LifecycleJoinPollResultObservation(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarness(t)
	t.Run("same-shard", func(t *testing.T) {
		runLifecycleModeAcrossShards(t, binPath, "join-same-shard")
	})
	t.Run("cross-shard", func(t *testing.T) {
		runLifecycleModeAcrossShards(t, binPath, "join-cross-shard")
	})
}

// TestRuntimeV2LifecycleJoinWaiterCleanupRegisterThenVerify covers the three
// timing cases from the epic's focused-probe list ("join waiter cleanup when
// the target completes before, during, and after registration"):
//
//   - "pre-done": the target is fully TASK_DONE before the joiner ever calls
//     rt_task_poll, exercising the immediate DONE short-circuit
//     (rt_async_task.c:116-124).
//   - "race-stress": target and joiner are spawned together with the target
//     completing on its very first poll (no yield), stressing the
//     register-then-verify window (rt_async_task.c:127-145) over many
//     iterations. This cannot force the exact interleaving deterministically
//     without touching rt_async_task.c/rt_async_state.c (out of scope for
//     Task 4); the iteration count is the mitigation, matching the Task 3
//     spike's own justification for its TSan model's iteration counts.
//   - "post-park": the target yields several times before completing, so the
//     joiner parks first and is woken later by the normal completion drain
//     (wake_key_all_with_policy, rt_async_state.c:1565) -- the same case
//     TestRuntimeV2LifecycleJoinPollResultObservation's spin-target path
//     already exercises; this test asserts it explicitly under its own name
//     per the epic's probe list rather than only incidentally.
func TestRuntimeV2LifecycleJoinWaiterCleanupRegisterThenVerify(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarness(t)
	env := lifecycleEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1")

	t.Run("pre-done", func(t *testing.T) {
		stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "join-cleanup-pre-done", env)
		if exitCode != 0 {
			t.Fatalf("pre-done join cleanup failed (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
		}
	})
	t.Run("race-stress", func(t *testing.T) {
		stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "join-cleanup-race-stress", env)
		if exitCode != 0 {
			t.Fatalf("register-then-verify race stress failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
				exitCode, stdout, stderr)
		}
	})
	t.Run("post-park", func(t *testing.T) {
		stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "join-cleanup-post-park", env)
		if exitCode != 0 {
			t.Fatalf("post-park join cleanup failed (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
		}
	})
}

// lifecycleHarnessCreateJoinModes holds the create/join mode drivers, moved
// out of runtime_v2_lifecycle_behavior_await_shutdown_test.go purely to keep
// that file under the project's 500-line cap; concatenated into the same
// translation unit as lifecycleHarnessCommon (see
// buildRuntimeV2LifecycleHarnessWithFlags).
const lifecycleHarnessCreateJoinModes = `
static int mode_owner_local_create(rt_executor* ex) {
    for (uint32_t wanted = 0; wanted < 3; wanted++) {
        atomic_store_explicit(&g_owner_probe_ran, 0, memory_order_relaxed);
        atomic_store_explicit(&g_owner_probe_shard, UINT32_MAX, memory_order_relaxed);
        rt_task* probe = spawn_pinned(ex, POLL_OWNER_PROBE, wanted);
        if (probe == NULL) {
            return fail("owner probe allocation failed");
        }
        if (!wait_u32_at_least(&g_owner_probe_ran, 1, 8000)) {
            (void)rt_executor_request_shutdown(ex);
            return fail("owner probe did not run");
        }
        if (!await_expect(ex, probe, 1, 0, "owner probe")) {
            return 1;
        }
        if (atomic_load_explicit(&g_owner_probe_shard, memory_order_acquire) == UINT32_MAX) {
            (void)rt_executor_request_shutdown(ex);
            return fail("owner probe ran on a non-owner shard");
        }
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_join_same_shard(rt_executor* ex) {
    atomic_store_explicit(&g_join_spin_steps, 0, memory_order_relaxed);
    rt_task* target = spawn_pinned(ex, POLL_JOIN_TARGET_SPIN, 0);
    if (target == NULL) {
        return fail("target allocation failed");
    }
    g_join_target = target;
    rt_task* joiner = spawn_pinned(ex, POLL_JOINER, 0);
    if (joiner == NULL) {
        return fail("joiner allocation failed");
    }
    if (!await_expect(ex, joiner, 1, 42, "same-shard joiner")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_join_cross_shard(rt_executor* ex) {
    atomic_store_explicit(&g_join_spin_steps, 0, memory_order_relaxed);
    rt_task* target = spawn_pinned(ex, POLL_JOIN_TARGET_SPIN, 1);
    if (target == NULL) {
        return fail("target allocation failed");
    }
    g_join_target = target;
    rt_task* joiner = spawn_pinned(ex, POLL_JOINER, 0);
    if (joiner == NULL) {
        return fail("joiner allocation failed");
    }
    if (!await_expect(ex, joiner, 1, 42, "cross-shard joiner")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_join_cleanup_pre_done(rt_executor* ex) {
    rt_task* target = spawn_pinned(ex, POLL_JOIN_TARGET_QUICK, 0);
    if (target == NULL) {
        return fail("target allocation failed");
    }
    if (!wait_task_status(target, TASK_DONE, 8000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("target did not reach DONE before registration");
    }
    g_join_target = target;
    rt_task* joiner = spawn_pinned(ex, POLL_JOINER, 1);
    if (joiner == NULL) {
        return fail("joiner allocation failed");
    }
    if (!await_expect(ex, joiner, 1, 7, "pre-done joiner")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_join_cleanup_race_stress(rt_executor* ex) {
    const uint32_t iterations = 500;
    for (uint32_t i = 0; i < iterations; i++) {
        rt_task* target = spawn_pinned(ex, POLL_JOIN_TARGET_QUICK, i % 3);
        if (target == NULL) {
            return fail("race target allocation failed");
        }
        g_join_target = target;
        rt_task* joiner = spawn_pinned(ex, POLL_JOINER, (i + 1) % 3);
        if (joiner == NULL) {
            return fail("race joiner allocation failed");
        }
        if (!await_expect(ex, joiner, 1, 7, "race-stress joiner")) {
            fprintf(stderr, "race-stress failed at iteration %u\n", i);
            return 1;
        }
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_join_cleanup_post_park(rt_executor* ex) {
    atomic_store_explicit(&g_join_target_release, 0, memory_order_relaxed);
    rt_task* target = spawn_pinned(ex, POLL_JOIN_TARGET_GATED, 0);
    if (target == NULL) {
        return fail("target allocation failed");
    }
    g_join_target = target;
    rt_task* joiner = spawn_pinned(ex, POLL_JOINER, 1);
    if (joiner == NULL) {
        return fail("joiner allocation failed");
    }
    if (!wait_task_status(joiner, TASK_WAITING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("joiner did not park before the target was released");
    }
    atomic_store_explicit(&g_join_target_release, 1, memory_order_release);
    if (!await_expect(ex, joiner, 1, 42, "post-park joiner")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
`
