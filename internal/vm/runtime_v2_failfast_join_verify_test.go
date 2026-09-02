//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// RV2-DEBT-261. rt_scope_join_all answers two questions -- has the set
// drained, and did fail-fast fire -- and scope_on_child_done decides both in
// one critical section: the cancelled child that trips the flag is retired
// from the count under the same lock. The join's register-then-verify re-check
// used to re-read only the count. A cancelled completion landing between the
// first snapshot and the verify therefore answered "drained" with the flag
// from before it, and the generated tail of a @failfast block
// (insertScopeJoins, internal/mir/async_lowering_state_machine.go) took the
// Success branch after a child had been cancelled -- the exit 12 / exit 13
// that TestMTStructuredConcurrency reported in 2 of 6 pinned runs.
//
// The drive holds the owner at SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY (after its
// scope_key registration, before the verify), cancels the only child there,
// waits until the scope has TAKEN that completion -- count 0, flag set, both
// read under the pinned lock and printed -- and releases. The owner's poll
// mirrors the generated tail exactly: join, exit, Cancelled when the join says
// fail-fast fired and Success otherwise. The positive run asserts Cancelled;
// the negative control drops the flag from the verify and MUST observe Success
// at that same point, which is what shows the window this drive builds is the
// window the fix closes.
func TestRuntimeV2LifecycleDebt261FailfastJoinVerifyProof(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt261(t, false)
	// SURGE_SHARDS=1 with two workers and up: the held owner is a RUNNING task
	// blocked inside its poll, so the child it registered needs ANOTHER worker
	// on the SAME shard to complete -- one worker deadlocks in the window, and
	// a child on another shard would not be this scope's same-owner path.
	for _, threads := range []string{"2", "4", "8"} {
		t.Run("threads-"+threads, func(t *testing.T) {
			env := lifecycleEnv(
				"SURGE_SHARDS=1",
				"SURGE_THREADS="+threads,
				"SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY:block")
			stdout, stderr, exitCode := runLifecycleHarness(
				t, binPath, "debt261-failfast-join-verify", env)
			if exitCode != 0 {
				t.Fatalf("DEBT-261 positive proof failed at SURGE_THREADS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					threads, exitCode, stdout, stderr)
			}
			// Non-vacuity: the completion landed INSIDE the held window with
			// both answers changed, or the verify had nothing to re-read and
			// the run proves nothing about it.
			if !strings.Contains(stderr, "debt261 landed: active=0 failfast_triggered=1") {
				t.Fatalf("DEBT-261 positive proof did not land the cancelled completion inside the window\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2LifecycleDebt261FailfastJoinVerifyNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt261(t, true)
	env := lifecycleEnv(
		"SURGE_SHARDS=1",
		"SURGE_THREADS=2",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY:block")
	stdout, stderr, exitCode := runLifecycleHarness(
		t, binPath, "debt261-failfast-join-verify", env)
	if exitCode == 0 {
		t.Fatalf("DEBT-261 negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	// The strand must be AT the verify, on a window built the same way: the
	// completion landed, both answers changed, and the join still said Success.
	if !strings.Contains(stderr, "debt261 landed: active=0 failfast_triggered=1") {
		t.Fatalf("DEBT-261 negative control did not build the window (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	const want = "debt261 failfast scope answered Success after a cancelled child"
	if !strings.Contains(stderr, want) {
		t.Fatalf("DEBT-261 negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

func buildRuntimeV2LifecycleHarnessDebt261(t *testing.T, negativeControl bool) string {
	t.Helper()
	name := "lifecycle_harness_debt261"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if negativeControl {
		name += "_negative"
		flags = append(flags, "-DRV2_DEBT_261_NEGATIVE_CONTROL")
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

// lifecycleHarnessFailfastJoinModes is concatenated into the shared lifecycle
// harness translation unit by buildRuntimeV2LifecycleHarnessWithFlags.
const lifecycleHarnessFailfastJoinModes = `
#ifdef RT_TEST_SYNC_POINTS
#define POLL_DEBT261_SCOPE_OWNER 4035

static _Atomic uint32_t g_debt261_owner_entered;
static _Atomic(void*) g_debt261_scope_handle;
static _Atomic(void*) g_debt261_child;

// The generated tail of a @failfast block (insertScopeJoins): join the set,
// exit the scope, then Cancelled when the join says fail-fast fired and
// Success otherwise. One child that ends only by cancellation, so the set can
// drain through the fail-fast path alone. The first poll enters the scope,
// registers the child and goes straight into the join, which reaches the
// verify window with that live child in hand.
//
// The child is created by the OWNER, inside its scope: creation is the sole
// writer of membership, and a child spawned by the driver and handed over
// afterwards is refused rather than counted. What the driver's spawn used to
// buy is kept by spawn_pinned_in_scope forcing the push onto the inject queue
// with the workers signalled -- a worker's own spawn lands on its local tail
// and a single local entry signals nobody (ready_push_task_locked,
// rt_ready_queue.c), so a child pushed locally by an owner that is then held
// at the sync point inside this same poll would never be popped, the pusher
// being the held worker, and its cancellation could never be observed.
static void poll_debt261_scope_owner(void) {
    if (atomic_load_explicit(&g_debt261_owner_entered, memory_order_acquire) == 0) {
        void* handle = rt_scope_enter(true);
        rt_task* child = spawn_pinned_in_scope(ensure_exec(), POLL_SPIN_FOREVER, 0);
        atomic_store_explicit(&g_debt261_child, child, memory_order_release);
        atomic_store_explicit(&g_debt261_scope_handle, handle, memory_order_release);
        atomic_store_explicit(&g_debt261_owner_entered, 1, memory_order_release);
    }
    void* handle = atomic_load_explicit(&g_debt261_scope_handle, memory_order_acquire);
    uint64_t pending = 0;
    bool failfast = false;
    if (!rt_scope_join_all(handle, &pending, &failfast)) {
        rt_async_yield(NULL, 0);
        return;
    }
    rt_scope_exit(handle);
    if (failfast) {
        rt_async_return_cancelled(NULL, 0);
        return;
    }
    rt_async_return(NULL, &(uint64_t){0});
}

// Both answers under the pinned lock, the way join_all must read them. A scope
// that is already gone reads as SIZE_MAX so the driver names that case apart.
static size_t debt261_scope_snapshot(rt_executor* ex, uint64_t scope_id, unsigned* triggered) {
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        *triggered = 0;
        return SIZE_MAX;
    }
    rt_shard* pinned = rt_scope_owner_shard(ex, scope);
    rt_shard_lock(pinned);
    size_t active = scope->active_children;
    *triggered = scope->failfast_triggered;
    rt_shard_unlock(pinned);
    return active;
}

static int mode_debt261_failfast_join_verify(rt_executor* ex) {
    atomic_store_explicit(&g_debt261_owner_entered, 0, memory_order_release);
    atomic_store_explicit(&g_debt261_scope_handle, NULL, memory_order_release);
    atomic_store_explicit(&g_debt261_child, NULL, memory_order_release);
    unsigned before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY);
    // The owner creates the child inside its scope on its first poll (see
    // poll_debt261_scope_owner) and publishes it here; the child spins until
    // cancelled, so it cannot complete before the join reaches the verify
    // window, and by the time that window is reached the handle is set.
    rt_task* owner = spawn_pinned(ex, POLL_DEBT261_SCOPE_OWNER, 0);
    if (owner == NULL) {
        return fail("debt261 owner allocation failed");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY, before,
                               4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt261 owner never reached the join verify window");
    }
    rt_task* child = (rt_task*)atomic_load_explicit(&g_debt261_child, memory_order_acquire);
    if (child == NULL) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("debt261 owner reached the join with no child created in its scope");
    }
    // The owner is held between its first snapshot (one live child, fail-fast
    // not fired) and the verify. Everything below lands inside that gap.
    uint64_t scope_id =
        (uint64_t)(uintptr_t)atomic_load_explicit(&g_debt261_scope_handle, memory_order_acquire);
    unsigned triggered = 0;
    size_t active = debt261_scope_snapshot(ex, scope_id, &triggered);
    fprintf(stderr, "debt261 window: child=%llu active=%zu failfast_triggered=%u\n",
            (unsigned long long)child->id, active, triggered);
    if (active != 1 || triggered != 0) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("debt261 caught the join in the wrong state: want one live child, no fail-fast");
    }
    rt_task_cancel(child);
    if (!wait_task_status(child, TASK_DONE, 4000)) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("debt261 cancelled child never completed");
    }
    // DONE is stored before the scope takes the completion (mark_done ->
    // scope_on_child_done, which needs the control lane for a fail-fast
    // completion), so wait for the accounting itself, not for the status.
    for (uint32_t i = 0; i < 4000; i++) {
        active = debt261_scope_snapshot(ex, scope_id, &triggered);
        if (active == 0 && triggered != 0) {
            break;
        }
        sleep_us(1000);
    }
    fprintf(stderr, "debt261 landed: active=%zu failfast_triggered=%u\n", active, triggered);
    if (active != 0 || triggered == 0) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("debt261 child completion was not taken by the scope while the join was held");
    }
    rt_sync_point_open();
    if (!wait_task_status(owner, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt261 owner stranded after release");
    }
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(owner, &kind, &bits);
    fprintf(stderr, "debt261 after: owner kind=%u (1=Success 2=Cancelled)\n", (unsigned)kind);
    if (kind != 2) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt261 failfast scope answered Success after a cancelled child");
    }
    if (!await_expect(ex, child, 2, 0, "debt261 cancelled child")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
#endif
`
