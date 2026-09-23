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
