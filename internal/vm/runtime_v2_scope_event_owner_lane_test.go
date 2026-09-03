//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"strings"
	"testing"
)

// RV2-DEBT-280 / 266 / 283, owner ruling 2026-09-02 (Р6): a scope's count,
// fail-fast flag and child list have ONE serializer, the pinned owner shard
// lock, and ONE lane that writes under it. A child that completes on another
// shard used to reach the scope from its own lane through the process-wide
// control lane (scope_on_child_done's control fallback); now it publishes a
// SCOPE_CHILD_DONE event into the owner shard's inbound control lane, and the
// owner lane applies it in the same single critical section a same-shard
// completion uses.
//
// The drive builds the cross-owner shape the ruling is about: an owner on
// shard 0 enters a fail-fast scope and creates its child there; the child
// adopts shard 1 through the real F2 machinery (it consumes a
// connection-placed grandchild) and then yields until cancelled; the owner is
// held at SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY with that one live child
// counted. The driver cancels the child on shard 1 and, while the owner is
// still held, reads shard 0's transport counter and the scope's two answers
// under the pinned lock: the completion must have PUBLISHED one event and
// touched nothing. Released, the owner's verify still sees the child, parks,
// and its own lane applies the event -- count retired and flag raised under
// one lock -- so the join answers Cancelled. The negative control restores
// the pre-ruling shape (the child's lane writes the scope itself under
// control) and MUST be caught by the same read: no event, accounting already
// changed under the held owner.
func TestRuntimeV2LifecycleDebt280ScopeEventOwnerLaneProof(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt280(t, false)
	// Cross-owner needs SURGE_SHARDS>=2 (at one shard the grandchild clamps to
	// shard 0 and the completion is same-owner); SURGE_THREADS must equal
	// SURGE_SHARDS whenever SURGE_SHARDS>1.
	for _, shards := range []int{2, 8} {
		shards := shards
		t.Run(fmt.Sprintf("shards-%d", shards), func(t *testing.T) {
			env := lifecycleEnv(
				fmt.Sprintf("SURGE_SHARDS=%d", shards),
				fmt.Sprintf("SURGE_THREADS=%d", shards),
				"SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY:block")
			stdout, stderr, exitCode := runLifecycleHarness(
				t, binPath, "debt280-scope-event-owner-lane", env)
			if exitCode != 0 {
				t.Fatalf("DEBT-280 proof failed at SURGE_SHARDS=%d (code=%d)\nstdout:\n%s\nstderr:\n%s",
					shards, exitCode, stdout, stderr)
			}
			for _, want := range []string{
				"debt280 window: events=1 active=1 failfast_triggered=0",
				"debt280 after: owner kind=2",
			} {
				if !strings.Contains(stderr, want) {
					t.Fatalf("DEBT-280 proof did not build its window (%q missing)\nstdout:\n%s\nstderr:\n%s",
						want, stdout, stderr)
				}
			}
		})
	}
}

