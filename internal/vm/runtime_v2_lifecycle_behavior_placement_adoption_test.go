//go:build runtime_v2_pending

package vm_test

import (
	"testing"
)

// TestRuntimeV2LifecycleJoinConsumePlacementAdoption proves F2 (Epic 8 Task
// 11 net-fairness fix, RV2-DEBT-015, folded into Task 7 by main-session
// decision 2026-07-04): rt_task_poll_adopt_placement (rt_async_task.c),
// called from both DONE-consume branches of rt_task_poll, makes a joiner
// adopt a DONE child's placement when that child carries
// TASK_PLACEMENT_CONNECTION -- and does NOT adopt anything from a plain
// TASK_PLACEMENT_GENERIC child. The negative case is as load-bearing as the
// positive one (main's explicit review requirement): a joiner on shard 0
// consumes a target pinned to shard 1; the positive subtest's target is
// connection-placed and the joiner's own owner_shard_id/placement_class must
// become shard-1/CONNECTION afterward, while the negative subtest's target is
// generic-placed and the joiner's placement must be unchanged.
func TestRuntimeV2LifecycleJoinConsumePlacementAdoption(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarness(t)
	env := lifecycleEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1")
	t.Run("positive-connection-placed", func(t *testing.T) {
		stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "placement-adopt-positive", env)
		if exitCode != 0 {
			t.Fatalf("placement adoption (positive) failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
				exitCode, stdout, stderr)
		}
	})
	t.Run("negative-generic-placed", func(t *testing.T) {
		stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "placement-adopt-negative", env)
		if exitCode != 0 {
			t.Fatalf("placement adoption (negative) failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
				exitCode, stdout, stderr)
		}
	})
}

// lifecycleHarnessPlacementAdoption holds the F2 mode drivers, concatenated
// into the same translation unit as lifecycleHarnessCommon (see
// buildRuntimeV2LifecycleHarnessWithFlags in
// runtime_v2_lifecycle_behavior_harness_test.go).
const lifecycleHarnessPlacementAdoption = `
static _Atomic uint32_t g_adopt_joiner_ran;
static _Atomic uint32_t g_adopt_joiner_owner_shard;
static _Atomic uint32_t g_adopt_joiner_placement_class;

// spawn_placed mirrors spawn_pinned_with_state but takes an explicit
// placement_class instead of hardcoding TASK_PLACEMENT_CONNECTION, so this
// test can construct a TASK_PLACEMENT_GENERIC target for the negative case.
static rt_task* spawn_placed(rt_executor* ex,
                             int64_t poll_fn_id,
                             uint32_t wanted_shard,
                             uint8_t placement_class,
                             void* state) {
    rt_control_lock(ex);
    rt_task* task = alloc_ready_task(ex, poll_fn_id);
    if (task != NULL) {
        task->state = state;
        rt_task_set_placement(task, pin_shard(ex, wanted_shard), placement_class);
        ready_push(ex, task->id);
    }
    rt_control_unlock(ex);
    return task;
}

static void poll_adopt_target(void) {
    rt_async_return(NULL, 55);
}

// Joins its own __task_state target via rt_task_poll (the worker join lane
// rt_task_poll_adopt_placement instruments), then -- after the join
// completes -- records this task's OWN post-join owner_shard_id/
// placement_class, which is exactly what rt_task_poll_adopt_placement may
// have just mutated (current == this task, target == the joined child).
static void poll_adopt_joiner(void) {
    void* target = __task_state();
    uint64_t bits = 0;
    uint8_t st = rt_task_poll(target, &bits);
    if (st == 0) {
        rt_async_yield(target);
        return;
    }
    const rt_task* self = rt_current_task();
    atomic_store_explicit(&g_adopt_joiner_owner_shard, self->owner_shard_id, memory_order_release);
    atomic_store_explicit(
        &g_adopt_joiner_placement_class, self->placement_class, memory_order_release);
    atomic_store_explicit(&g_adopt_joiner_ran, 1, memory_order_release);
    rt_async_return(NULL, st == 1 && bits == 55 ? 1 : 0);
}

static int mode_placement_adopt(rt_executor* ex, uint8_t target_class, uint8_t want_class) {
    atomic_store_explicit(&g_adopt_joiner_ran, 0, memory_order_relaxed);
    atomic_store_explicit(&g_adopt_joiner_owner_shard, UINT32_MAX, memory_order_relaxed);
    atomic_store_explicit(&g_adopt_joiner_placement_class, UINT32_MAX, memory_order_relaxed);
    rt_task* target = spawn_placed(ex, POLL_ADOPT_TARGET, 1, target_class, NULL);
    if (target == NULL) {
        return fail("adopt target allocation failed");
    }
    rt_task* joiner = spawn_placed(ex, POLL_ADOPT_JOINER, 0, TASK_PLACEMENT_GENERIC, target);
    if (joiner == NULL) {
        return fail("adopt joiner allocation failed");
    }
    if (!await_expect(ex, joiner, 1, 1, "placement-adopt joiner")) {
        return 1;
    }
    if (!wait_u32_at_least(&g_adopt_joiner_ran, 1, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("adopt joiner never recorded post-join placement");
    }
    uint32_t got_shard = atomic_load_explicit(&g_adopt_joiner_owner_shard, memory_order_acquire);
    uint32_t got_class =
        atomic_load_explicit(&g_adopt_joiner_placement_class, memory_order_acquire);
    // want_shard mirrors want_class's expectation: adoption -> shard 1 (the
    // target's shard); no adoption -> shard 0 (the joiner's own spawn shard).
    uint32_t want_shard = pin_shard(ex, want_class == TASK_PLACEMENT_CONNECTION ? 1 : 0);
    (void)rt_executor_request_shutdown(ex);
    if (got_shard != want_shard || got_class != want_class) {
        fprintf(stderr,
                "placement-adopt: got shard=%u class=%u want shard=%u class=%u\n",
                got_shard,
                got_class,
                want_shard,
                (unsigned)want_class);
        return fail("joiner placement after join did not match expectation");
    }
    return 0;
}

static int mode_placement_adopt_positive(rt_executor* ex) {
    return mode_placement_adopt(ex, TASK_PLACEMENT_CONNECTION, TASK_PLACEMENT_CONNECTION);
}

static int mode_placement_adopt_negative(rt_executor* ex) {
    return mode_placement_adopt(ex, TASK_PLACEMENT_GENERIC, TASK_PLACEMENT_GENERIC);
}
`
