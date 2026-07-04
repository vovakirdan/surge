//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Epic 8 Task 5 static gates. These pin the task/scope lifecycle-lane shape
// decided by 08-lifecycle-lane-proving-spike.md (rules 1-6, decisions
// S5-Q1..S9-Q7). The ACTIVE gates below assert properties already true at
// baseline daeac51e plus the per-site control-lock counter wiring this task
// adds; they run green and are wired into `make runtime-v2-check` via the
// runtime-v2-lifecycle-check stage, whose -run regex enumerates each green
// test by name (Epic 7 precedent) — the active gates below plus Task 4's
// behavior contracts. Pending gates are added to that regex by their owning
// task when the Skip is removed.
//
// The PENDING gates at the bottom are the per-path "no control lane" / owner-
// lane assertions for Tasks 6-10. Following the Epic 7 additive-then-peel rule,
// each lands here now as machinery + a written assertion but t.Skip()s with its
// activating task and exact activation criteria; the owning task deletes the
// Skip line in the same commit that peels its path, so the gate turns green
// with the migration. (This is a deliberate, documented deviation from Epic 7's
// red-until-wired static tests: t.Skip keeps `go test -tags runtime_v2_pending
// ./...` green while Task 4 shares the tree.)

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
// signature. This is the contract Tasks 6-10 measure their peels against.
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

// G2 (S5-Q3, rule 2): join waiters route to the target task's owner shard store
// via the task id, so the join register and the completion drain serialize on
// one lock. This routing is stable across the whole epic (join never moves back
// to control), so it is asserted as a permanent invariant.
func TestRuntimeV2LifecycleStaticJoinWaiterRoutesByTargetOwner(t *testing.T) {
	source := lifecycleReadNativeFile(t, "rt_waiter_route.c")
	if !strings.Contains(source, "case WAKER_JOIN:") {
		t.Fatal("rt_waiter_route.c must handle WAKER_JOIN explicitly")
	}
	if !strings.Contains(source, "rt_task_owner_shard(ex, get_task(ex, key.id))") {
		t.Fatal("WAKER_JOIN must resolve the target owner shard from the task id " +
			"(rt_task_owner_shard(ex, get_task(ex, key.id)))")
	}
}

// G3 (rule 1): task lookup is a lock-free acquire snapshot — the table pointer
// and the slot are both acquire-loaded, and the copy-on-grow table is published
// through the slot-store/snapshot helpers. This is the protocol S5-Q7 has the
// scope table adopt in Task 9.
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
// decides the Task 6 segmented-table escalation (>= 2.0/request).
func TestRuntimeV2LifecycleStaticCreateSiteCounterWired(t *testing.T) {
	body := lifecycleFindFunctionBody(t, "__task_create")
	if !strings.Contains(body, "rt_trace_control_lock_site(RT_CTRL_SITE_CREATE)") {
		t.Fatalf("__task_create must tag its control acquisition RT_CTRL_SITE_CREATE:\n%s", body)
	}
}

