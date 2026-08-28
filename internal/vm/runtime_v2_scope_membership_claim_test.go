//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// A child's scope membership has two writers that share no lock: the
// registration (rt_scope_register_child, under the scope's pinned shard lock)
// and the child's own completion (scope_on_child_done), which has to read the
// membership before it knows which lock to take and therefore reads it holding
// none. Deciding that race with a test and a store on each side leaves an
// execution in which both miss: the registration reads the child's status
// before it is DONE and the completion reads the membership before it is
// published, so the completion skips the scope entirely -- no fail-fast raise,
// no retire -- and the registration then counts a child that has already
// finished. The scope's live-child count never reaches zero again and the
// `@failfast` block never resolves.
//
// The drive holds the registration at SP_SCOPE_MEMBERSHIP_DECIDED_BEFORE_PUBLISH
// -- membership decided, accounting not yet published -- and completes the only
// child, cancelled, inside that gap. The completion is proved to be IN the gap
// rather than assumed to be: it must publish DONE and cross
// SP_SCOPE_CHILD_DONE_AFTER_MEMBERSHIP_TAKE, the point at which it has read its
// own membership, before the registration is released. The owner's poll then
// mirrors the generated tail of a @failfast block: join, exit, Cancelled when
// the join says fail-fast fired.
//
// SURGE_SHARDS=1 with two workers and up: one worker sits inside the child's
// poll (holding no runtime lock, which is what lets its completion run while
// the registration holds the shard lock) and another runs the held owner.
func TestRuntimeV2LifecycleScopeMembershipClaimProof(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessScopeMembership(t, false)
	for _, threads := range []string{"2", "4", "8"} {
		t.Run("threads-"+threads, func(t *testing.T) {
			env := lifecycleEnv(
				"SURGE_SHARDS=1",
				"SURGE_THREADS="+threads,
				"SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_SCOPE_MEMBERSHIP_DECIDED_BEFORE_PUBLISH:block")
			stdout, stderr, exitCode := runLifecycleHarness(
				t, binPath, "scope-membership-claim", env)
			if exitCode != 0 {
				t.Fatalf("scope-membership proof failed at SURGE_THREADS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					threads, exitCode, stdout, stderr)
			}
			if !strings.Contains(stderr, scopeMembershipWindowLanded) {
				t.Fatalf("scope-membership proof did not land the completion inside the window at SURGE_THREADS=%s\nstdout:\n%s\nstderr:\n%s",
					threads, stdout, stderr)
			}
			if !strings.Contains(stderr, "scope-membership after: owner kind=2") {
				t.Fatalf("scope-membership proof did not show the scope resolving Cancelled at SURGE_THREADS=%s\nstdout:\n%s\nstderr:\n%s",
					threads, stdout, stderr)
			}
		})
	}
}

// The same drive with the claim taken out of both sides: the registration
// decides from the child's status and publishes the id with a plain store, the
// completion decides from a plain read. It must build the SAME window -- the
// completion lands in the gap and reads its membership there -- and then strand
// the scope, which is what the two-sided test-and-store used to do.
func TestRuntimeV2LifecycleScopeMembershipClaimNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessScopeMembership(t, true)
	env := lifecycleEnv(
		"SURGE_SHARDS=1",
		"SURGE_THREADS=2",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_SCOPE_MEMBERSHIP_DECIDED_BEFORE_PUBLISH:block")
	stdout, stderr, exitCode := runLifecycleHarness(
		t, binPath, "scope-membership-claim", env)
	if exitCode == 0 {
		t.Fatalf("scope-membership negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if !strings.Contains(stderr, scopeMembershipWindowLanded) {
		t.Fatalf("scope-membership negative control did not build the window (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	const want = "the failfast scope never resolved"
	if !strings.Contains(stderr, want) {
		t.Fatalf("scope-membership negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	// The strand is the accounting, and the accounting is printed: a child that
	// completed is still counted live and nothing raised fail-fast for it.
	if !strings.Contains(stderr, "scope-membership stranded: active=1 failfast_triggered=0") {
		t.Fatalf("scope-membership negative control stranded for a different reason than an uncounted completion (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

// The second candidate for the same symptom, and the row that answers it: a
// spawn publishes and ready-pushes its child before the caller reaches
// rt_scope_register_child, so a child can complete before it is a member. This
// row forces that ordering instead of racing for it -- the child is cancelled
// and DONE before the scope exists -- and requires the block to resolve
// Cancelled anyway. No sync point is armed: there is no window to hold open,
// only an order to impose. It is also the only row that exercises the side of
// the claim that LOSES, which is where a registration that arrives after its
// child has to answer for it.
func TestRuntimeV2LifecycleScopeMembershipCompletedBeforeRegistration(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessScopeMembership(t, false)
	for _, threads := range []string{"2", "4"} {
		t.Run("threads-"+threads, func(t *testing.T) {
			env := lifecycleEnv(
				"SURGE_SHARDS=1", "SURGE_THREADS="+threads, "SURGE_BLOCKING_THREADS=1")
			stdout, stderr, exitCode := runLifecycleHarness(
				t, binPath, "scope-membership-completed-before-registration", env)
			if exitCode != 0 {
				t.Fatalf("scope-membership-late row failed at SURGE_THREADS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					threads, exitCode, stdout, stderr)
			}
			if !strings.Contains(stderr, "scope-membership-late after: owner kind=2") {
				t.Fatalf("scope-membership-late row did not show the scope resolving Cancelled at SURGE_THREADS=%s\nstdout:\n%s\nstderr:\n%s",
					threads, stdout, stderr)
			}
		})
	}
}

// The sentence the stand prints once the completion is proved to be inside the
// held registration's window. Both rows check for it, because a run that never
// built the window proves nothing either way.
const scopeMembershipWindowLanded = "scope-membership window: the child completed and read " +
	"its membership while its registration was held"

func buildRuntimeV2LifecycleHarnessScopeMembership(t *testing.T, negativeControl bool) string {
	t.Helper()
	name := "lifecycle_harness_scope_membership"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if negativeControl {
		name += "_negative"
		flags = append(flags, "-DRV2_SCOPE_MEMBERSHIP_CLAIM_NEGATIVE_CONTROL")
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

// lifecycleHarnessScopeMembershipModes is concatenated into the shared lifecycle
// harness translation unit by buildRuntimeV2LifecycleHarnessWithFlags.
const lifecycleHarnessScopeMembershipModes = `
#ifdef RT_TEST_SYNC_POINTS
#define POLL_SCOPE_MEMBERSHIP_OWNER 4042
#define POLL_SCOPE_MEMBERSHIP_CHILD 4043

static _Atomic uint32_t g_membership_owner_entered;
static _Atomic(void*) g_membership_scope_handle;
static _Atomic(void*) g_membership_child;
static _Atomic uint32_t g_membership_child_running;
static _Atomic uint32_t g_membership_child_release;

// The child. It sits inside its own poll holding no runtime lock until the
// driver releases it, then completes CANCELLED from there.
//
// Completing from inside the poll rather than through rt_task_cancel is the
// whole reason this stand can build its window: a cancel would have to queue
// this task and the pop would take the shard lock the held registration is
// holding, so the completion could not run until the window was already over.
static void poll_scope_membership_child(void) {
    atomic_store_explicit(&g_membership_child_running, 1, memory_order_release);
    while (atomic_load_explicit(&g_membership_child_release, memory_order_acquire) == 0) {
        sleep_us(1000);
    }
    rt_async_return_cancelled(NULL, 0);
}

// The owner, shaped like the generated tail of a @failfast block: enter the
// scope, register the one child, join the set, exit, and answer Cancelled when
// the join says fail-fast fired. The handle is published BEFORE the
// registration, because the registration is where this poll is held and the
// driver needs to be able to name the scope while it is held there.
//
// The child is spawned by the DRIVER, not created here: a worker's own spawn
// lands on its local tail and a single local entry signals nobody
// (ready_push_task_locked, rt_ready_queue.c), so a child pushed from this poll
// would never be popped and could never complete inside any window.
static void poll_scope_membership_owner(void) {
    if (atomic_load_explicit(&g_membership_owner_entered, memory_order_acquire) == 0) {
        void* handle = rt_scope_enter(true);
        atomic_store_explicit(&g_membership_scope_handle, handle, memory_order_release);
        atomic_store_explicit(&g_membership_owner_entered, 1, memory_order_release);
        rt_scope_register_child(handle,
                                atomic_load_explicit(&g_membership_child, memory_order_acquire));
    }
    void* handle = atomic_load_explicit(&g_membership_scope_handle, memory_order_acquire);
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

// Both answers under the scope's own serializer, for the diagnosis a stranded
// run prints. A scope that is already gone reads as SIZE_MAX so the driver can
// tell that case apart from a scope still holding a child.
static size_t membership_scope_snapshot(rt_executor* ex, uint64_t scope_id, unsigned* triggered) {
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

// The other side of the same race, and it needs no window at all: a spawn
// publishes and ready-pushes its child before the caller reaches the
// registration, so a child can complete BEFORE anything claims it. Here that is
// forced rather than raced -- the child is completed, cancelled, and observed
// DONE before the owner is even spawned. The scope has no member to retire and
// nothing to wake it, so the only thing that can answer for that child is the
// registration itself, on the path it takes when its claim loses.
static int mode_scope_membership_completed_before_registration(rt_executor* ex) {
    atomic_store_explicit(&g_membership_owner_entered, 0, memory_order_release);
    atomic_store_explicit(&g_membership_scope_handle, NULL, memory_order_release);
    atomic_store_explicit(&g_membership_child, NULL, memory_order_release);
    atomic_store_explicit(&g_membership_child_running, 0, memory_order_release);
    atomic_store_explicit(&g_membership_child_release, 1, memory_order_release);

    rt_task* child = spawn_child_for_stand(ex, POLL_SCOPE_MEMBERSHIP_CHILD, 0);
    if (child == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-membership-late stand: child allocation failed");
    }
    atomic_store_explicit(&g_membership_child, child, memory_order_release);
    if (!wait_task_status(child, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-membership-late stand: the child never completed");
    }
    fputs("scope-membership-late window: the child was cancelled and DONE before the scope "
          "existed\n",
          stderr);
    rt_task* owner = spawn_child_for_stand(ex, POLL_SCOPE_MEMBERSHIP_OWNER, 0);
    if (owner == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-membership-late stand: owner allocation failed");
    }
    if (!wait_task_status(owner, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-membership-late stand: the failfast scope never resolved");
    }
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(owner, &kind, &bits);
    fprintf(stderr, "scope-membership-late after: owner kind=%u (1=Success 2=Cancelled)\n",
            (unsigned)kind);
    if (kind != 2) {
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-membership-late stand: the failfast scope answered Success after "
                    "registering a child that had already been cancelled");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_scope_membership_claim(rt_executor* ex) {
    atomic_store_explicit(&g_membership_owner_entered, 0, memory_order_release);
    atomic_store_explicit(&g_membership_scope_handle, NULL, memory_order_release);
    atomic_store_explicit(&g_membership_child, NULL, memory_order_release);
    atomic_store_explicit(&g_membership_child_running, 0, memory_order_release);
    atomic_store_explicit(&g_membership_child_release, 0, memory_order_release);
    unsigned decided_before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_SCOPE_MEMBERSHIP_DECIDED_BEFORE_PUBLISH);

    rt_task* child = spawn_child_for_stand(ex, POLL_SCOPE_MEMBERSHIP_CHILD, 0);
    if (child == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-membership stand: child allocation failed");
    }
    atomic_store_explicit(&g_membership_child, child, memory_order_release);
    // The child must be INSIDE its poll before the owner is held: that is what
    // keeps its completion off the shard lock the held registration holds.
    if (!wait_u32_at_least(&g_membership_child_running, 1, 4000)) {
        atomic_store_explicit(&g_membership_child_release, 1, memory_order_release);
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-membership stand: the child never entered its poll");
    }
    rt_task* owner = spawn_child_for_stand(ex, POLL_SCOPE_MEMBERSHIP_OWNER, 0);
    if (owner == NULL) {
        atomic_store_explicit(&g_membership_child_release, 1, memory_order_release);
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-membership stand: owner allocation failed");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_SCOPE_MEMBERSHIP_DECIDED_BEFORE_PUBLISH,
                               decided_before, 4000)) {
        atomic_store_explicit(&g_membership_child_release, 1, memory_order_release);
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-membership stand: the registration never reached the membership window");
    }

    // The registration is held with this child's membership decided and the
    // scope's accounting not published yet. Everything below lands in that gap.
    unsigned take_before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_SCOPE_CHILD_DONE_AFTER_MEMBERSHIP_TAKE);
    atomic_store_explicit(&g_membership_child_release, 1, memory_order_release);
    if (!wait_task_status(child, TASK_DONE, 4000)) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-membership stand: the child never completed inside the window");
    }
    // DONE is published before the completion reaches the scope at all, so it
    // is not evidence that the completion is in the window. The membership read
    // is: wait for the completion to cross it, rather than sleeping and hoping.
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_SCOPE_CHILD_DONE_AFTER_MEMBERSHIP_TAKE,
                               take_before, 4000)) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail(
            "scope-membership stand: the completion never read its membership inside the window");
    }
    fputs("scope-membership window: the child completed and read its membership while its "
          "registration was held\n",
          stderr);
    rt_sync_point_open();

    uint64_t scope_id =
        (uint64_t)(uintptr_t)atomic_load_explicit(&g_membership_scope_handle, memory_order_acquire);
    if (!wait_task_status(owner, TASK_DONE, 4000)) {
        unsigned triggered = 0;
        size_t active = membership_scope_snapshot(ex, scope_id, &triggered);
        fprintf(stderr, "scope-membership stranded: active=%zu failfast_triggered=%u\n", active,
                triggered);
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-membership stand: the failfast scope never resolved -- it is holding a "
                    "child that completed while its registration was in flight");
    }
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(owner, &kind, &bits);
    fprintf(stderr, "scope-membership after: owner kind=%u (1=Success 2=Cancelled)\n",
            (unsigned)kind);
    if (kind != 2) {
        (void)rt_executor_request_shutdown(ex);
        return fail(
            "scope-membership stand: the failfast scope answered Success after a cancelled child");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
#endif
`
