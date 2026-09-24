package vm

import (
	"strings"
	"testing"

	"surge/internal/asyncrt"
	"surge/internal/mir"
)

// RV2-DEBT-372. Once a task's state is released its home is retired, on every
// road a state is released by: the end of the task, a yield that finds the task
// cancelled, the first poll of a task cancelled while cold (RT-COLD-2), the
// discard of a cold task whose last handle went (RV2-DEBT-370), and the shutdown
// drain. A location into the home that outlived the state then fails the
// generation check instead of reading the zeros the release left (storage model
// section 7: "stale locations fail deterministically").
func TestReleasedTaskStateRetiresItsHome(t *testing.T) {
	for _, road := range []struct {
		name    string
		release func(t *testing.T, f *storageFixture, task *asyncrt.Task[asyncPayload])
	}{
		{"release", func(_ *testing.T, f *storageFixture, task *asyncrt.Task[asyncPayload]) {
			f.vm.releaseTaskState(task)
		}},
		{"yield_finds_cancelled", yieldFindsTheTaskCancelled},
		{"cancelled_cold", func(t *testing.T, f *storageFixture, task *asyncrt.Task[asyncPayload]) {
			task.CancelledCold = true
			outcome, vmErr := f.vm.pollTask(task)
			if vmErr != nil || outcome.Kind != asyncrt.PollDoneCancelled {
				t.Fatalf("a task cancelled while cold must answer Cancelled unrun: %v %v", outcome.Kind, vmErr)
			}
		}},
		{"cold_discard", func(t *testing.T, f *storageFixture, task *asyncrt.Task[asyncPayload]) {
			if !f.vm.discardColdTask(task.ID) {
				t.Fatal("a cold task whose last handle went must be discarded")
			}
		}},
		{"shutdown_drain", func(_ *testing.T, f *storageFixture, _ *asyncrt.Task[asyncPayload]) {
			f.vm.dropAsyncTasks()
		}},
	} {
		t.Run(road.name, func(t *testing.T) {
			f := newStorageFixture(t)
			members, err := f.vm.compositeMembers(f.node)
			if err != nil {
				t.Fatalf("Node must have describable members: %v", err)
			}
			creator := f.vm.activate(f.compositeFunc())
			start, err := f.vm.storageRefAt(creator.storage, f.vm.storagePlanFor(creator.Func).OffsetOf(0), f.node)
			if err != nil {
				t.Fatalf("naming the start state must succeed: %v", err)
			}
			label := f.writeNode(t, start, 1, 2, 3, "resident")
			homed, home, vmErr := f.vm.homeTaskState(creator, MakeComposite(start))
			if vmErr != nil || home == nil {
				t.Fatalf("a composite start state must be given a home: %v", vmErr)
			}
			ref, ok := homed.Storage()
			if !ok || ref.Arena != home || ref.Arena == creator.storage {
				t.Fatal("the homed state does not live in its home")
			}
			lent, err := ref.memberRef(members[1])
			if err != nil {
				t.Fatalf("lending the label cell must succeed: %v", err)
			}
			if got, readErr := f.vm.storageReadCell(lent, members[1]); readErr != nil || got.H != label {
				t.Fatalf("the home must hold the label the start state held: %v %v", got, readErr)
			}
			state := &userTaskState{home: home}
			if setErr := f.vm.setUserTaskState(state, homed); setErr != nil {
				t.Fatalf("storing the homed state must succeed: %v", setErr.Message)
			}
			exec := f.vm.ensureExecutor()
			id := exec.Create(1, state)
			road.release(t, f, exec.Task(id))
			got, readErr := f.vm.storageReadCell(lent, members[1])
			if readErr == nil {
				t.Fatalf("PVMR-UNIT a location into a released task's home still reads %v on the %s road", got, road.name)
			}
			if !strings.Contains(readErr.Error(), "stale reference") {
				t.Fatalf("PVMR-UNIT a location into a released task's home fails on the %s road, but not as stale: %v",
					road.name, readErr)
			}
		})
	}
}

// yieldFindsTheTaskCancelled is the road of a task cancelled while it runs. Its
// poll receives the state and reaches its next yield, and the yield ends it
// Cancelled instead of suspending it (execTermAsyncYield); pollUserTask hands
// the state back with the yield's pins, and pollTask, on that Done outcome,
// releases it. It is the road a cancelled parent takes while a child it lent a
// local to may still be parked.
func yieldFindsTheTaskCancelled(t *testing.T, f *storageFixture, task *asyncrt.Task[asyncPayload]) {
	t.Helper()
	exec := f.vm.ensureExecutor()
	exec.SetCurrent(task.ID)
	defer exec.SetCurrent(0)
	task.Cancelled = true
	state, ok := task.State.(*userTaskState)
	if !ok {
		t.Fatal("the task under test holds no user task state")
	}
	poll := f.vm.activate(f.compositeFunc())
	if vmErr := f.vm.handleTaskState(poll, &mir.CallInstr{HasDst: true, Dst: mir.Place{Local: 0}}, nil); vmErr != nil {
		t.Fatalf("the poll must receive its state: %v", vmErr.Message)
	}
	exit := asyncExit{}
	f.vm.asyncCapture = &exit
	f.vm.Stack = []*Frame{poll}
	yield := &mir.Terminator{Kind: mir.TermAsyncYield, AsyncYield: mir.AsyncYieldTerm{
		State: mir.Operand{Kind: mir.OperandMove, Place: mir.Place{Local: 0}},
	}}
	vmErr := f.vm.execTermAsyncYield(poll, yield)
	f.vm.asyncCapture = nil
	if vmErr != nil || !exit.set || exit.kind != asyncrt.PollDoneCancelled {
		t.Fatalf("a yield that finds its task cancelled must end it Cancelled: set=%v kind=%v err=%v", exit.set, exit.kind, vmErr)
	}
	f.vm.setUserTaskStateWithPins(state, exit.state, exit.pins)
	f.vm.releaseTaskState(task)
}
