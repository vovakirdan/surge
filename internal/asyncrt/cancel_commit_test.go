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

	refused, givenBack := exec.MarkDone(child, TaskResultSuccess, "the body's value")
	// The refused value must come BACK, not vanish: this executor is generic
	// over its payload and cannot destroy one, so a value it silently zeroed
	// would be a value with no owner left.
	if !givenBack || refused != "the body's value" {
		t.Fatalf("the commit did not hand back the value it refused: refused=%q givenBack=%v",
			refused, givenBack)
	}

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

	refused, givenBack := exec.MarkDone(child, TaskResultSuccess, "the body's value")
	if givenBack || refused != "" {
		t.Fatalf("an ordinary commit handed a value back: refused=%q givenBack=%v",
			refused, givenBack)
	}

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

// A commit that was ALREADY answering Cancelled hands nothing back: its caller
// passed no value in, and inventing one to give back would be a second owner
// for a payload that never had a first.
func TestMarkDoneHandsBackNothingForACancelledCommit(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	task := exec.Spawn(1, nil)

	refused, givenBack := exec.MarkDone(task, TaskResultCancelled, "")
	if givenBack || refused != "" {
		t.Fatalf("a Cancelled commit handed a value back: refused=%q givenBack=%v",
			refused, givenBack)
	}
	if exec.tasks[task].ResultKind != TaskResultCancelled {
		t.Fatalf("a Cancelled commit did not commit Cancelled: %+v", exec.tasks[task])
	}
}
