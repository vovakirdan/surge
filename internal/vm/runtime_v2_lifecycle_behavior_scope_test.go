//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"testing"
)

// TestRuntimeV2LifecycleScopeEnterRegisterJoinExit exercises the normal
// same-owner scope lifecycle directly at the C level (rt_scope_enter/
// register_child/join_all/exit, rt_async_scope.c:5-170): two children (one
// completing immediately, one yielding a few times first) registered under
// one scope, join_all parking the owner until both drain, then a clean exit.
// This gives file:line-precise coverage of the park/wake path itself ahead
// of Task 9's scope-owner-lane migration (S5-Q7/Q8/Q10), complementing the
// existing Surge-source-level scope coverage in
// runtime_v2_task_scope_blocking_waiter_contract_test.go.
func TestRuntimeV2LifecycleScopeEnterRegisterJoinExit(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarness(t)
	env := lifecycleEnv("SURGE_SHARDS=1", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "scope-basic", env)
	if exitCode != 0 {
		t.Fatalf("scope enter/register/join/exit failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

// TestRuntimeV2LifecycleScopeFailfastCancellation exercises the failfast
// branch directly at the C level: `rt_scope_register_child`'s late-
// registration path (`rt_async_scope.c:67-75`), which fires when a child is
// already TASK_DONE+TASK_RESULT_CANCELLED at registration time, sets
// `failfast_triggered`, cancels every already-registered sibling via
// `scope_cancel_children_locked`, and wakes the scope owner. The owner
// registers a slow sibling first (so it is in `scope->children[]`), cancels
// a second child directly and waits for it to drain, then registers that
// already-cancelled child -- which must cancel the slow sibling and set
// `failfast_triggered`, observed via `rt_scope_join_all`'s `failfast` out
// param (`rt_async_scope.c:107-108`). This complements, rather than
// duplicates, the existing Surge-source-level
// `TestRuntimeV2FailfastScopeCancellationWakesOwner`
// (`runtime_v2_task_scope_blocking_waiter_contract_test.go`, also selected
// by the Task 3 spike's corroboration table): that test proves the contract
// through compiled Surge/LLVM codegen; this one proves the same C entry
// points directly, with an explicit registration order the raw C harness
// controls. Guards S5-Q8/S5-Q9 (rule 3) ahead of Task 9.
func TestRuntimeV2LifecycleScopeFailfastCancellation(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarness(t)
	env := lifecycleEnv("SURGE_SHARDS=1", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "scope-failfast", env)
	if exitCode != 0 {
		t.Fatalf("scope failfast cancellation failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

// TestRuntimeV2LifecycleScopeCancelledPollTeardown exercises the
// apply_poll_outcome cancelled branch (rt_async_state.c:1585-1613): a task
// owning a scope with an active (never-naturally-completing) child is
// cancelled directly; the branch must cancel the child, re-park the owner on
// scope_key, and only call scope_exit_locked + mark_done(CANCELLED) once the
// child has actually drained. This is a genuine gap in existing coverage --
// the existing failfast test covers a *child's* cancellation triggering
// scope failfast, not the *owner* being cancelled while it owns a scope with
// pending children. Guards S5-Q14 ahead of Task 9.
func TestRuntimeV2LifecycleScopeCancelledPollTeardown(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarness(t)
	env := lifecycleEnv("SURGE_SHARDS=1", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "scope-cancelled-poll-teardown", env)
	if exitCode != 0 {
		t.Fatalf("cancelled-poll scope teardown failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

// The three ...AcrossShards tests below re-run the same scope scenarios under
// SURGE_SHARDS=1,2,8 (Epic 8 Task 9 scope-owner-lane migration). Under
// SURGE_SHARDS>1 the scope is pinned to its owner's shard and scope_key parks/
// wakes route to that shard's waiter store (not the control-lane store), so
// these prove the segmented scope table, pinned-shard scope_key routing, and
// owner-lane register/child-done/join_all/exit/failfast/cancel bookkeeping all
// hold when the runtime has multiple shards. Genuine cross-thread, cross-shard
// scope-child completion (post-F2 net-wrapper children on contending workers)
// is additionally covered by the 8-shard/1024 net benchmark that anchors the
// task's ctrl_scope/ctrl_completion measurement.
func TestRuntimeV2LifecycleScopeEnterRegisterJoinExitAcrossShards(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarness(t)
	runLifecycleModeAcrossShards(t, binPath, "scope-basic")
}

func TestRuntimeV2LifecycleScopeFailfastCancellationAcrossShards(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarness(t)
	runLifecycleModeAcrossShards(t, binPath, "scope-failfast")
}

func TestRuntimeV2LifecycleScopeCancelledPollTeardownAcrossShards(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarness(t)
	runLifecycleModeAcrossShards(t, binPath, "scope-cancelled-poll-teardown")
}

// TestRuntimeV2LifecycleScopeCrossOwnerChildDone is the deterministic
// cross-owner scope-completion regression test (RV2-DEBT-021). The
// cross-owner scope_on_child_done control fallback (rt_async_scope.c:340-377)
// is the sole producer of the residual ctrl_scope on the 8x1024 net benchmark
// (Epic 8 Task 9); before this test it was exercised only by that benchmark
// and the no-keepalive completion-pin TSan stress, neither deterministic in
// CI. Here a scope owner pinned to shard 0 registers a scope-child while it is
// same-owner, then parks in join_all; the scope-child adopts a shard-1
// CONNECTION placement via the real F2 machinery (rt_task_poll_adopt_placement
// consuming a connection-placed grandchild) and completes cross-owner, so
// scope_on_child_done takes the counted control fallback and wakes the owner
// cross-shard (the harness driver holds the owner parked until then, so the
// wake, not a self-consume, is what completes it). The mode driver asserts the
// scope-child actually adopted the cross-owner shard. Cross-owner needs
// SURGE_SHARDS>=2 (at SHARDS=1 the grandchild clamps to shard 0 and the
// completion is same-owner, so the branch is unreachable), so this sweeps only
// 2 and 8; the existing Scope*AcrossShards tests keep their children
// same-owner and never take this path (see their comment above).
func TestRuntimeV2LifecycleScopeCrossOwnerChildDone(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarness(t)
	// SURGE_THREADS must equal SURGE_SHARDS whenever SURGE_SHARDS>1 (runtime
	// startup requirement, matching runLifecycleModeAcrossShards).
	for _, shards := range []int{2, 8} {
		shards := shards
		t.Run(fmt.Sprintf("shards-%d", shards), func(t *testing.T) {
			env := lifecycleEnv(
				fmt.Sprintf("SURGE_SHARDS=%d", shards),
				fmt.Sprintf("SURGE_THREADS=%d", shards),
				"SURGE_BLOCKING_THREADS=1")
			stdout, stderr, exitCode := runLifecycleHarness(t, binPath, "scope-cross-owner", env)
			if exitCode != 0 {
				t.Fatalf("cross-owner scope completion failed at SURGE_SHARDS=%d (code=%d)\nstdout:\n%s\nstderr:\n%s",
					shards, exitCode, stdout, stderr)
			}
		})
	}
}

// lifecycleHarnessScopeAndShutdown holds the scope, timer, channel, blocking,
// and external-await poll functions (concatenated after
// lifecycleHarnessCommon in runtime_v2_lifecycle_behavior_harness_test.go).
const lifecycleHarnessScopeAndShutdown = `
static void poll_scope_child_quick(void) {
    rt_async_return(NULL, 1);
}

static _Atomic uint32_t g_scope_spin_steps;

static void poll_scope_child_spin(void) {
    uint32_t step = atomic_fetch_add_explicit(&g_scope_spin_steps, 1, memory_order_acq_rel);
    if (step < 3) {
        rt_async_yield(NULL);
        return;
    }
    rt_async_return(NULL, 1);
}

// Never completes on its own; only ends via cancellation, since
// rt_async_yield checks current_task_cancelled before yielding
// (rt_async_poll.c:288) and returns POLL_DONE_CANCELLED automatically.
// Reused by the scope-cancelled-poll-teardown owner's child and by the
// shutdown-parked join/scope scenarios (runtime_v2_lifecycle_behavior_
// await_shutdown_test.go) as a generic "runs until cancelled or shutdown"
// task.
static void poll_spin_forever(void) {
    rt_async_yield(NULL);
}

static _Atomic uint32_t g_scope_owner_phase;
static _Atomic(void*) g_scope_handle;
static _Atomic(void*) g_scope_child_a;
static _Atomic(void*) g_scope_child_b;

static void poll_scope_owner(void) {
    uint32_t phase = atomic_load_explicit(&g_scope_owner_phase, memory_order_acquire);
    if (phase == 0) {
        void* handle = rt_scope_enter(false);
        atomic_store_explicit(&g_scope_handle, handle, memory_order_release);
        void* child_a = __task_create(POLL_SCOPE_CHILD_QUICK, NULL);
        void* child_b = __task_create(POLL_SCOPE_CHILD_SPIN, NULL);
        atomic_store_explicit(&g_scope_child_a, child_a, memory_order_release);
        atomic_store_explicit(&g_scope_child_b, child_b, memory_order_release);
        rt_scope_register_child(handle, child_a);
        rt_scope_register_child(handle, child_b);
        atomic_store_explicit(&g_scope_owner_phase, 1, memory_order_release);
        rt_async_yield(NULL);
        return;
    }
    void* handle = atomic_load_explicit(&g_scope_handle, memory_order_acquire);
    uint64_t pending = 0;
    bool failfast = false;
    bool done = rt_scope_join_all(handle, &pending, &failfast);
    if (!done) {
        rt_async_yield(NULL);
        return;
    }
    rt_scope_exit(handle);
    rt_async_return(NULL, failfast ? 1 : 0);
}

static _Atomic uint32_t g_cancel_owner_phase;
static _Atomic(void*) g_cancel_scope_handle;
static _Atomic(void*) g_cancel_child;

static void poll_scope_cancel_owner(void) {
    uint32_t phase = atomic_load_explicit(&g_cancel_owner_phase, memory_order_acquire);
    if (phase == 0) {
        void* handle = rt_scope_enter(false);
        atomic_store_explicit(&g_cancel_scope_handle, handle, memory_order_release);
        void* child = __task_create(POLL_SPIN_FOREVER, NULL);
        atomic_store_explicit(&g_cancel_child, child, memory_order_release);
        rt_scope_register_child(handle, child);
        atomic_store_explicit(&g_cancel_owner_phase, 1, memory_order_release);
    }
    // Every subsequent invocation just yields; a cancellation delivered by
    // the harness driver surfaces here via rt_async_yield's own cancelled
    // check, producing POLL_DONE_CANCELLED and driving apply_poll_outcome's
    // cancelled-with-owned-scope branch (rt_async_state.c:1585-1613).
    rt_async_yield(NULL);
}

static _Atomic uint32_t g_failfast_owner_phase;
static _Atomic(void*) g_failfast_scope_handle;
static _Atomic(void*) g_failfast_victim;
static _Atomic(void*) g_failfast_sibling;

// Registers a slow sibling first (so it lands in scope->children[]), waits
// (by direct status peek -- no separate handle, no extra ref) for a
// harness-cancelled victim to reach TASK_DONE+TASK_RESULT_CANCELLED, then
// registers that victim: rt_scope_register_child's late-registration branch
// (rt_async_scope.c:67-75) must fire failfast_triggered, cancel the already-
// registered sibling, and wake the owner. Phase 2 confirms via
// rt_scope_join_all's failfast out param once the cancelled sibling drains.
static void poll_scope_owner_failfast(void) {
    uint32_t phase = atomic_load_explicit(&g_failfast_owner_phase, memory_order_acquire);
    if (phase == 0) {
        void* handle = rt_scope_enter(true);
        atomic_store_explicit(&g_failfast_scope_handle, handle, memory_order_release);
        void* sibling = __task_create(POLL_SPIN_FOREVER, NULL);
        void* victim = __task_create(POLL_SPIN_FOREVER, NULL);
        atomic_store_explicit(&g_failfast_sibling, sibling, memory_order_release);
        atomic_store_explicit(&g_failfast_victim, victim, memory_order_release);
        rt_scope_register_child(handle, sibling);
        atomic_store_explicit(&g_failfast_owner_phase, 1, memory_order_release);
        rt_async_yield(NULL);
        return;
    }
    if (phase == 1) {
        rt_task* victim = (rt_task*)atomic_load_explicit(&g_failfast_victim, memory_order_acquire);
        if (task_status_load(victim) != TASK_DONE) {
            rt_async_yield(NULL);
            return;
        }
        void* handle = atomic_load_explicit(&g_failfast_scope_handle, memory_order_acquire);
        rt_scope_register_child(handle, victim);
        atomic_store_explicit(&g_failfast_owner_phase, 2, memory_order_release);
        rt_async_yield(NULL);
        return;
    }
    void* handle = atomic_load_explicit(&g_failfast_scope_handle, memory_order_acquire);
    uint64_t pending = 0;
    bool failfast = false;
    bool done = rt_scope_join_all(handle, &pending, &failfast);
    if (!done) {
        rt_async_yield(NULL);
        return;
    }
    rt_scope_exit(handle);
    rt_async_return(NULL, failfast ? 1 : 0);
}

static _Atomic uint32_t g_scope_forever_phase;
static _Atomic(void*) g_scope_forever_handle;
static _Atomic(void*) g_scope_forever_child;

// Used only by the shutdown-with-parked-tasks scenario: enters a scope,
// registers one never-completing child, then parks in join_all forever (the
// child only ends via cancellation or process exit), so the owner is
// observably TASK_WAITING on scope_key at the moment shutdown is requested.
static void poll_scope_owner_forever(void) {
    uint32_t phase = atomic_load_explicit(&g_scope_forever_phase, memory_order_acquire);
    if (phase == 0) {
        void* handle = rt_scope_enter(false);
        atomic_store_explicit(&g_scope_forever_handle, handle, memory_order_release);
        void* child = __task_create(POLL_PARK_FOREVER, NULL);
        atomic_store_explicit(&g_scope_forever_child, child, memory_order_release);
        rt_scope_register_child(handle, child);
        atomic_store_explicit(&g_scope_forever_phase, 1, memory_order_release);
    }
    void* handle = atomic_load_explicit(&g_scope_forever_handle, memory_order_acquire);
    uint64_t pending = 0;
    bool failfast = false;
    (void)rt_scope_join_all(handle, &pending, &failfast);
    rt_async_yield(NULL);
}

// --- Timer/channel/blocking park (shutdown-with-parked-tasks scenario) ---

static _Atomic(void*) g_sleep_handle;

static void poll_timer_park(void) {
    void* handle = atomic_load_explicit(&g_sleep_handle, memory_order_acquire);
    if (handle == NULL) {
        // A long delay that will not fire before the harness requests
        // shutdown; this task must still be parked on timer_key at that
        // point (LIVENESS_PROBES.md "Timer, timeout, and shutdown liveness").
        handle = rt_sleep(60000);
        if (handle == NULL) {
            rt_async_return(NULL, 0);
            return;
        }
        atomic_store_explicit(&g_sleep_handle, handle, memory_order_release);
    }
    uint64_t bits = 0;
    uint8_t st = rt_task_poll(handle, &bits);
    if (st == 0) {
        rt_async_yield(NULL);
        return;
    }
    rt_async_return(NULL, 0);
}

static void poll_make_chan(void) {
    atomic_store_explicit(&g_chan_a, rt_channel_new(0), memory_order_release);
    rt_async_return(NULL, 0);
}

static void poll_channel_park(void) {
    void* ch = atomic_load_explicit(&g_chan_a, memory_order_acquire);
    uint64_t bits = 0;
    uint8_t st = rt_channel_recv(ch, &bits);
    if (st == 0) {
        rt_async_yield(NULL);
        return;
    }
    rt_async_return(NULL, st);
}

static void poll_blocking_park(void) {
    void* handle = atomic_load_explicit(&g_clone_shared_target, memory_order_acquire);
    if (handle == NULL) {
        // Long enough that shutdown is requested while this task is still
        // parked on blocking_key.
        handle = rt_blocking_submit(BLOCKING_FN_SLOW, NULL, 0, 0);
        if (handle == NULL) {
            rt_async_return(NULL, 0);
            return;
        }
        atomic_store_explicit(&g_clone_shared_target, handle, memory_order_release);
    }
    uint64_t bits = 0;
    uint8_t st = rt_task_poll(handle, &bits);
    if (st == 0) {
        rt_async_yield(NULL);
        return;
    }
    rt_async_return(NULL, 0);
}

// --- External-await target (worker-side join vs external rt_task_await) ---

static _Atomic uint32_t g_external_await_steps;

static void poll_external_await_target(void) {
    uint32_t step = atomic_fetch_add_explicit(&g_external_await_steps, 1, memory_order_acq_rel);
    if (step < 5) {
        rt_async_yield(NULL);
        return;
    }
    rt_async_return(NULL, 123);
}

uint64_t __surge_blocking_call(uint64_t id, void* state) {
    (void)state;
    if (id == BLOCKING_FN_SLOW) {
        sleep_us(30000);
        return 42;
    }
    return 0;
}

static int mode_scope_failfast(rt_executor* ex) {
    atomic_store_explicit(&g_failfast_owner_phase, 0, memory_order_relaxed);
    atomic_store_explicit(&g_failfast_scope_handle, NULL, memory_order_relaxed);
    atomic_store_explicit(&g_failfast_victim, NULL, memory_order_relaxed);
    atomic_store_explicit(&g_failfast_sibling, NULL, memory_order_relaxed);
    rt_task* owner = spawn_pinned(ex, POLL_SCOPE_OWNER_FAILFAST, 0);
    if (owner == NULL) {
        return fail("failfast owner allocation failed");
    }
    if (!wait_ptr(&g_failfast_victim, 4000) || !wait_ptr(&g_failfast_sibling, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("failfast owner did not spawn victim/sibling");
    }
    rt_task* victim = (rt_task*)atomic_load_explicit(&g_failfast_victim, memory_order_acquire);
    rt_task* sibling = (rt_task*)atomic_load_explicit(&g_failfast_sibling, memory_order_acquire);
    // Cancel while still READY: task_cancelled_store is unconditional
    // (rt_async_state.c:1466-1469), so this is safe before victim is ever
    // scheduled. Peeked via task_status_load only -- no rt_task_await/
    // task_release on victim's handle here, since the owner's poll function
    // (phase 1) also holds and dereferences this same raw pointer; releasing
    // it from this driver first would race the owner's own use of it.
    rt_task_cancel(victim);
    if (!wait_task_status(victim, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("failfast victim did not reach DONE after cancel");
    }
    if (!await_expect(ex, owner, 1, 1, "failfast owner (expected failfast flag set)")) {
        return 1;
    }
    if (!await_expect(ex, sibling, 2, 0, "failfast-cancelled sibling")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
`