func TestRuntimeV2LifecycleDebt280ScopeEventOwnerLaneNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt280(t, true)
	env := lifecycleEnv(
		"SURGE_SHARDS=2",
		"SURGE_THREADS=2",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY:block")
	stdout, stderr, exitCode := runLifecycleHarness(
		t, binPath, "debt280-scope-event-owner-lane", env)
	if exitCode == 0 {
		t.Fatalf("DEBT-280 negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	// Same window, opposite answer: the accounting changed under the held
	// owner and no event was ever published.
	if !strings.Contains(stderr, "debt280 window: events=0 active=0 failfast_triggered=1") {
		t.Fatalf("DEBT-280 negative control did not build the window (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	const want = "debt280 cross-owner completion wrote the scope from its own lane instead of publishing a scope event"
	if !strings.Contains(stderr, want) {
		t.Fatalf("DEBT-280 negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

func buildRuntimeV2LifecycleHarnessDebt280(t *testing.T, negativeControl bool) string {
	t.Helper()
	name := "lifecycle_harness_debt280"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if negativeControl {
		name += "_negative"
		flags = append(flags, "-DRV2_DEBT_280_NEGATIVE_CONTROL")
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

// lifecycleHarnessScopeEventModes is concatenated into the shared lifecycle
// harness translation unit by buildRuntimeV2LifecycleHarnessWithFlags.
const lifecycleHarnessScopeEventModes = `
#ifdef RT_TEST_SYNC_POINTS
#define POLL_DEBT280_SCOPE_OWNER 4090
#define POLL_DEBT280_SCOPE_CHILD 4091
#define POLL_DEBT280_GRANDCHILD 4092

static _Atomic uint32_t g_debt280_owner_entered;
static _Atomic(void*) g_debt280_scope_handle;
static _Atomic(void*) g_debt280_child;
static _Atomic(void*) g_debt280_grandchild;
static _Atomic(void*) g_debt280_sleeper;
static _Atomic uint32_t g_debt280_child_shard;

static void poll_debt280_grandchild(void) {
    rt_async_return(NULL, &(uint64_t){55});
}

// Adopts the grandchild's shard-1 CONNECTION placement through rt_task_poll
// (rt_task_poll_adopt_placement, the production F2 path), publishes the shard
// it now lives on, and then PARKS on a shard-1 task that never completes. A
// yield loop would keep re-running on the carrier that adopted it -- shard
// 0's, the one the held owner blocks -- whereas a park is woken by the cancel
// through the child's owner shard, so the cancellation is observed and
// committed on shard 1: a completion on a shard that is not the scope's.
static void poll_debt280_scope_child(void) {
    if (atomic_load_explicit(&g_debt280_child_shard, memory_order_acquire) == UINT32_MAX) {
        void* gc = atomic_load_explicit(&g_debt280_grandchild, memory_order_acquire);
        uint64_t bits = 0;
        uint8_t st = rt_task_poll(gc, &bits);
        if (st == 0) {
            rt_async_yield(gc, 0);
            return;
        }
        const rt_task* self = rt_current_task();
        atomic_store_explicit(&g_debt280_child_shard, self->owner_shard_id, memory_order_release);
    }
    void* sleeper = atomic_load_explicit(&g_debt280_sleeper, memory_order_acquire);
    uint64_t bits = 0;
    uint8_t st = rt_task_poll(sleeper, &bits);
    if (st == 0) {
        rt_async_yield(sleeper, 0);
        return;
    }
    rt_async_return(NULL, &(uint64_t){0});
}

// The generated tail of a @failfast block: join, exit, Cancelled when the
// join says fail-fast fired and Success otherwise. The child is created by
// the owner inside its scope (creation is the sole writer of membership) and
// the owner keeps yielding until the child has left this shard, so the held
// join below holds ONE live child that completes cross-owner.
static void poll_debt280_scope_owner(void) {
    if (atomic_load_explicit(&g_debt280_owner_entered, memory_order_acquire) == 0) {
        void* handle = rt_scope_enter(true);
        void* child = __task_create(POLL_DEBT280_SCOPE_CHILD, NULL, rt_channel_opaque_word_ops());
        rt_scope_register_child(handle, child);
        atomic_store_explicit(&g_debt280_child, child, memory_order_release);
        atomic_store_explicit(&g_debt280_scope_handle, handle, memory_order_release);
        atomic_store_explicit(&g_debt280_owner_entered, 1, memory_order_release);
        rt_async_yield(NULL, 0);
        return;
    }
    if (atomic_load_explicit(&g_debt280_child_shard, memory_order_acquire) == UINT32_MAX) {
        rt_async_yield(NULL, 0);
        return;
    }
    void* handle = atomic_load_explicit(&g_debt280_scope_handle, memory_order_acquire);
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

// Both answers under the pinned lock, the way join_all must read them.
static size_t debt280_scope_snapshot(rt_executor* ex, uint64_t scope_id, unsigned* triggered) {
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

static uint64_t debt280_scope_events(rt_executor* ex, uint32_t shard_id) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), shard_id);
    struct rt_transport_debug_snapshot snap = rt_transport_debug_snapshot(shard);
    return snap.scope_child_done_events;
}

static int debt280_fail_open(rt_executor* ex, const char* msg) {
    rt_sync_point_open();
    (void)rt_executor_request_shutdown(ex);
    return fail(msg);
}

static int mode_debt280_scope_event_owner_lane(rt_executor* ex) {
    atomic_store_explicit(&g_debt280_owner_entered, 0, memory_order_release);
    atomic_store_explicit(&g_debt280_scope_handle, NULL, memory_order_release);
    atomic_store_explicit(&g_debt280_child, NULL, memory_order_release);
    atomic_store_explicit(&g_debt280_grandchild, NULL, memory_order_release);
    atomic_store_explicit(&g_debt280_sleeper, NULL, memory_order_release);
    atomic_store_explicit(&g_debt280_child_shard, UINT32_MAX, memory_order_release);
    unsigned before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY);
    uint32_t scope_shard = pin_shard(ex, 0);
    uint32_t far_shard = pin_shard(ex, 1);
    if (scope_shard == far_shard) {
        return fail("debt280 needs two shards to build a cross-owner completion");
    }
    rt_task* grandchild =
        spawn_placed(ex, POLL_DEBT280_GRANDCHILD, far_shard, TASK_PLACEMENT_CONNECTION, NULL);
    if (grandchild == NULL) {
        return fail("debt280 grandchild allocation failed");
    }
    atomic_store_explicit(&g_debt280_grandchild, grandchild, memory_order_release);
    rt_task* sleeper =
        spawn_placed(ex, POLL_SPIN_FOREVER, far_shard, TASK_PLACEMENT_CONNECTION, NULL);
    if (sleeper == NULL) {
        return fail("debt280 sleeper allocation failed");
    }
    atomic_store_explicit(&g_debt280_sleeper, sleeper, memory_order_release);
    rt_task* owner = spawn_pinned(ex, POLL_DEBT280_SCOPE_OWNER, scope_shard);
    if (owner == NULL) {
        return fail("debt280 owner allocation failed");
    }
    // The child must live on the far shard BEFORE the owner is held: a held
    // owner is a running task blocked inside its poll, and its carrier is the
    // one a same-shard child would need.
    for (uint32_t i = 0; i < 4000; i++) {
        if (atomic_load_explicit(&g_debt280_child_shard, memory_order_acquire) != UINT32_MAX) {
            break;
        }
        sleep_us(1000);
    }
    uint32_t child_shard = atomic_load_explicit(&g_debt280_child_shard, memory_order_acquire);
    if (child_shard != far_shard) {
        fprintf(stderr, "debt280: scope-child owner shard=%u, expected adopted shard=%u\n",
                child_shard, far_shard);
        return debt280_fail_open(ex, "debt280 scope-child did not adopt the cross-owner shard");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY, before,
                               4000)) {
        return debt280_fail_open(ex, "debt280 owner never reached the join verify window");
    }
    rt_task* child = (rt_task*)atomic_load_explicit(&g_debt280_child, memory_order_acquire);
    uint64_t scope_id =
        (uint64_t)(uintptr_t)atomic_load_explicit(&g_debt280_scope_handle, memory_order_acquire);
    unsigned triggered = 0;
    size_t active = debt280_scope_snapshot(ex, scope_id, &triggered);
    uint64_t events_before = debt280_scope_events(ex, scope_shard);
    if (active != 1 || triggered != 0) {
        return debt280_fail_open(ex, "debt280 caught the join in the wrong state: want one live child, no fail-fast");
    }
    // The owner is held between its first snapshot and the verify. The child
    // completes Cancelled on the far shard inside that gap.
    rt_task_cancel(child);
    if (!wait_task_status(child, TASK_DONE, 4000)) {
        return debt280_fail_open(ex, "debt280 cancelled child never completed");
    }
    // DONE is stored before the scope step runs, so wait for the step's own
    // trace: either the event reached the owner shard's lane, or (the shape
    // the ruling forbids) the scope's answers changed under the held owner.
    uint64_t events = events_before;
    for (uint32_t i = 0; i < 4000; i++) {
        events = debt280_scope_events(ex, scope_shard);
        active = debt280_scope_snapshot(ex, scope_id, &triggered);
        if (events != events_before || active != 1 || triggered != 0) {
            break;
        }
        sleep_us(1000);
    }
    fprintf(stderr, "debt280 window: events=%llu active=%zu failfast_triggered=%u\n",
            (unsigned long long)(events - events_before), active, triggered);
    if (events == events_before) {
        return debt280_fail_open(ex, "debt280 cross-owner completion wrote the scope from its own lane instead of publishing a scope event");
    }
    if (active != 1 || triggered != 0) {
        return debt280_fail_open(ex, "debt280 scope accounting changed while its owner lane was held");
    }
    rt_sync_point_open();
    if (!wait_task_status(owner, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt280 owner stranded after release");
    }
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(owner, &kind, &bits);
    active = debt280_scope_snapshot(ex, scope_id, &triggered);
    fprintf(stderr, "debt280 after: owner kind=%u (1=Success 2=Cancelled) scope_gone=%d\n",
            (unsigned)kind, active == SIZE_MAX);
    if (kind != 2) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt280 failfast scope answered Success after a cross-owner cancelled child");
    }
    if (!await_expect(ex, child, 2, 0, "debt280 cancelled child")) {
        return 1;
    }
    rt_task_cancel(sleeper);
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
#endif
`
