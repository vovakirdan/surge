//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// RV2-DEBT-263. A task can run its body to a value and be cancelled after its
// last suspension point: rt_task_poll's TASK_DONE fast path answers from the
// TARGET and never consults the awaiter's own cancelled flag, so once every
// await in the body resolves from an already-DONE child there is no suspension
// left to observe the cancel at. The task then reached rt_async_return, which
// published POLL_DONE_SUCCESS unconditionally, and mark_done committed the kind
// it was handed -- so a cancelled child answered Success, its @failfast scope
// never fired, and the block resolved Success after a child had been cancelled.
// That is the live half of the exit 12 / exit 13 the lead measured on
// TestMTStructuredConcurrency after RV2-DEBT-261's join fix landed (7 of 20
// pinned runs).
//
// The drive holds the child at SP_ASYNC_RETURN_BEFORE_SUCCESS_COMMIT -- after
// its value is in its own result slot, before the scheduler is told the outcome
// -- cancels it there, and releases. The commit boundary in mark_done is the
// only thing left that can answer Cancelled, so the positive run asserts the
// child answers Cancelled AND that the failfast scope it belongs to resolves
// Cancelled; RV2_DEBT_263_NEGATIVE_CONTROL commits the kind as brought and MUST
// observe the Success the fix removes, at that same point.
func TestRuntimeV2LifecycleDebt263CancelCommitBoundaryProof(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt263(t, false)
	// SURGE_SHARDS=1 with two workers and up: the child is held inside its own
	// poll on one worker, so the scope owner needs ANOTHER worker on the SAME
	// shard -- one worker deadlocks in the window, and a child on another shard
	// would not be this scope's same-owner path.
	for _, threads := range []string{"2", "4", "8"} {
		t.Run("threads-"+threads, func(t *testing.T) {
			env := lifecycleEnv(
				"SURGE_SHARDS=1",
				"SURGE_THREADS="+threads,
				"SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_ASYNC_RETURN_BEFORE_SUCCESS_COMMIT:block")
			stdout, stderr, exitCode := runLifecycleHarness(
				t, binPath, "debt263-cancel-commit-boundary", env)
			if exitCode != 0 {
				t.Fatalf("DEBT-263 positive proof failed at SURGE_THREADS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					threads, exitCode, stdout, stderr)
			}
			// Non-vacuity: the cancel landed while the task was held INSIDE the
			// window with its value already published and its status still not
			// DONE, or the commit boundary had nothing to decide.
			if !strings.Contains(stderr, "debt263 cancelled in window: cancelled=1 done=0 done_waiters=0") {
				t.Fatalf("DEBT-263 positive proof did not land the cancel inside the window\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2LifecycleDebt263CancelCommitBoundaryNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt263(t, true)
	env := lifecycleEnv(
		"SURGE_SHARDS=1",
		"SURGE_THREADS=2",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_ASYNC_RETURN_BEFORE_SUCCESS_COMMIT:block")
	stdout, stderr, exitCode := runLifecycleHarness(
		t, binPath, "debt263-cancel-commit-boundary", env)
	if exitCode == 0 {
		t.Fatalf("DEBT-263 negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	// The strand must be AT the commit, on a window built the same way: the
	// cancel landed before the commit and the task still answered Success.
	if !strings.Contains(stderr, "debt263 cancelled in window: cancelled=1 done=0 done_waiters=0") {
		t.Fatalf("DEBT-263 negative control did not build the window (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	const want = "debt263 the task committed Success after a cancel that landed before the commit"
	if !strings.Contains(stderr, want) {
		t.Fatalf("DEBT-263 negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

func buildRuntimeV2LifecycleHarnessDebt263(t *testing.T, negativeControl bool) string {
	t.Helper()
	name := "lifecycle_harness_debt263"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if negativeControl {
		name += "_negative"
		flags = append(flags, "-DRV2_DEBT_263_NEGATIVE_CONTROL")
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

// lifecycleHarnessCancelCommitModes is concatenated into the shared lifecycle
// harness translation unit by buildRuntimeV2LifecycleHarnessWithFlags.
const lifecycleHarnessCancelCommitModes = `
#ifdef RT_TEST_SYNC_POINTS
#define POLL_DEBT263_CANCELLED_CHILD 4036
#define POLL_DEBT263_SCOPE_OWNER 4037

static _Atomic uint32_t g_debt263_child_release;
static _Atomic uint32_t g_debt263_owner_entered;
static _Atomic(void*) g_debt263_scope_handle;
static _Atomic(void*) g_debt263_child;

// The child whose completion this proof is about. It yields until the driver
// releases it, so the owner is provably registered before it ever reaches its
// return, and then returns a VALUE -- the window only exists on the path that
// publishes one.
//
// The driver spawns it (spawn_pinned -> ready_push under control, which signals
// the sleeping workers), never a poll held at a sync point: a task pushed from
// inside a held poll lands on that worker's own local deque, and a single-entry
// local push signals nobody (rt_ready_queue.c), so it would never run.
static void poll_debt263_cancelled_child(void) {
    if (atomic_load_explicit(&g_debt263_child_release, memory_order_acquire) == 0) {
        rt_async_yield(NULL, 0);
        return;
    }
    rt_async_return(NULL, &(uint64_t){7});
}

// The generated tail of a @failfast block (insertScopeJoins): join the set,
// exit the scope, then Cancelled when the join says fail-fast fired and Success
// otherwise. The owner CREATES the child inside its scope -- creation is the
// sole writer of membership, so a child spawned elsewhere and handed over
// would not be counted -- and spawn_pinned_in_scope forces the push onto the
// inject queue, so the child's schedule still does not depend on this poll,
// which is about to be held.
static void poll_debt263_scope_owner(void) {
    if (atomic_load_explicit(&g_debt263_owner_entered, memory_order_acquire) == 0) {
        void* handle = rt_scope_enter(true);
        rt_task* child = spawn_pinned_in_scope(ensure_exec(), POLL_DEBT263_CANCELLED_CHILD, 0);
        atomic_store_explicit(&g_debt263_child, child, memory_order_release);
        atomic_store_explicit(&g_debt263_scope_handle, handle, memory_order_release);
        atomic_store_explicit(&g_debt263_owner_entered, 1, memory_order_release);
    }
    void* handle = atomic_load_explicit(&g_debt263_scope_handle, memory_order_acquire);
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

// Both scope answers under the pinned lock. Deliberately not shared with the
// RV2-DEBT-261 stand's twin: the two stands must stay readable and repairable
// one at a time.
static size_t debt263_scope_snapshot(rt_executor* ex, uint64_t scope_id, unsigned* triggered) {
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

static int mode_debt263_cancel_commit_boundary(rt_executor* ex) {
    atomic_store_explicit(&g_debt263_child_release, 0, memory_order_release);
    atomic_store_explicit(&g_debt263_owner_entered, 0, memory_order_release);
    atomic_store_explicit(&g_debt263_scope_handle, NULL, memory_order_release);
    atomic_store_explicit(&g_debt263_child, NULL, memory_order_release);

    // The owner creates the child from inside its own scope (creation is the
    // sole writer of membership; a driver-spawned task handed over afterwards
    // is refused), and publishes it here for the driver to cancel and await.
    rt_task* owner = spawn_pinned(ex, POLL_DEBT263_SCOPE_OWNER, 0);
    if (owner == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt263 owner allocation failed");
    }
    if (!wait_u32_at_least(&g_debt263_owner_entered, 1, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt263 owner never entered its scope");
    }
    rt_task* child = (rt_task*)atomic_load_explicit(&g_debt263_child, memory_order_acquire);
    if (child == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt263 owner entered its scope but created no child");
    }
    uint64_t scope_id =
        (uint64_t)(uintptr_t)atomic_load_explicit(&g_debt263_scope_handle, memory_order_acquire);
    unsigned triggered = 0;
    size_t active = debt263_scope_snapshot(ex, scope_id, &triggered);
    fprintf(stderr, "debt263 registered: child=%llu active=%zu failfast_triggered=%u\n",
            (unsigned long long)child->id, active, triggered);
    if (active != 1 || triggered != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt263 caught the scope in the wrong state: want one live child, no fail-fast");
    }

    unsigned before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_ASYNC_RETURN_BEFORE_SUCCESS_COMMIT);
    atomic_store_explicit(&g_debt263_child_release, 1, memory_order_release);
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_ASYNC_RETURN_BEFORE_SUCCESS_COMMIT, before, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt263 child never reached the async-return commit window");
    }
    // The child is held with its value already in its own result slot and its
    // outcome not yet published. Everything below lands inside that gap.
    rt_task_cancel(child);
    unsigned cancelled = task_cancelled_load(child);
    unsigned done = task_status_load(child) == TASK_DONE ? 1u : 0u;
    // done_waiters is the input that would otherwise put this child's mark_done
    // on the control lane (mark_done_needs_control): the driver holds no
    // external await here, so the completion that follows runs control-free and
    // this row cannot be passing because a lock happened to serialise it.
    unsigned waiters = rt_done_waiters_load_before_done(ex);
    fprintf(stderr, "debt263 cancelled in window: cancelled=%u done=%u done_waiters=%u\n",
            cancelled, done, waiters);
    // Two permits, not one. The child takes the first. The second is for the
    // owner's OWN value-returning return, which the negative control reaches
    // (the fixed build answers Cancelled there and returns no value); an unspent
    // permit is inert, a missing one would hang the negative control at a window
    // it is not being measured at.
    rt_sync_point_open();
    rt_sync_point_open();
    if (cancelled != 1 || done != 0 || waiters != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt263 cancel did not land inside a control-free window");
    }

    if (!wait_task_status(child, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt263 child never completed after release");
    }
    uint8_t child_kind = 0;
    uint64_t child_bits = 0;
    rt_task_await(child, &child_kind, &child_bits);
    fprintf(stderr, "debt263 after: child kind=%u bits=%llu (1=Success 2=Cancelled)\n",
            (unsigned)child_kind, (unsigned long long)child_bits);
    if (child_kind != 2) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt263 the task committed Success after a cancel that landed before the commit");
    }
    if (!wait_task_status(owner, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt263 scope owner stranded after release");
    }
    uint8_t owner_kind = 0;
    uint64_t owner_bits = 0;
    rt_task_await(owner, &owner_kind, &owner_bits);
    fprintf(stderr, "debt263 after: owner kind=%u (1=Success 2=Cancelled)\n", (unsigned)owner_kind);
    if (owner_kind != 2) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt263 failfast scope answered Success after a cancelled child");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
#endif
`
