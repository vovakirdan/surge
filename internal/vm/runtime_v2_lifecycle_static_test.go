//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// static gates. These pin the task/scope lifecycle-lane shape
// decided by 08-lifecycle-lane-proving-spike.md (rules 1-6, decisions
// S5-Q1..S9-Q7). The ACTIVE gates below assert properties already true at
// baseline daeac51e plus the per-site control-lock counter wiring this task
// adds; they run green and are wired into `make runtime-v2-check` via the
// runtime-v2-lifecycle-check stage, whose -run regex enumerates each green
// test by name (the established gate pattern) — the active gates below plus
// behavior contracts. Pending gates are added to that regex by their owning
// task when the Skip is removed.
//
// The PENDING gates at the bottom are the per-path "no control lane" / owner-
// lane assertions for the migrated paths. Following the additive-then-peel rule,
// each lands here now as machinery + a written assertion but t.Skip()s with its
// activating task and exact activation criteria; the owning task deletes the
// Skip line in the same commit that peels its path, so the gate turns green
// with the migration. (This is a deliberate, documented deviation from the
// red-until-wired static tests: t.Skip keeps `go test -tags runtime_v2_pending
// ./...` green while the migration shares the tree.)

// lifecycleFindFunctionBody returns the definition body of a C function in
// runtime/native, reusing the lock-split scanner (same package, same tag).
func lifecycleFindFunctionBody(t *testing.T, name string) string {
	t.Helper()
	return lockSplitFindFunctionBody(t, name)
}

