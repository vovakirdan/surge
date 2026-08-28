//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// Claiming a task for a poll is three stores -- enqueued cleared, status
// RUNNING, wake token consumed -- and every gate in the wake path is a test of
// exactly the first two:
//
//	if (status == TASK_DONE || status == TASK_RUNNING ||
//	    task_enqueued_load(task) != 0) return 0;   (rt_task_park.c)
//
// So a task removed from a queue is unreachable to a waker only once it has
// been claimed. The worker turn does both inside its pop's own critical
// section (rt_worker_turn.c). The inline claim in rt_task_poll used to do them
// apart: the take released the shard lock and the claim's stores followed
// outside it, leaving an instant when the task sat in NO queue and still read
// READY with enqueued already cleared -- the one pair of values that passes the
// gate. A wake landing there pushes, and a second worker takes a task the first
// is about to poll.
//
// The drive holds the owner at SP_INLINE_CHILD_TAKEN_OFF_QUEUE, immediately
// after the take released the lock and before the poll begins, and cancels the
// child there -- a cancel wakes unconditionally (rt_task_complete.c), so it is
// the smallest real racing action that reaches the gate. What the child reads
// at that instant is the whole question: the claim taken with the take says
// RUNNING, the claim left for afterwards still says READY.
//
// The negative control restores the split, and MUST be seen doing what the
// split does: the wake queues the task, another worker picks it up, and the
// two pollers collide inside the same task.
func TestRuntimeV2LifecycleInlineClaimIsOneObservation(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessInlineClaim(t, false)
	// SURGE_SHARDS=1 so the child the owner creates from inside its own poll
	// lands on a queue this same worker owns -- the only shape the inline claim
	// accepts. Two workers and up so there IS a peer for a duplicate entry to
	// be handed to; the stand quiesces first, and a single local push signals
	// nobody (rt_ready_queue.c), so that peer stays parked unless a wake wakes
	// it.
	for _, threads := range []string{"2", "4"} {
		t.Run("threads-"+threads, func(t *testing.T) {
			env := lifecycleEnv(
				"SURGE_SHARDS=1",
				"SURGE_THREADS="+threads,
				"SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_INLINE_CHILD_TAKEN_OFF_QUEUE:block")
			stdout, stderr, exitCode := runLifecycleHarness(
				t, binPath, "inline-claim-off-queue", env)
			if exitCode != 0 {
				t.Fatalf("inline-claim proof failed at SURGE_THREADS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					threads, exitCode, stdout, stderr)
			}
			// Non-vacuity, and the discrimination itself: the window was
			// reached with the child already claimed (TASK_RUNNING = 1). A run
			// that never got there proves nothing about the claim.
			if !strings.Contains(stderr, "inline-claim window: ") ||
				!strings.Contains(stderr, "status=1 enqueued=0") {
				t.Fatalf("inline-claim proof did not observe a claimed child at the window\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
			if !strings.Contains(stderr, "inline-claim after wake: enqueued=0") {
				t.Fatalf("inline-claim proof did not show the wake being refused\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2LifecycleInlineClaimSplitNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessInlineClaim(t, true)
	env := lifecycleEnv(
		"SURGE_SHARDS=1",
		"SURGE_THREADS=2",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_INLINE_CHILD_TAKEN_OFF_QUEUE:block")
	stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "inline-claim-off-queue", env)
	if exitCode == 0 {
		t.Fatalf("inline-claim negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	// The window must be built the same way, and reached with the claim NOT
	// taken: the child is off every queue and still reads READY (status=0).
	// That is what makes the wake below reach the gate at all.
	if !strings.Contains(stderr, "inline-claim window: ") ||
		!strings.Contains(stderr, "status=0 enqueued=0") {
		t.Fatalf("inline-claim negative control did not build the window (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	// And it must fail for the right reason: the wake queued the task, and the
	// worker that took the duplicate collided with the owner inside it. Either
	// half is the defect; the runtime's own double-poll panic is the louder.
	queued := strings.Contains(stderr, "inline-claim after wake: enqueued=1")
	collided := strings.Contains(stderr, "async: double poll")
	if !queued && !collided {
		t.Fatalf("inline-claim negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

func buildRuntimeV2LifecycleHarnessInlineClaim(t *testing.T, negativeControl bool) string {
	t.Helper()
	name := "lifecycle_harness_inline_claim"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if negativeControl {
		name += "_negative"
		flags = append(flags, "-DRV2_INLINE_CLAIM_SPLIT_NEGATIVE_CONTROL")
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

// lifecycleHarnessInlineClaimModes is concatenated into the shared lifecycle
// harness translation unit by buildRuntimeV2LifecycleHarnessWithFlags, after
// lifecycleHarnessStandHelpers (whose spawn_child_for_stand this uses) and
// before lifecycleHarnessMain.
const lifecycleHarnessInlineClaimModes = `
#ifdef RT_TEST_SYNC_POINTS
#define POLL_INLINE_CLAIM_OWNER 4040
#define POLL_INLINE_CLAIM_CHILD 4041

static _Atomic uint32_t g_inline_claim_owner_entered;
static _Atomic(void*) g_inline_claim_scope;
static _Atomic(void*) g_inline_claim_child;
static _Atomic uint32_t g_inline_claim_child_polls;
static _Atomic uint32_t g_inline_claim_child_release;

// The child of an inline claim. It ends only by cancellation, so the scope's
// answer depends on this task's completion and nothing else.
//
// It HOLDS at the top of its poll while the driver says so. That hold is what
// makes a collision provable rather than likely: if a second poller is ever
// handed this task, it is still inside the poll when the first one arrives, and
// task_polling_enter names both sides (rt_async_state.c). The hold is bounded,
// so a stand can never hang on it.
static void poll_inline_claim_child(void) {
    atomic_fetch_add_explicit(&g_inline_claim_child_polls, 1, memory_order_acq_rel);
    for (uint32_t i = 0; i < 4000; i++) {
        if (atomic_load_explicit(&g_inline_claim_child_release, memory_order_acquire) != 0) {
            break;
        }
        sleep_us(1000);
    }
    rt_async_yield(NULL, 0);
}

// The owner creates its child with __task_create from inside its own poll ON
// PURPOSE. That push lands on THIS worker's local deque tail
// (ready_push_task_locked, rt_ready_queue.c), which is the one shape
// rt_task_poll's inline-claim branch accepts -- so unlike every other stand
// here, the child must NOT come from the driver: from the driver it would go to
// the inject queue and no claim would ever happen. The owner is not held while
// the child needs another worker, so the stand-helper's trap does not apply --
// this owner polls the child itself, which is the whole point.
//
// The tail mirrors the generated tail of a @failfast block (insertScopeJoins):
// join the set, exit the scope, Cancelled when fail-fast fired and Success
// otherwise. So the stand also reports whether the fail-fast answer survives
// the window, not just whether the queue does.
static void poll_inline_claim_owner(void) {
    if (atomic_load_explicit(&g_inline_claim_owner_entered, memory_order_acquire) == 0) {
        void* handle = rt_scope_enter(true);
        void* child = __task_create(POLL_INLINE_CLAIM_CHILD, NULL, rt_channel_opaque_word_ops());
        rt_scope_register_child(handle, child);
        atomic_store_explicit(&g_inline_claim_scope, handle, memory_order_release);
        atomic_store_explicit(&g_inline_claim_child, child, memory_order_release);
        atomic_store_explicit(&g_inline_claim_owner_entered, 1, memory_order_release);
        uint64_t out = 0;
        if (rt_task_poll(child, &out) == 0) {
            rt_async_yield(NULL, 0);
            return;
        }
    }
    void* handle = atomic_load_explicit(&g_inline_claim_scope, memory_order_acquire);
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

static int mode_inline_claim_off_queue(rt_executor* ex) {
    atomic_store_explicit(&g_inline_claim_owner_entered, 0, memory_order_release);
    atomic_store_explicit(&g_inline_claim_scope, NULL, memory_order_release);
    atomic_store_explicit(&g_inline_claim_child, NULL, memory_order_release);
    atomic_store_explicit(&g_inline_claim_child_polls, 0, memory_order_release);
    atomic_store_explicit(&g_inline_claim_child_release, 0, memory_order_release);
    unsigned before = rt_sync_point_reached_count(RT_SYNC_POINT_SP_INLINE_CHILD_TAKEN_OFF_QUEUE);

    // Quiesce. The owner's child is pushed onto that worker's own local deque,
    // and a single local entry signals nobody (rt_ready_queue.c) -- but an
    // ALREADY AWAKE peer steals from a local deque without being signalled
    // (worker_next_ready). One task driven to completion through the driver
    // path, then a pause, leaves every other worker parked on worker_cv, so the
    // only thread that can reach the child is the one about to claim it.
    rt_task* warm = spawn_child_for_stand(ex, POLL_JOIN_TARGET_QUICK, 0);
    if (warm == NULL || !wait_task_status(warm, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("inline-claim stand: warm-up child never completed");
    }
    sleep_us(100000);

    rt_task* owner = spawn_child_for_stand(ex, POLL_INLINE_CLAIM_OWNER, 0);
    if (owner == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("inline-claim stand: owner allocation failed");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_INLINE_CHILD_TAKEN_OFF_QUEUE, before, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("inline-claim stand: the owner never reached the claim window");
    }
    rt_task* child = (rt_task*)atomic_load_explicit(&g_inline_claim_child, memory_order_acquire);
    if (child == NULL) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("inline-claim stand: the owner reached the window without publishing a child");
    }

    // The window: the take has removed the child from every queue and released
    // the shard lock. The pair below is the answer -- RUNNING means the claim
    // came with the take, READY means it is still to come and the wake path's
    // gate is wide open.
    fprintf(stderr, "inline-claim window: child=%llu status=%u enqueued=%u\n",
            (unsigned long long)child->id, (unsigned)task_status_load(child),
            (unsigned)task_enqueued_load(child));

    rt_task_cancel(child);
    unsigned enq_after_wake = (unsigned)task_enqueued_load(child);
    fprintf(stderr, "inline-claim after wake: enqueued=%u\n", enq_after_wake);
    // A queue entry is only half of it: the entry has to be TAKEN for the
    // damage to land, so wait for a second poller to actually enter the child.
    int second_poller = wait_u32_at_least(&g_inline_claim_child_polls, 1, 500);
    fprintf(stderr, "inline-claim second poller: %d\n", second_poller);

    if (enq_after_wake == 0 && !second_poller) {
        // Nothing to collide with; let the owner's own poll run the child.
        atomic_store_explicit(&g_inline_claim_child_release, 1, memory_order_release);
        rt_sync_point_open();
        if (!wait_task_status(owner, TASK_DONE, 8000)) {
            (void)rt_executor_request_shutdown(ex);
            return fail("inline-claim stand: owner stranded after release");
        }
        uint8_t kind = 0;
        uint64_t bits = 0;
        rt_task_await(owner, &kind, &bits);
        fprintf(stderr, "inline-claim owner kind=%u (1=Success 2=Cancelled)\n", (unsigned)kind);
        (void)rt_executor_request_shutdown(ex);
        if (kind != 2) {
            return fail("inline-claim stand: failfast scope answered Success after a cancelled child");
        }
        return 0;
    }

    // The wake got in. Release the OWNER first and leave the second poller
    // inside the child, so the collision the split makes possible is the one
    // that gets observed rather than a timing accident.
    rt_sync_point_open();
    sleep_us(200000);
    atomic_store_explicit(&g_inline_claim_child_release, 1, memory_order_release);
    (void)rt_executor_request_shutdown(ex);
    return fail("inline-claim stand: a wake at the window queued this task for a second worker -- "
                "the take and the claim are not one observation");
}
#endif
`
