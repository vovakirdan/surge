package vm

import (
	"testing"

	"surge/internal/mir"
)

// RV2-DEBT-372. A task's async state keeps one address from its first poll to
// its last (docs/RUNTIME_V2.md, owner ruling 2026-09-02; storage model section
// 7, "Stable task activation storage"). A child that borrowed a resident local
// holds a location into the state taken during one poll and reads it during a
// later one; it must find the value, not the zeros a move left behind.
//
// The two polls are driven the way runPoll drives them: `__task_state` fills
// the poll's `__state` slot, a location into the state is lent, and the poll
// suspends as execTermAsyncYield does -- pins collected, the slot moved out,
// the frame dropped and retired, the state handed back with its pins.
//
// The three checks report and go on (Errorf, not Fatalf), so a red row names
// every one that fails. The first poll's arena staying unretired is a side
// effect; what the defect IS -- the lent location reading zeros -- must be in
// the same report, not hidden behind it.
func TestTaskStateKeepsOneAddressAcrossPolls(t *testing.T) {
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
	state := &userTaskState{}
	if vmErr := f.vm.setUserTaskState(state, MakeComposite(start)); vmErr != nil {
		t.Fatalf("storing the start state must succeed: %v", vmErr.Message)
	}
	exec := f.vm.ensureExecutor()
	id := exec.Create(1, state)
	exec.SetCurrent(id)
	defer exec.SetCurrent(0)
	pollFn := f.compositeFunc()
	call := &mir.CallInstr{HasDst: true, Dst: mir.Place{Local: 0}}

	first := f.vm.activate(pollFn)
	if vmErr := f.vm.handleTaskState(first, call, nil); vmErr != nil {
		t.Fatalf("the first poll must receive its state: %v", vmErr.Message)
	}
	held, ok := first.Locals[0].V.Storage()
	if !ok {
		t.Fatal("the first poll's `__state` does not name inline storage")
	}
	lent, err := held.memberRef(members[1])
	if err != nil {
		t.Fatalf("lending the label cell must succeed: %v", err)
	}
	suspendPollUnderTest(t, f.vm, state, first)
	if first.storage.Generation() == 1 {
		t.Error("PVMR-UNIT the first poll's arena was not retired: the state it suspended still lives in it")
	}

	second := f.vm.activate(pollFn)
	if vmErr := f.vm.handleTaskState(second, call, nil); vmErr != nil {
		t.Fatalf("the second poll must receive its state: %v", vmErr.Message)
	}
	now, ok := second.Locals[0].V.Storage()
	if !ok || now.Arena != held.Arena || now.Offset != held.Offset || now.Gen != held.Gen {
		t.Errorf("PVMR-UNIT the task state moved between polls: first at %p+%d gen %d, second at %p+%d gen %d",
			held.Arena, held.Offset, held.Gen, now.Arena, now.Offset, now.Gen)
	}
	got, err := f.vm.storageReadCell(lent, members[1])
	if err != nil || got.Kind != VKHandleString || got.H != label {
		t.Errorf("PVMR-UNIT a location lent in the first poll reads %v (err %v) in the second, want the label handle %d",
			got, err, label)
	}
	suspendPollUnderTest(t, f.vm, state, second)
	f.vm.releaseTaskState(exec.Task(id))
}

// suspendPollUnderTest ends a poll the way execTermAsyncYield and pollUserTask
// do: the state's pins are collected, the `__state` slot is moved out, the
// frame's locals are dropped and the frame retired, and the state goes back to
// the task with its pins.
func suspendPollUnderTest(t *testing.T, machine *VM, state *userTaskState, frame *Frame) {
	t.Helper()
	slot := &frame.Locals[0]
	value := slot.V
	pins, vmErr := machine.collectTaskStatePins(value)
	if vmErr != nil {
		t.Fatalf("collecting the state's pins must succeed: %v", vmErr.Message)
	}
	slot.IsMoved = true
	machine.dropFrameLocals(frame)
	if retireErr := machine.retireActivation(frame); retireErr != nil {
		t.Fatalf("retiring the poll must succeed: %v", retireErr.Message)
	}
	machine.setUserTaskStateWithPins(state, value, pins)
}
