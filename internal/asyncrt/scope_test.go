package asyncrt

import (
	"fmt"
	"testing"
)

func TestScopeExitPanicsOnLiveChildren(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	owner := exec.Spawn(1, nil)
	scopeID := exec.EnterScope(owner, false)
	exec.SetCurrent(owner)
	child := exec.Spawn(2, nil)
	exec.RegisterChild(scopeID, child)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic on scope exit with live children")
		}
		scopeErr, ok := r.(*ScopeExitError)
		if !ok {
			t.Fatalf("expected ScopeExitError, got %T", r)
		}
		msg := scopeErr.Error()
		want := fmt.Sprintf("scope %d exited with live children: [%d]", scopeID, child)
		if msg != want {
			t.Fatalf("panic mismatch: want %q, got %q", want, msg)
		}
		if scopeErr.ScopeID != scopeID {
			t.Fatalf("expected scope id %d, got %d", scopeID, scopeErr.ScopeID)
		}
		if len(scopeErr.LiveChildren) != 1 || scopeErr.LiveChildren[0] != child {
			t.Fatalf("expected live children [%d], got %v", child, scopeErr.LiveChildren)
		}
	}()

	exec.ExitScope(scopeID)
}

func TestScopeDropsCompletedChildrenImmediately(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	owner := exec.Spawn(1, nil)
	scopeID := exec.EnterScope(owner, false)
	exec.SetCurrent(owner)

	scope := exec.scopes[scopeID]
	if scope == nil {
		t.Fatal("expected scope to exist")
	}
	ownerTask := exec.tasks[owner]
	if ownerTask == nil || ownerTask.ScopeID != scopeID {
		t.Fatalf("expected owner scope id %d, got %+v", scopeID, ownerTask)
	}

	active := exec.Spawn(2, nil)
	exec.RegisterChild(scopeID, active)
	activeTask := exec.tasks[active]
	if activeTask == nil {
		t.Fatal("expected active task to exist")
	}
	if len(scope.Children) != 1 || scope.Children[0] != active {
		t.Fatalf("expected active child to be tracked, got %v", scope.Children)
	}
	if !activeTask.ScopeRegistered || activeTask.CreationScopeID != scopeID {
		t.Fatalf("expected active child registration metadata, got %+v", activeTask)
	}

	exec.MarkDone(active, TaskResultSuccess, "")
	if len(scope.Children) != 0 {
		t.Fatalf("expected completed child to be pruned, got %v", scope.Children)
	}
	if activeTask.ScopeRegistered || activeTask.CreationScopeID != scopeID {
		t.Fatalf("expected completed child metadata to be cleared, got %+v", activeTask)
	}

	completed := exec.Spawn(3, nil)
	exec.MarkDone(completed, TaskResultSuccess, "")
	exec.RegisterChild(scopeID, completed)
	completedTask := exec.tasks[completed]
	if completedTask == nil {
		t.Fatal("expected completed task to exist")
	}
	if len(scope.Children) != 0 {
		t.Fatalf("expected already completed child to be ignored, got %v", scope.Children)
	}
	if completedTask.ScopeRegistered || completedTask.CreationScopeID != scopeID {
		t.Fatalf("expected completed child to remain unregistered, got %+v", completedTask)
	}

	exec.ExitScope(scopeID)
	if ownerTask.ScopeID != 0 {
		t.Fatalf("expected owner scope id to be cleared, got %d", ownerTask.ScopeID)
	}
}

func TestScopeMemberCancellationTriggersFailfastAtCompletion(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	owner := exec.Spawn(1, nil)
	scopeID := exec.EnterScope(owner, true)
	exec.SetCurrent(owner)

	active := exec.Spawn(2, nil)
	exec.RegisterChild(scopeID, active)

	cancelled := exec.Spawn(3, nil)
	exec.MarkDone(cancelled, TaskResultCancelled, "")
	exec.RegisterChild(scopeID, cancelled)

	scope := exec.scopes[scopeID]
	if scope == nil {
		t.Fatal("expected scope to exist")
	}
	if !scope.FailfastTriggered {
		t.Fatal("expected failfast to trigger when a member completed cancelled")
	}
	activeTask := exec.tasks[active]
	if activeTask == nil || !activeTask.Cancelled {
		t.Fatalf("expected active child to be cancelled, got %+v", activeTask)
	}
	if len(scope.Children) != 1 || scope.Children[0] != active {
		t.Fatalf("expected active child to remain the only registered child, got %v", scope.Children)
	}
}

func TestScopeRegistrationDoesNotAdoptTaskCreatedOutside(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	foreign := exec.Spawn(2, nil)
	owner := exec.Spawn(1, nil)
	scopeID := exec.EnterScope(owner, true)
	exec.SetCurrent(owner)

	exec.RegisterChild(scopeID, foreign)
	exec.MarkDone(foreign, TaskResultCancelled, "")

	scope := exec.scopes[scopeID]
	if scope == nil || scope.FailfastTriggered || len(scope.Children) != 0 {
		t.Fatalf("foreign task changed scope accounting: %+v", scope)
	}
	foreignTask := exec.tasks[foreign]
	if foreignTask == nil || foreignTask.CreationScopeID != 0 || foreignTask.ScopeRegistered {
		t.Fatalf("foreign task was adopted: %+v", foreignTask)
	}
}
