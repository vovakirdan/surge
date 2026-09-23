package vm

import (
	"testing"

	"surge/internal/asyncrt"
)

// RV2-DEBT-370, VM: the last entitlement to a cold task ends it -- its start state
// dropped, its body never entered -- and an entitlement that is not the last changes
// nothing.
func TestDroppedColdTaskReleasesItsStateAndNeverRuns(t *testing.T) {
	machine, str, _ := newTaskResultFixture(t)
	handle := machine.Heap.AllocString(str, "captured by the start frame")
	taskID := machine.Async.Create(1, &userTaskState{state: MakeHandleString(handle, str)})
	if vmErr := machine.registerAsyncTaskOwner(taskID, str); vmErr != nil {
		t.Fatalf("register task owner: %v", vmErr)
	}
	machine.taskHandleCreated(taskID)
	machine.taskHandleCreated(taskID)

	machine.taskHandleReleased(taskID)
	task := machine.Async.Task(taskID)
	if !task.Cold || task.State == nil {
		t.Fatalf("an entitlement that was not the last ended the task: %+v", task)
	}
	if obj, _ := machine.Heap.lookup(handle); obj == nil || obj.Freed {
		t.Fatalf("the start state was released while an entitlement remained: %#v", obj)
	}

	machine.taskHandleReleased(taskID)
	if task.Cold || task.Status != asyncrt.TaskDone || task.State != nil {
		t.Fatalf("the last entitlement did not end the cold task: %+v", task)
	}
	if obj, _ := machine.Heap.lookup(handle); obj == nil || !obj.Freed {
		t.Fatalf("the start state outlived its task: %#v", obj)
	}
	if _, ok := machine.taskCohorts[taskID]; ok {
		t.Fatal("a discarded task kept its cohort")
	}
	if id, ok := machine.Async.NextReady(); ok {
		t.Fatalf("task %d became runnable after the discard", id)
	}
}

// The control: a task something published keeps the rule it always had -- the last release
// of a task that has not completed leaves it to run and releases nothing of its state.
func TestDroppedPublishedTaskStillRuns(t *testing.T) {
	machine, str, _ := newTaskResultFixture(t)
	handle := machine.Heap.AllocString(str, "captured by the start frame")
	taskID := machine.Async.Create(1, &userTaskState{state: MakeHandleString(handle, str)})
	if vmErr := machine.registerAsyncTaskOwner(taskID, str); vmErr != nil {
		t.Fatalf("register task owner: %v", vmErr)
	}
	machine.taskHandleCreated(taskID)
	machine.Async.Wake(taskID)

	machine.taskHandleReleased(taskID)
	task := machine.Async.Task(taskID)
	if task.Status == asyncrt.TaskDone || task.State == nil {
		t.Fatalf("the release ended a published task: %+v", task)
	}
	if obj, _ := machine.Heap.lookup(handle); obj == nil || obj.Freed {
		t.Fatalf("the release dropped a published task's state: %#v", obj)
	}
	if id, ok := machine.Async.NextReady(); !ok || id != taskID {
		t.Fatalf("NextReady = %d, %v; want the published task %d", id, ok, taskID)
	}
	machine.releaseTaskState(task)
}

// VM: a member cancelled while cold -- by the cancel-all its cancelled sibling raises --
// answers Cancelled without its body being entered, its start state dropped once, and its scope
// drains fail-fast. The poll functions named here do not exist in the fixture's module, so
// entering either body is a VM error rather than a quiet success.
func TestColdTaskCancelledByFailfastAnswersWithoutRunning(t *testing.T) {
	machine, str, _ := newTaskResultFixture(t)
	exec := machine.Async
	owner := exec.Create(1, nil)
	scopeID := exec.EnterScope(owner, true)
	exec.SetCurrent(owner)
	texts := []Handle{
		machine.Heap.AllocString(str, "the sibling's start state"),
		machine.Heap.AllocString(str, "the member's start state"),
	}
	sibling := exec.Create(9001, &userTaskState{state: MakeHandleString(texts[0], str)})
	member := exec.Create(9002, &userTaskState{state: MakeHandleString(texts[1], str)})
	exec.SetCurrent(0)
	for _, id := range []asyncrt.TaskID{sibling, member} {
		if vmErr := machine.registerAsyncTaskOwner(id, str); vmErr != nil {
			t.Fatalf("register task owner: %v", vmErr)
		}
		machine.taskHandleCreated(id)
	}
	exec.Cancel(sibling)
	for _, want := range []asyncrt.TaskID{sibling, member} {
		ran, vmErr := machine.runReadyOne()
		if vmErr != nil || !ran {
			t.Fatalf("turn for task %d: ran %v, error %v; want it answered without entering its body", want, ran, vmErr)
		}
		if task := exec.Task(want); task.Status != asyncrt.TaskDone || task.ResultKind != asyncrt.TaskResultCancelled || task.State != nil {
			t.Fatalf("task %d after its turn = %+v, want done, Cancelled, start state released", want, task)
		}
	}
	for _, handle := range texts {
		if obj, _ := machine.Heap.lookup(handle); obj == nil || !obj.Freed {
			t.Fatalf("a start state outlived its cancelled task: %#v", obj)
		}
	}
	if done, _, failfast := exec.JoinAllChildrenBlocking(scopeID); !done || !failfast {
		t.Fatalf("join: done %v failfast %v, want drained with fail-fast raised", done, failfast)
	}
	machine.taskHandleReleased(sibling)
	machine.taskHandleReleased(member)
}