// G6: every lifecycle census site tags its control acquisition with the matching
// rt_ctrl_site, so Tasks 6-10 cannot silently drop a tag while peeling a path
// (which would make the per-request attribution lie). checkpoint and rt_sleep
// stay intentionally untagged (OTHER): they are spawn-shaped and negligible on
// the net bench.
func TestRuntimeV2LifecycleStaticCensusSitesTagged(t *testing.T) {
	cases := []struct {
		fn  string
		tag string
	}{
		{"__task_create", "RT_CTRL_SITE_CREATE"},
		{"rt_task_poll", "RT_CTRL_SITE_JOIN_POLL"},
		{"rt_task_await", "RT_CTRL_SITE_AWAIT_COMPAT"},
		{"rt_task_clone", "RT_CTRL_SITE_HANDLE"},
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

// ===== PENDING gates (Tasks 6-10; delete the t.Skip line in the peel commit) =====

// P6 (Task 6, create/publish): realization B (the segmented never-moved-slot
// task table, adopted per the Task 5 escalation verdict: ctrl_create=3.500/req
// >= 2.0) moves id-alloc, slot publish, and ready-push under the owner shard
// lock with no control acquisition in the steady state; only a rare
// segment-growth event still takes control. Activated by Task 6's commit:
// asserts ready_push runs under rt_shard_lock in __task_create.
func TestRuntimeV2LifecycleStaticCreateReadyPushOwnerShard(t *testing.T) {
	body := lifecycleFindFunctionBody(t, "__task_create")
	if !strings.Contains(body, "rt_shard_lock(") {
		t.Fatalf("__task_create must ready-push under the owner shard lock:\n%s", body)
	}
}

// P7 (Task 7, join poll + handle lifetime): the join register + result read move
// to the target owner store lane; the DONE fast path no longer spans the control
// lane. Activation: Task 7's commit; delete the Skip and assert rt_task_poll's
// steady join path takes no rt_control_lock across register/read (mirrors the
// lock-split WorkerLoopShardLane banned-substring gate).
func TestRuntimeV2LifecycleStaticJoinPollOwnerLane(t *testing.T) {
	t.Skip("activates in Task 7 (07-join-poll-and-handle-lifetime): " +
		"rt_task_poll join register + result read run on the target owner store " +
		"lane (rule 2). Assert the poll steady path holds no control lock across " +
		"the join register/DONE read; the JOIN_POLL counter drops toward zero on " +
		"the 8x1024 row.")
	body := lifecycleFindFunctionBody(t, "rt_task_poll")
	if strings.Contains(body, "rt_control_lock(") {
		t.Fatalf("rt_task_poll must not hold the control lane on the join path:\n%s", body)
	}
}

// P8 (Task 8, completion epilogue): mark_done writes result_kind/result_bits
// BEFORE the TASK_DONE release store (rule 1/2), and mark_done_needs_control is
// reduced to net-key removal + done_waiters only (S6-Q1). Activation: Task 8's
// commit; delete the Skip and assert the result writes precede the TASK_DONE
// store and the scope/WAKER_JOIN/WAKER_SCOPE reasons are gone from
// mark_done_needs_control.
func TestRuntimeV2LifecycleStaticCompletionResultVisibilityOrder(t *testing.T) {
	t.Skip("activates in Task 8 (08-completion-epilogue-and-done-path): " +
		"mark_done writes result_kind/result_bits before the TASK_DONE release " +
		"store, and mark_done_needs_control keeps only net-key removal + " +
		"done_waiters>0. Assert the result-write offsets precede the TASK_DONE " +
		"store and the parent_scope_id/WAKER_JOIN/WAKER_SCOPE reasons are removed.")
	body := lifecycleFindFunctionBody(t, "mark_done")
	resultIdx := strings.Index(body, "task->result_kind = result_kind")
	doneIdx := strings.Index(body, "task_status_store(task, TASK_DONE)")
	if resultIdx < 0 || doneIdx < 0 || resultIdx > doneIdx {
		t.Fatalf("mark_done must write the result before the TASK_DONE release store:\n%s", body)
	}
}

// P9 (Task 9, scope owner lane): ex->scopes becomes an atomic-snapshot table
// (get_scope is a lock-free acquire load), scope object bookkeeping + the
// scope_key store move to the scope owner shard, and WAKER_SCOPE routes to the
// scope owner store (revising Epic 7 D8). Activation: Task 9's commit; delete
// the Skip and assert get_scope acquire-loads a scopes snapshot and
// rt_waiter_route.c maps WAKER_SCOPE to the scope owner shard.
func TestRuntimeV2LifecycleStaticScopeOwnerLane(t *testing.T) {
	t.Skip("activates in Task 9 (09-scope-owner-lane): ex->scopes atomic-snapshot, " +
		"get_scope lock-free acquire load, WAKER_SCOPE routes to the scope owner " +
		"store instead of ex->control_waiters. Assert get_scope uses " +
		"memory_order_acquire and rt_waiter_route.c no longer maps WAKER_SCOPE to " +
		"control_waiters.")
	body := lifecycleFindFunctionBody(t, "get_scope")
	if !strings.Contains(body, "memory_order_acquire") {
		t.Fatalf("get_scope must acquire-load the scopes snapshot:\n%s", body)
	}
	route := lifecycleReadNativeFile(t, "rt_waiter_route.c")
	if strings.Contains(route, "case WAKER_SCOPE:\n            return &ex->control_waiters;") {
		t.Fatal("WAKER_SCOPE must move off ex->control_waiters to the scope owner store")
	}
}

// P10 (Task 10, await/runner/blocking compat): done_cv and compat_cv stay
// external/main-thread only and are counted separately from the worker-lane
// join path (rule 5). Activation: Task 10's commit; delete the Skip and assert
// the worker-side join path never references done_cv and the await-compat
// counter accounts only for the non-worker (workers>1) await entries.
func TestRuntimeV2LifecycleStaticAwaitCompatCountedSeparately(t *testing.T) {
	t.Skip("activates in Task 10 (10-await-runner-blocking-compat): done_cv / " +
		"compat_cv remain external-only and counted separately (rule 5). Assert " +
		"only the workers>1 branch of rt_task_await touches done_cv and tags " +
		"AWAIT_COMPAT, and the worker-lane join path (rt_task_poll) never " +
		"references done_cv.")
	body := lifecycleFindFunctionBody(t, "rt_task_poll")
	if strings.Contains(body, "done_cv") {
		t.Fatalf("worker-lane join (rt_task_poll) must not reference done_cv:\n%s", body)
	}
}
