package asyncrt

import (
	"slices"
	"testing"
)

// RV2-DEBT-370: a task made by Create is recorded and is in no ready queue; only a Wake
// (spawn, await, timeout, select) or a Cancel or its scope's join publishes it.

func TestCreateLeavesTheTaskOutOfTheReadyQueue(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	id := exec.Create(1, "frame")
	task := exec.Task(id)
	if task == nil || !task.Cold || task.Status != TaskReady || task.State != "frame" {
		t.Fatalf("created task = %+v, want cold, ready and holding its state", task)
	}
	if slices.Contains(exec.ready, id) {
		t.Fatalf("a created task is queued: %v", exec.ready)
	}
	if got, ok := exec.NextReady(); ok {
		t.Fatalf("task %d is runnable, want none", got)
	}
}

func TestWakePublishesAColdTask(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	id := exec.Create(1, nil)
	exec.Wake(id)
	if task := exec.Task(id); task.Cold {
		t.Fatal("a woken task is still cold")
	}
	if got, ok := exec.NextReady(); !ok || got != id {
		t.Fatalf("NextReady = %d, %v; want the woken task %d", got, ok, id)
	}
}

func TestSpawnIsCreateThenPublish(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	id := exec.Spawn(1, nil)
	if task := exec.Task(id); task.Cold || !slices.Contains(exec.ready, id) {
		t.Fatalf("a spawned task = %+v, ready %v; want published", task, exec.ready)
	}
}

func TestCancelPublishesAColdTask(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	id := exec.Create(1, nil)
	exec.Cancel(id)
	task := exec.Task(id)
	if !task.Cancelled || task.Cold || !slices.Contains(exec.ready, id) {
		t.Fatalf("a cancelled cold task = %+v, ready %v; want cancelled and published", task, exec.ready)
	}
}

func TestDiscardColdRetiresTheMemberWithoutFailfast(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	owner := exec.Spawn(1, nil)
	scopeID := exec.EnterScope(owner, true)
	exec.SetCurrent(owner)
	child := exec.Create(2, "frame")
	exec.SetCurrent(0)
	if scope := exec.scopes[scopeID]; !slices.Contains(scope.Children, child) {
		t.Fatalf("a created task is not its scope's member: %v", scope.Children)
	}
	task := exec.DiscardCold(child)
	if task == nil || task.State != "frame" || task.Status != TaskDone || task.Cold || task.ScopeRegistered {
		t.Fatalf("discarded task = %+v, want done, not cold, not a member, state handed back", task)
	}
	scope := exec.scopes[scopeID]
	if scope.FailfastTriggered || len(scope.Children) != 0 {
		t.Fatalf("scope after the discard = %+v, want drained and not fail-fast", scope)
	}
	if done, _, failfast := exec.JoinAllChildrenBlocking(scopeID); !done || failfast {
		t.Fatalf("join after the discard: done %v failfast %v, want done and not fail-fast", done, failfast)
	}
	if slices.Contains(exec.ready, child) {
		t.Fatal("the discarded task became runnable")
	}
	if exec.DiscardCold(child) != nil {
		t.Fatal("a task was discarded twice")
	}
}

func TestDiscardColdRefusesAPublishedTask(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	woken := exec.Create(1, nil)
	exec.Wake(woken)
	spawned := exec.Spawn(1, nil)
	for _, id := range []TaskID{woken, spawned} {
		if exec.DiscardCold(id) != nil || exec.Task(id).Status == TaskDone {
			t.Fatalf("task %d was discarded after it was published", id)
		}
	}
}

func TestJoinPublishesAColdMemberItStillCounts(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	owner := exec.Spawn(1, nil)
	scopeID := exec.EnterScope(owner, false)
	exec.SetCurrent(owner)
	child := exec.Create(2, nil)
	done, pending, _ := exec.JoinAllChildrenBlocking(scopeID)
	if done || pending != child {
		t.Fatalf("join = done %v pending %d, want to wait on %d", done, pending, child)
	}
	if task := exec.Task(child); task.Cold || !slices.Contains(exec.ready, child) {
		t.Fatalf("the join did not publish its cold member: %+v, ready %v", task, exec.ready)
	}
}

// A cancel that reaches a task while it is cold is recorded by the publication it
// causes, and only then; a task published first and cancelled after keeps the cooperative rule.
func TestCancelWhileColdIsRecordedByThePublication(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	cold := exec.Create(1, nil)
	exec.Cancel(cold)
	if task := exec.Task(cold); !task.CancelledCold || task.Cold || !slices.Contains(exec.ready, cold) {
		t.Fatalf("a task cancelled while cold = %+v, ready %v; want published and marked cancelled-cold", task, exec.ready)
	}
	published := exec.Create(1, nil)
	exec.Wake(published)
	exec.Cancel(published)
	if task := exec.Task(published); task.CancelledCold || !task.Cancelled {
		t.Fatalf("a task cancelled after its publication = %+v, want cancelled and not marked cancelled-cold", task)
	}
}

// The fail-fast road: a member that ends Cancelled cancels every member of its scope, and a
// member still cold when that cancel reaches it is published marked, to be answered unrun.
func TestFailfastCancelAllMarksAColdMemberCancelledCold(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	owner := exec.Spawn(1, nil)
	scopeID := exec.EnterScope(owner, true)
	exec.SetCurrent(owner)
	sibling := exec.Create(2, nil)
	member := exec.Create(3, nil)
	exec.SetCurrent(0)
	exec.Wake(sibling)
	exec.MarkDone(sibling, TaskResultCancelled, "")
	if task := exec.Task(member); !task.CancelledCold || !task.Cancelled || task.Cold {
		t.Fatalf("a cold member reached by the fail-fast cancel-all = %+v, want published and marked cancelled-cold", task)
	}
	if done, _, failfast := exec.JoinAllChildrenBlocking(scopeID); done || !failfast {
		t.Fatalf("join: done %v failfast %v, want to wait on the member with fail-fast raised", done, failfast)
	}
}
