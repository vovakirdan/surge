//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// RV2-DEBT-263, the RESIDUAL window. The first stand
// (debt263-cancel-commit-boundary) proves a cancel that arrives BEFORE the
// commit is answered Cancelled. This one proves the other side, at the window
// that is left once the gate is sealed: from the seal to the TASK_DONE store,
// mark_done still has everything else to do -- the waiter removal, the
// abandoned-state release, three owned releases and a mutex acquisition in
// release_matching_leases (rt_remote_task_lease.c). Hundreds of nanoseconds, and
// the shape of fix that pairs ordered plain accesses instead of one
// read-modify-write leaves exactly this gap open: a cancel landing in it stores
// its flag, sees a not-yet-DONE task, and believes it landed, while the
// completion has already chosen Success. Two answers for one task.
//
// With the gate, the cancel's compare-and-swap finds SEALED and fails, so the
// stand requires BOTH halves of one answer: the task commits Success AND its
// cancel is visibly refused (task_cancelled_load reads 0, not "a cancel is
// outstanding"). Requiring only the first would pass on a runtime that
// swallowed the cancel silently.
//
// The drive is its own proof that the window is control-free. The driver
// cancels while a worker is held inside mark_done, and rt_task_cancel takes the
// control lock: if mark_done held control across this window, the cancel would
// block on it while the completion waits for the driver's release, and the
// stand would strand at the sync point's bounded guard rather than pass. It
// also asserts done_waiters == 0 at the window, which is the input that would
// otherwise force mark_done onto the control lane
// (mark_done_needs_control) -- so nothing here is being serialised for it.
func TestRuntimeV2LifecycleDebt263CancelAfterSealIsRefusedProof(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt263(t, false)
	for _, threads := range []string{"2", "4", "8"} {
		t.Run("threads-"+threads, func(t *testing.T) {
			env := lifecycleEnv(
				"SURGE_SHARDS=1",
				"SURGE_THREADS="+threads,
				"SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_MARKDONE_AFTER_SEAL_BEFORE_DONE:block")
			stdout, stderr, exitCode := runLifecycleHarness(
				t, binPath, "debt263-cancel-after-seal", env)
			if exitCode != 0 {
				t.Fatalf("DEBT-263 seal-refusal proof failed at SURGE_THREADS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					threads, exitCode, stdout, stderr)
			}
			// Non-vacuity: the cancel really did land inside the residual
			// window -- after the seal, before TASK_DONE -- on a completion no
			// external awaiter was holding the control lane for.
			if !strings.Contains(stderr, "debt263 seal window: done=0 done_waiters=0") {
				t.Fatalf("DEBT-263 seal-refusal proof did not build the residual window\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2LifecycleDebt263CancelAfterSealNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt263(t, true)
	env := lifecycleEnv(
		"SURGE_SHARDS=1",
		"SURGE_THREADS=2",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_MARKDONE_AFTER_SEAL_BEFORE_DONE:block")
	stdout, stderr, exitCode := runLifecycleHarness(
		t, binPath, "debt263-cancel-after-seal", env)
	if exitCode == 0 {
		t.Fatalf("DEBT-263 seal-refusal negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if !strings.Contains(stderr, "debt263 seal window: done=0 done_waiters=0") {
		t.Fatalf("DEBT-263 seal-refusal negative control did not build the window (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	const want = "debt263 the task committed Success while its canceller believed the cancel landed"
	if !strings.Contains(stderr, want) {
		t.Fatalf("DEBT-263 seal-refusal negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

// lifecycleHarnessCancelSealModes is concatenated into the shared lifecycle
// harness translation unit by buildRuntimeV2LifecycleHarnessWithFlags.
const lifecycleHarnessCancelSealModes = `
#ifdef RT_TEST_SYNC_POINTS
#define POLL_DEBT263_SEALED_TASK 4038

static _Atomic uint32_t g_debt263_seal_release;

// Returns a value, so its completion carries a result and reaches the window
// with something to answer for. It yields until the driver releases it, which
// is what puts the whole of mark_done inside the driver's control rather than
// somewhere in the harness's startup.
static void poll_debt263_sealed_task(void) {
    if (atomic_load_explicit(&g_debt263_seal_release, memory_order_acquire) == 0) {
        rt_async_yield(NULL, 0);
        return;
    }
    rt_async_return(NULL, &(uint64_t){9});
}

static int mode_debt263_cancel_after_seal(rt_executor* ex) {
    atomic_store_explicit(&g_debt263_seal_release, 0, memory_order_release);
    rt_task* task = spawn_pinned(ex, POLL_DEBT263_SEALED_TASK, 0);
    if (task == NULL) {
        return fail("debt263 seal task allocation failed");
    }
    unsigned before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_MARKDONE_AFTER_SEAL_BEFORE_DONE);
    atomic_store_explicit(&g_debt263_seal_release, 1, memory_order_release);
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_MARKDONE_AFTER_SEAL_BEFORE_DONE, before, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt263 completion never reached the post-seal window");
    }
    // The completing worker is held between the seal and the TASK_DONE store.
    // done_waiters is the reason mark_done would otherwise be on the control
    // lane, and it is zero here: nothing outside the runtime is awaiting.
    unsigned done_before = task_status_load(task) == TASK_DONE ? 1u : 0u;
    unsigned waiters = rt_done_waiters_load_before_done(ex);
    fprintf(stderr, "debt263 seal window: done=%u done_waiters=%u\n", done_before, waiters);
    if (done_before != 0 || waiters != 0) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("debt263 caught the completion in the wrong state: want not-DONE, no external awaiter");
    }
    // Takes the control lock. If mark_done held control across this window the
    // call would block on it while the completion waits for the release below,
    // and this stand would strand instead of passing -- which is what makes it
    // a proof about the CONTROL-FREE window and not about the control lane.
    rt_task_cancel(task);
    unsigned believed = task_cancelled_load(task);
    fprintf(stderr, "debt263 after seal: cancel believed=%u\n", believed);
    rt_sync_point_open();
    if (!wait_task_status(task, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt263 sealed task never completed after release");
    }
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(task, &kind, &bits);
    fprintf(stderr, "debt263 after seal: kind=%u bits=%llu (1=Success 2=Cancelled)\n",
            (unsigned)kind, (unsigned long long)bits);
    // Both halves of ONE answer. Success with a believed cancel is the split
    // brain this window used to allow.
    if (kind == 1 && believed != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt263 the task committed Success while its canceller believed the cancel landed");
    }
    if (kind != 1 || bits != 9) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt263 a cancel that lost the seal still changed the answer");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
#endif
`
