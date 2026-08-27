package asyncrt

import "testing"

// RV2-DEBT-263, the VM lane's half. A task's answer is decided at the moment
// its completion commits, not by whoever carried the kind in: `cancel` through
// a live handle is task-global and, before committed success, must be observed
// by every awaited entitlement (23-storage-model-and-typed-carrier-abi.md).
//
// The VM reaches the same state as the native runtime by a shorter road and
// without a race: pollTask delivers a DONE target unconditionally and lets the
// SUSPENSION carry the cancellation (execTermAsyncYield, internal/vm/
// vm_terminator.go), so a body whose every await resolves from an already-DONE
// child reaches execTermAsyncReturn with no suspension left to observe the
// cancel at -- and that terminator set PollDoneSuccess unconditionally
// (vm_terminator.go), which runReadyOne handed straight to MarkDone. One
// thread, so there is no window to close here and nothing to linearize: the
// defect is the DECISION, and this is where it is made.
func TestMarkDoneCommitsCancelledForACancelledTask(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	owner := exec.Spawn(1, nil)
	scopeID := exec.EnterScope(owner, true)
	child := exec.Spawn(2, nil)
	exec.RegisterChild(scopeID, child)

	exec.Cancel(child)
	childTask := exec.tasks[child]
	if childTask == nil || !childTask.Cancelled {
		t.Fatalf("expected the child to carry the cancel, got %+v", childTask)
	}

	exec.MarkDone(child, TaskResultSuccess, "the body's value")

	if childTask.ResultKind != TaskResultCancelled {
		t.Fatalf("a cancelled task committed kind %v, want %v",
			childTask.ResultKind, TaskResultCancelled)
	}
	if childTask.ResultValue != "" {
		t.Fatalf("a cancelled task kept a value it did not commit: %q", childTask.ResultValue)
	}
	// The reason the kind matters: fail-fast keys solely on the child's
	// committed kind, so a cancelled child that answers Success leaves its
	// @failfast scope resolving Success after a child was cancelled.
	scope := exec.scopes[scopeID]
	if scope == nil || !scope.FailfastTriggered {
		t.Fatalf("the failfast scope did not fire on the cancelled child: %+v", scope)
	}
}

// The acceptance twin: nothing else may become Cancelled. A task nobody
// cancelled commits the value its body produced, and its scope stays quiet.
func TestMarkDoneKeepsSuccessForATaskNobodyCancelled(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	owner := exec.Spawn(1, nil)
	scopeID := exec.EnterScope(owner, true)
	child := exec.Spawn(2, nil)
	exec.RegisterChild(scopeID, child)

	exec.MarkDone(child, TaskResultSuccess, "the body's value")

	childTask := exec.tasks[child]
	if childTask == nil || childTask.ResultKind != TaskResultSuccess {
		t.Fatalf("an uncancelled task did not commit Success: %+v", childTask)
	}
	if childTask.ResultValue != "the body's value" {
		t.Fatalf("an uncancelled task lost its value: %q", childTask.ResultValue)
	}
	if scope := exec.scopes[scopeID]; scope == nil || scope.FailfastTriggered {
		t.Fatalf("fail-fast fired without a cancelled child: %+v", scope)
	}
}

// CommitKindFor is what lets a caller holding an OWNED payload ask before it
// hands it over -- the VM's Values must be destroyed by the VM, not dropped on
// the floor by a generic executor. It must answer exactly what MarkDone will
// do, or the two drift and a refused value leaks.
func TestCommitKindForAgreesWithMarkDone(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	cancelled := exec.Spawn(1, nil)
	exec.Cancel(cancelled)
	live := exec.Spawn(2, nil)

	if got := exec.CommitKindFor(cancelled, TaskResultSuccess); got != TaskResultCancelled {
		t.Fatalf("CommitKindFor(cancelled, Success) = %v, want %v", got, TaskResultCancelled)
	}
	if got := exec.CommitKindFor(live, TaskResultSuccess); got != TaskResultSuccess {
		t.Fatalf("CommitKindFor(live, Success) = %v, want %v", got, TaskResultSuccess)
	}
	exec.MarkDone(cancelled, TaskResultSuccess, "refused")
	exec.MarkDone(live, TaskResultSuccess, "kept")
	if exec.tasks[cancelled].ResultKind != exec.CommitKindFor(cancelled, TaskResultSuccess) {
		t.Fatalf("MarkDone and CommitKindFor disagreed for the cancelled task")
	}
	if exec.tasks[live].ResultKind != TaskResultSuccess {
		t.Fatalf("MarkDone and CommitKindFor disagreed for the live task")
	}
}
