package asyncrt

import (
	"fmt"
	"testing"
)

func TestScopeExitPanicsOnLiveChildren(t *testing.T) {
	exec := NewExecutor(Config{Deterministic: true})
	owner := exec.Spawn(1, nil)
	scopeID := exec.EnterScope(owner, false)
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
	exec := NewExecutor(Config{Deterministic: true})
	owner := exec.Spawn(1, nil)
	scopeID := exec.EnterScope(owner, false)

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
	if !activeTask.ScopeRegistered || activeTask.ParentScopeID != scopeID {
		t.Fatalf("expected active child registration metadata, got %+v", activeTask)
	}

	exec.MarkDone(active, TaskResultSuccess, nil)
	if len(scope.Children) != 0 {
		t.Fatalf("expected completed child to be pruned, got %v", scope.Children)
	}
	if activeTask.ScopeRegistered || activeTask.ParentScopeID != 0 {
		t.Fatalf("expected completed child metadata to be cleared, got %+v", activeTask)
	}

	completed := exec.Spawn(3, nil)
	exec.MarkDone(completed, TaskResultSuccess, nil)
	exec.RegisterChild(scopeID, completed)
	completedTask := exec.tasks[completed]
	if completedTask == nil {
		t.Fatal("expected completed task to exist")
	}
	if len(scope.Children) != 0 {
		t.Fatalf("expected already completed child to be ignored, got %v", scope.Children)
	}
	if completedTask.ScopeRegistered || completedTask.ParentScopeID != 0 {
		t.Fatalf("expected completed child to remain unregistered, got %+v", completedTask)
	}

	exec.ExitScope(scopeID)
	if ownerTask.ScopeID != 0 {
		t.Fatalf("expected owner scope id to be cleared, got %d", ownerTask.ScopeID)
	}
}

func TestScopeRegisterCancelledChildTriggersFailfast(t *testing.T) {
	exec := NewExecutor(Config{Deterministic: true})
	owner := exec.Spawn(1, nil)
	scopeID := exec.EnterScope(owner, true)

	active := exec.Spawn(2, nil)
	exec.RegisterChild(scopeID, active)

	cancelled := exec.Spawn(3, nil)
	exec.MarkDone(cancelled, TaskResultCancelled, nil)
	exec.RegisterChild(scopeID, cancelled)

	scope := exec.scopes[scopeID]
	if scope == nil {
		t.Fatal("expected scope to exist")
	}
	if !scope.FailfastTriggered {
		t.Fatal("expected failfast to trigger when registering a cancelled child")
	}
	activeTask := exec.tasks[active]
	if activeTask == nil || !activeTask.Cancelled {
		t.Fatalf("expected active child to be cancelled, got %+v", activeTask)
	}
	if len(scope.Children) != 1 || scope.Children[0] != active {
		t.Fatalf("expected active child to remain the only registered child, got %v", scope.Children)
	}
}