func lifecycleReadNativeFile(t *testing.T, name string) string {
	t.Helper()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "runtime", "native", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

// G1: the per-site attribution enum carries exactly the six census sites plus
// the untagged OTHER residual, and the increment entry point has the expected
// signature. This is the contract used to measure migration steps.
func TestRuntimeV2LifecycleStaticControlSiteEnumShape(t *testing.T) {
	runLockSplitClangShapeCheck(t, `
#include "rt_async_internal.h"

_Static_assert(RT_CTRL_SITE_OTHER == 0,
               "OTHER must be 0: it is the untagged control-lock residual");
_Static_assert(RT_CTRL_SITE_COUNT == 7,
               "six lifecycle census sites plus OTHER");
_Static_assert((int)RT_CTRL_SITE_HANDLE < (int)RT_CTRL_SITE_COUNT,
               "every census site must index within the counter array");

void lifecycle_ctrl_site_api_shape(void);
void lifecycle_ctrl_site_api_shape(void) {
    void (*site)(rt_ctrl_site) = rt_trace_control_lock_site;
    rt_ctrl_site census[] = {
        RT_CTRL_SITE_CREATE,       RT_CTRL_SITE_JOIN_POLL,
        RT_CTRL_SITE_COMPLETION,   RT_CTRL_SITE_SCOPE,
        RT_CTRL_SITE_AWAIT_COMPAT, RT_CTRL_SITE_HANDLE,
    };
    (void)site;
    (void)census;
}
`)
}

// G2 (S5-Q3, rule 2): join waiters route through the target task's atomic
// join-owner shard id. Add/remove/pop must resolve the route, lock that shard,
// and re-read the route under the same lock before touching the store; this
// keeps owner replacement from pairing an old store with a new lock.
func TestRuntimeV2LifecycleStaticJoinWaiterRoutesByTargetOwner(t *testing.T) {
	routeSource := lifecycleReadNativeFile(t, "rt_waiter_route.c")
	if !strings.Contains(routeSource, "case WAKER_JOIN:") {
		t.Fatal("rt_waiter_route.c must handle WAKER_JOIN explicitly")
	}
	if !strings.Contains(routeSource, "rt_task_join_waiter_shard(ex, key.id)") {
		t.Fatal("WAKER_JOIN must resolve through rt_task_join_waiter_shard")
	}
	waiterSource := lifecycleReadNativeFile(t, "rt_waiter_join_route.c")
	for _, needle := range []string{
		"lock_join_waiter_route",
		"rt_task_join_owner_shard_id_load(target)",
		"rt_task_join_owner_shard_id_load(target) == route",
		"rt_waiter_pop_join_waiter",
		"rt_waiter_remove_join_waiter_generation",
		"rt_waiter_collect_join_waiters",
	} {
		if !strings.Contains(waiterSource, needle) {
			t.Fatalf("join waiter route protocol missing %q", needle)
		}
	}
	parkSource := lifecycleReadNativeFile(t, "rt_task_park.c")
	for _, needle := range []string{
		"key.kind == WAKER_JOIN",
		"rt_waiter_collect_join_waiters(ex, key",
	} {
		if !strings.Contains(parkSource, needle) {
			t.Fatalf("wake_key_all_with_policy must route WAKER_JOIN through collect helper; missing %q", needle)
		}
	}
}

// G3 (rule 1): task lookup is a lock-free acquire snapshot — the table pointer
// and the slot are both acquire-loaded, and the copy-on-grow table is published
// through the slot-store/snapshot helpers. This is the protocol S5-Q7 has the
// scope table adopt this protocol.
func TestRuntimeV2LifecycleStaticTaskTableAtomicSnapshot(t *testing.T) {
	body := lifecycleFindFunctionBody(t, "get_task")
	if strings.Count(body, "memory_order_acquire") < 2 {
		t.Fatalf("get_task must acquire-load both the table pointer and the slot:\n%s", body)
	}
	if !strings.Contains(body, "tasks_table") {
		t.Fatalf("get_task must read the atomic tasks_table snapshot:\n%s", body)
	}
	state := lifecycleReadNativeFile(t, "rt_async_state.c")
	for _, needle := range []string{"rt_task_slot_store", "rt_task_table_snapshot"} {
		if !strings.Contains(state, needle) {
			t.Fatalf("task-table atomic-snapshot protocol requires %s in rt_async_state.c", needle)
		}
	}
}

// G4 (rule 6, S9-Q7): join and scope waiter entries stay unqualified (seq == 0)
// — their keys embed a monotonic never-reused id, so the unqualified removal
// predicate is correct and must not adopt the channel park_seq qualification.
func TestRuntimeV2LifecycleStaticJoinScopeWaitersUnqualified(t *testing.T) {
	source := lifecycleReadNativeFile(t, "rt_async_waiter.c")
	if !strings.Contains(source, "seq == 0 || w.seq == seq") {
		t.Fatal("rt_async_waiter.c must keep the unqualified removal predicate " +
			"`seq == 0 || w.seq == seq` for join/scope waiters (rule 6)")
	}
}

// G5: the escalation-critical create-site counter is wired. __task_create takes
// the control lane and tags it CREATE; this counter's 8x1024 per-request value
// decides the segmented-table escalation (>= 2.0/request).
func TestRuntimeV2LifecycleStaticCreateSiteCounterWired(t *testing.T) {
	body := lifecycleFindFunctionBody(t, "__task_create")
	if !strings.Contains(body, "rt_trace_control_lock_site(RT_CTRL_SITE_CREATE)") {
		t.Fatalf("__task_create must tag its control acquisition RT_CTRL_SITE_CREATE:\n%s", body)
	}
}

// G6: every lifecycle census site tags its control acquisition with the matching
// rt_ctrl_site, so cannot silently drop a tag while peeling a path
// (which would make the per-request attribution lie). checkpoint and rt_sleep
// stay intentionally untagged (OTHER): they are spawn-shaped and negligible on
// the net bench.
func TestRuntimeV2LifecycleStaticCensusSitesTagged(t *testing.T) {
	cases := []struct {
		fn  string
		tag string
	}{
		{"__task_create", "RT_CTRL_SITE_CREATE"},
		// rt_task_poll itself no longer takes the control lane
		// at all (P7, StaticJoinPollOwnerLane, below) - its only remaining
		// control acquisition is the rare F2 placement-adoption fallback,
		// which must live in a separate function
		// (rt_task_poll_adopt_placement) so rt_task_poll's own body can
		// satisfy P7's "no rt_control_lock(" bar. Repointed here rather than
		// removed: the site is still real and still tagged, just under a
		// different function name.
		{"rt_task_poll_adopt_placement", "RT_CTRL_SITE_JOIN_POLL"},
		{"rt_task_await", "RT_CTRL_SITE_AWAIT_COMPAT"},
		// rt_task_clone: S5-Q6 drops control unconditionally (unlike clone/
		// wake's scope-adoption case, this is not a rare fallback) - there is
		// nothing left to tag, so this case is deleted rather than repointed.
		{"rt_task_wake", "RT_CTRL_SITE_HANDLE"},
		{"rt_task_cancel", "RT_CTRL_SITE_HANDLE"},
		{"mark_done", "RT_CTRL_SITE_COMPLETION"},
		{"rt_scope_enter", "RT_CTRL_SITE_SCOPE"},
	}
	for _, c := range cases {
		body := lifecycleFindFunctionBody(t, c.fn)
		call := "rt_trace_control_lock_site(" + c.tag + ")"
		if !strings.Contains(body, call) {
			t.Fatalf("%s must tag its control acquisition %s:\n%s", c.fn, call, body)
		}
	}
}

// ===== PENDING gates (delete the t.Skip line in the peel commit) =====

// P6 (create/publish): realization B (the segmented never-moved-slot
// task table, adopted per the escalation verdict: ctrl_create=3.500/req
// >= 2.0) moves id-alloc, slot publish, and ready-push under the owner shard
// lock with no control acquisition in the steady state; only a rare
// segment-growth event still takes control. Activated when the implementation lands:
// asserts ready_push runs under rt_shard_lock in __task_create.
func TestRuntimeV2LifecycleStaticCreateReadyPushOwnerShard(t *testing.T) {
	body := lifecycleFindFunctionBody(t, "__task_create")
	if !strings.Contains(body, "rt_shard_lock(") {
		t.Fatalf("__task_create must ready-push under the owner shard lock:\n%s", body)
	}
}

// P7 (join poll + handle lifetime): the join register + result read move
// to the target owner store lane; the DONE fast path no longer spans the control
// lane. Activated: rt_task_poll itself never takes rt_control_lock (its only
// remaining control acquisition, the rare F2 placement-adoption fallback,
// lives in the separate rt_task_poll_adopt_placement function - see G6 above
// and 08-tasks/07-join-poll-and-handle-lifetime.md).
func TestRuntimeV2LifecycleStaticJoinPollOwnerLane(t *testing.T) {
	body := lifecycleFindFunctionBody(t, "rt_task_poll")
	if strings.Contains(body, "rt_control_lock(") {
		t.Fatalf("rt_task_poll must not hold the control lane on the join path:\n%s", body)
	}
}

// P8 (completion epilogue): mark_done writes result_kind/result_bits
// BEFORE the TASK_DONE release store (rule 1/2), and the WAKER_JOIN reason is
// gone from mark_done_needs_control (S6-Q1) — join-key removal is
// join-owner-route local and runs control-free. The remaining scope reason
// (parent_scope_id/scope_
// registered and the WAKER_SCOPE park_key) stays until the scope bookkeeping moves
// bookkeeping + the scope_key store to the scope owner lane; that
// scope-reason-gone assertion belongs to P9 (TestRuntimeV2LifecycleStaticScope
// OwnerLane), not here. Activated when the implementation lands.
func TestRuntimeV2LifecycleStaticCompletionResultVisibilityOrder(t *testing.T) {
	body := lifecycleFindFunctionBody(t, "mark_done")
	resultIdx := strings.Index(body, "task->result_kind = result_kind")
	doneIdx := strings.Index(body, "task_status_store(task, TASK_DONE)")
	if resultIdx < 0 || doneIdx < 0 || resultIdx > doneIdx {
		t.Fatalf("mark_done must write the result before the TASK_DONE release store:\n%s", body)
	}
	needsControl := lifecycleFindFunctionBody(t, "mark_done_needs_control")
	if strings.Contains(needsControl, "WAKER_JOIN") {
		t.Fatalf("mark_done_needs_control must not keep the WAKER_JOIN reason "+
			"(join removal is owner-local):\n%s", needsControl)
	}
}

// P9 (scope owner lane): ex->scopes becomes an atomic-snapshot table
// (get_scope is a lock-free acquire load), scope object bookkeeping + the
// scope_key store move to the scope owner shard, and WAKER_SCOPE routes to the
// scope owner store. Activation: when the implementation lands; delete
// the Skip and assert get_scope acquire-loads a scopes snapshot and
// rt_waiter_route.c maps WAKER_SCOPE to the scope owner shard.
func TestRuntimeV2LifecycleStaticScopeOwnerLane(t *testing.T) {
	// get_scope is a lock-free acquire snapshot of the segmented scopes_table
	// (mirroring get_task/G3): the segment pointer and the slot are both
	// acquire-loaded.
	body := lifecycleFindFunctionBody(t, "get_scope")
	if strings.Count(body, "memory_order_acquire") < 2 {
		t.Fatalf("get_scope must acquire-load both the scope segment and the slot:\n%s", body)
	}
	if !strings.Contains(body, "scopes_table") {
		t.Fatalf("get_scope must read the atomic scopes_table snapshot:\n%s", body)
	}
	state := lifecycleReadNativeFile(t, "rt_async_state.c")
	if !strings.Contains(state, "rt_scope_slot_store") {
		t.Fatal("scope-table atomic-snapshot protocol requires rt_scope_slot_store in rt_async_state.c")
	}
	// WAKER_SCOPE routes to the scope owner shard store (S5-Q10),
	// D8), not ex->control_waiters.
	route := lifecycleReadNativeFile(t, "rt_waiter_route.c")
	if strings.Contains(route, "case WAKER_SCOPE:\n            return &ex->control_waiters;") {
		t.Fatal("WAKER_SCOPE must move off ex->control_waiters to the scope owner store")
	}
	if !strings.Contains(route, "rt_scope_owner_shard(ex, get_scope(ex, key.id))") {
		t.Fatal("WAKER_SCOPE must resolve the scope owner shard via " +
			"rt_scope_owner_shard(ex, get_scope(ex, key.id))")
	}
	// Scope-reason-gone (the assertion P8 deferred to P9, S6-Q1 complete):
	// mark_done_needs_control no longer forces control for a scope-registered
	// child, and mark_done's park_needs_control no longer keys on WAKER_SCOPE -
	// scope completion runs on the scope owner lane and the scope_key store is
	// owner-local, so both reasons are removed. mark_done_needs_control's final
	// form is net-key + done_waiters (plus the select/multi-key residual).
	needsControl := lifecycleFindFunctionBody(t, "mark_done_needs_control")
	for _, banned := range []string{"parent_scope_id", "scope_registered"} {
		if strings.Contains(needsControl, banned) {
			t.Fatalf("mark_done_needs_control must not keep the scope reason (%s) "+
				"- scope completion is owner-lane:\n%s", banned, needsControl)
		}
	}
	markDone := lifecycleFindFunctionBody(t, "mark_done")
	if strings.Contains(markDone, "WAKER_SCOPE") {
		t.Fatalf("mark_done park_needs_control must not key on WAKER_SCOPE "+
			"(scope_key removal is owner-local):\n%s", markDone)
	}
}

// P10 (await/runner/blocking compat): done_cv and compat_cv stay
// external/main-thread only and are counted separately from the worker-lane
// join path (rule 5). Activation: when the implementation lands; delete the Skip and assert
// the worker-side join path never references done_cv and the await-compat
// counter accounts only for the non-worker (workers>1) await entries.
func TestRuntimeV2LifecycleStaticAwaitCompatCountedSeparately(t *testing.T) {
	// (i) rule 5: the worker-lane join path never touches done_cv. Worker-side
	// join is the owner-lane path (rt_task_poll, P7); done_cv is external-only.
	poll := lifecycleFindFunctionBody(t, "rt_task_poll")
	if strings.Contains(poll, "done_cv") {
		t.Fatalf("worker-lane join (rt_task_poll) must not reference done_cv:\n%s", poll)
	}
	// (ii) the only done_cv waiter is rt_task_await's non-worker (workers>1)
	// branch, and it tags its control acquisition AWAIT_COMPAT so external await
	// is counted separately from worker steady state.
	await := lifecycleFindFunctionBody(t, "rt_task_await")
	if !strings.Contains(await, "pthread_cond_wait(&ex->done_cv") {
		t.Fatalf("rt_task_await (workers>1) must be the done_cv external-await waiter:\n%s", await)
	}
	if !strings.Contains(await, "rt_trace_control_lock_site(RT_CTRL_SITE_AWAIT_COMPAT)") {
		t.Fatalf("rt_task_await must tag its control acquisition AWAIT_COMPAT (rule 5):\n%s", await)
	}
	for _, needle := range []string{
		"rt_done_waiters_increment_for_external_await(ex)",
		"RT_SYNC_POINT(SP_AWAIT_AFTER_INCREMENT)",
		"rt_task_status_load_for_external_await(target)",
		"RT_SYNC_POINT(SP_AWAIT_BEFORE_DONECV_WAIT)",
	} {
		if !strings.Contains(await, needle) {
			t.Fatalf("rt_task_await must keep the RV2-DEBT-022 await-side protocol; missing %q:\n%s",
				needle, await)
		}
	}
	// (iii) mark_done publishes DONE through the external-await StoreLoad helper
	// and delegates the done_cv broadcast to rt_done_cv.c, so the legacy file does
	// not regain lines while the broadcast contract remains pinned.
	markDone := lifecycleFindFunctionBody(t, "mark_done")
	for _, needle := range []string{
		"rt_task_status_store_done_for_external_awaiters(task)",
		"RT_SYNC_POINT(SP_MARKDONE_BEFORE_DONEWAITERS_LOAD)",
		"rt_done_cv_broadcast_after_done(ex)",
	} {
		if !strings.Contains(markDone, needle) {
			t.Fatalf("mark_done must keep the RV2-DEBT-022 completion-side protocol; missing %q:\n%s",
				needle, markDone)
		}
	}
	doneCV := lifecycleFindFunctionBody(t, "rt_done_cv_broadcast_after_done")
	bcastIdx := strings.Index(doneCV, "pthread_cond_broadcast(&ex->done_cv)")
	if bcastIdx < 0 {
		t.Fatalf("rt_done_cv_broadcast_after_done must broadcast done_cv for external awaiters:\n%s",
			doneCV)
	}
	guardIdx := strings.Index(doneCV, "rt_done_waiters_load_after_done(ex)")
	if guardIdx < 0 || guardIdx > bcastIdx {
		t.Fatalf("done_cv broadcast must be guarded by the post-DONE waiter load:\n%s", doneCV)
	}
	for _, needle := range []string{
		"rt_control_lock(ex)",
		"rt_trace_control_lock_site(RT_CTRL_SITE_AWAIT_COMPAT)",
	} {
		if !strings.Contains(doneCV, needle) {
			t.Fatalf("done_cv helper must take/tag the control lane for late external awaiters; missing %q:\n%s",
				needle, doneCV)
		}
	}
	// done_cv is confined to the external-await lane: steady-state completion
	// only ever broadcasts done_cv once, through rt_done_cv.c, under the
	// done_waiters guard above; it never waits on it. The lone waiter is
	// rt_task_await (asserted in (ii)). This is robust to comments mentioning
	// the condvar by name — it counts the actual condvar operations.
	// mark_done moved to rt_task_complete.c; both the legacy
	// file and the completion file must delegate done_cv broadcasting.
	state := lifecycleReadNativeFile(t, "rt_async_state.c")
	complete := lifecycleReadNativeFile(t, "rt_task_complete.c")
	doneCVSource := lifecycleReadNativeFile(t, "rt_done_cv.c")
	if strings.Contains(state, "pthread_cond_broadcast(&ex->done_cv)") {
		t.Fatal("rt_async_state.c must delegate done_cv broadcasting to rt_done_cv.c")
	}
	if strings.Contains(complete, "pthread_cond_broadcast(&ex->done_cv)") {
		t.Fatal("rt_task_complete.c must delegate done_cv broadcasting to rt_done_cv.c")
	}
	if n := strings.Count(doneCVSource, "pthread_cond_broadcast(&ex->done_cv)"); n != 1 {
		t.Fatalf("rt_done_cv.c must broadcast done_cv exactly once "+
			"(the done_waiters-guarded completion broadcast); got %d", n)
	}
	if strings.Contains(state, "pthread_cond_wait(&ex->done_cv") ||
		strings.Contains(complete, "pthread_cond_wait(&ex->done_cv") ||
		strings.Contains(doneCVSource, "pthread_cond_wait(&ex->done_cv") {
		t.Fatal("completion code must never wait on done_cv; the only waiter is rt_task_await")
	}
	header := lifecycleReadNativeFile(t, "rt_async_internal.h")
	for _, item := range []struct {
		name string
		want string
	}{
		{"rt_done_waiters_increment_for_external_await", "memory_order_seq_cst"},
		{"rt_task_status_load_for_external_await", "task_status_load_seq_cst(task)"},
		{"rt_task_status_store_done_for_external_awaiters", "task_status_store_seq_cst(task, TASK_DONE)"},
		{"rt_done_waiters_load_after_done", "memory_order_seq_cst"},
	} {
		body, ok := lockSplitFunctionDefinitionBody(header, item.name)
		if !ok {
			t.Fatalf("rt_async_internal.h must define RV2-DEBT-022 helper %q", item.name)
		}
		if !strings.Contains(body, item.want) {
			t.Fatalf("RV2-DEBT-022 helper %s must keep its seq-cst contract; missing %q:\n%s",
				item.name, item.want, body)
		}
	}
}
