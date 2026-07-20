//go:build runtime_v2_pending

package vm_test

import (
	"testing"
)

// TestRuntimeV2LifecycleJoinConsumePlacementAdoption proves F2 (RV2-DEBT-015)
// net-fairness behavior: join consumption adopts the child's placement.
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
        rt_async_yield(target, 0);
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

// lifecycleHarnessScopeCrossOwner is the RV2-DEBT-021 deterministic
// cross-owner scope-completion driver. It is concatenated after
// lifecycleHarnessPlacementAdoption (it reuses that block's spawn_placed
// helper) and before lifecycleHarnessMain. See
// TestRuntimeV2LifecycleScopeCrossOwnerChildDone
// (runtime_v2_lifecycle_behavior_scope_test.go) for the contract.
const lifecycleHarnessScopeCrossOwner = `
// RV2-DEBT-021: the cross-owner scope_on_child_done control fallback
// (rt_async_scope.c:340-377) is the sole producer of the residual ctrl_scope
// on the 8x1024 net bench, but before this test only that benchmark and the
// no-keepalive completion-pin TSan stress exercised it -- neither
// deterministic in CI. This drives it deterministically via the REAL F2
// machinery: a scope owner pinned to shard 0 registers a scope-child while it
// is still same-owner (shard 0), then parks in join_all; the scope-child then
// adopts a shard-1 CONNECTION placement by consuming a connection-placed
// grandchild through rt_task_poll (rt_task_poll_adopt_placement, exactly the
// production F2 path), so when it completes its owner_shard_id (1) != the
// scope's pinned shard (0) and scope_on_child_done takes the counted
// cross-owner control fallback, waking the owner cross-shard. Needs
// SURGE_SHARDS>=2; at SHARDS=1 the grandchild clamps to shard 0 and the
// completion stays same-owner, so the test only sweeps 2 and 8.

static _Atomic(void*) g_xowner_grandchild;
static _Atomic uint32_t g_xowner_registered;
static _Atomic uint32_t g_xowner_go;
static _Atomic uint32_t g_xowner_child_shard;

static void poll_xowner_grandchild(void) {
    rt_async_return(NULL, 55);
}

// Gated on g_xowner_go, which the driver sets only after the owner is parked
// in join_all, so the completion drives a genuine cross-shard wake rather than
// racing the owner's park. On release it joins the connection-placed
// grandchild -- F2 adopts shard-1 CONNECTION onto THIS task -- then returns,
// completing cross-owner. The grandchild handle is re-read from the global on
// each poll (not the one-shot __task_state, which clears after the first read)
// so a not-yet-DONE first poll can yield and retry safely.
static void poll_xowner_scope_child(void) {
    if (atomic_load_explicit(&g_xowner_go, memory_order_acquire) == 0) {
        rt_async_yield(NULL, 0);
        return;
    }
    void* gc = atomic_load_explicit(&g_xowner_grandchild, memory_order_acquire);
    uint64_t bits = 0;
    uint8_t st = rt_task_poll(gc, &bits);
    if (st == 0) {
        rt_async_yield(gc, 0);
        return;
    }
    const rt_task* self = rt_current_task();
    atomic_store_explicit(&g_xowner_child_shard, self->owner_shard_id, memory_order_release);
    rt_async_return(NULL, st == 1 && bits == 55 ? 1 : 0);
}

static void poll_xowner_owner(void) {
    uint32_t phase = atomic_load_explicit(&g_scope_owner_phase, memory_order_acquire);
    if (phase == 0) {
        void* handle = rt_scope_enter(false);
        atomic_store_explicit(&g_scope_handle, handle, memory_order_release);
        // Same-owner at register time (child inherits the owner's shard 0);
        // the cross-owner placement is adopted only later, when the child
        // consumes the grandchild (which it reads from g_xowner_grandchild).
        void* child = __task_create(POLL_XOWNER_SCOPE_CHILD, NULL);
        atomic_store_explicit(&g_scope_child_a, child, memory_order_release);
        rt_scope_register_child(handle, child);
        atomic_store_explicit(&g_xowner_registered, 1, memory_order_release);
        atomic_store_explicit(&g_scope_owner_phase, 1, memory_order_release);
        rt_async_yield(NULL, 0);
        return;
    }
    void* handle = atomic_load_explicit(&g_scope_handle, memory_order_acquire);
    uint64_t pending = 0;
    bool failfast = false;
    bool done = rt_scope_join_all(handle, &pending, &failfast);
    if (!done) {
        rt_async_yield(NULL, 0);
        return;
    }
    rt_scope_exit(handle);
    rt_async_return(NULL, 0);
}

static int mode_scope_cross_owner(rt_executor* ex) {
    atomic_store_explicit(&g_scope_owner_phase, 0, memory_order_relaxed);
    atomic_store_explicit(&g_scope_handle, NULL, memory_order_relaxed);
    atomic_store_explicit(&g_scope_child_a, NULL, memory_order_relaxed);
    atomic_store_explicit(&g_xowner_grandchild, NULL, memory_order_relaxed);
    atomic_store_explicit(&g_xowner_registered, 0, memory_order_relaxed);
    atomic_store_explicit(&g_xowner_go, 0, memory_order_relaxed);
    atomic_store_explicit(&g_xowner_child_shard, UINT32_MAX, memory_order_relaxed);

    // The F2 adoption source: a grandchild connection-placed on shard 1.
    rt_task* grandchild =
        spawn_placed(ex, POLL_XOWNER_GRANDCHILD, 1, TASK_PLACEMENT_CONNECTION, NULL);
    if (grandchild == NULL) {
        return fail("cross-owner grandchild allocation failed");
    }
    atomic_store_explicit(&g_xowner_grandchild, grandchild, memory_order_release);

    rt_task* owner = spawn_pinned(ex, POLL_XOWNER_OWNER, 0);
    if (owner == NULL) {
        return fail("cross-owner scope owner allocation failed");
    }
    // The owner must register the scope-child and be parked in join_all before
    // the child is released, so scope_on_child_done drives a real cross-shard
    // wake instead of the owner self-consuming at its join_all re-check.
    if (!wait_u32_at_least(&g_xowner_registered, 1, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("owner did not register its scope-child");
    }
    if (!wait_task_status(owner, TASK_WAITING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("owner did not park in join_all before child release");
    }
    atomic_store_explicit(&g_xowner_go, 1, memory_order_release);

    if (!await_expect(ex, owner, 1, 0, "cross-owner scope owner")) {
        return 1;
    }
    // The scope-child must have adopted the grandchild's shard (1), which
    // differs from the scope's pinned shard (0) -- proving scope_on_child_done
    // actually took the cross-owner branch.
    uint32_t child_shard = atomic_load_explicit(&g_xowner_child_shard, memory_order_acquire);
    uint32_t adopted_shard = pin_shard(ex, 1);
    (void)rt_executor_request_shutdown(ex);
    if (child_shard != adopted_shard) {
        fprintf(stderr, "cross-owner: scope-child owner shard=%u, expected adopted shard=%u\n",
                child_shard, adopted_shard);
        return fail("scope-child did not adopt the cross-owner shard");
    }
    return 0;
}
`
