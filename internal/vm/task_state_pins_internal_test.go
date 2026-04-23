package vm

import (
	"testing"

	"surge/internal/asyncrt"
	"surge/internal/types"
)

func TestCollectTaskStatePinsRejectsMovedLocal(t *testing.T) {
	vm := New(nil, nil, nil, nil, nil)
	frame := &Frame{
		Locals: []LocalSlot{{
			Name:    "value",
			V:       MakeInt(7, types.NoTypeID),
			IsInit:  true,
			IsMoved: true,
		}},
	}

	_, vmErr := vm.collectTaskStatePins(MakeRef(Location{
		Kind:     LKLocal,
		FrameRef: frame,
		Local:    0,
	}, types.NoTypeID))
	if vmErr == nil {
		t.Fatal("expected moved local error")
	}
	if vmErr.Code != PanicUseAfterMove {
		t.Fatalf("expected %v, got %v", PanicUseAfterMove, vmErr.Code)
	}
	if got := frame.Locals[0].PinCount; got != 0 {
		t.Fatalf("expected pin count 0, got %d", got)
	}
}

func TestCollectTaskStatePinsRollbackKeepsDetachedLocalAlive(t *testing.T) {
	vm := New(nil, nil, nil, nil, nil)
	frame := &Frame{
		Locals: []LocalSlot{
			{
				Name:   "keep",
				V:      MakeHandleString(vm.Heap.AllocString(types.NoTypeID, "keep"), types.NoTypeID),
				IsInit: true,
			},
			{
				Name:    "moved",
				V:       MakeInt(7, types.NoTypeID),
				IsInit:  true,
				IsMoved: true,
			},
		},
	}
	stateHandle := vm.Heap.AllocArray(types.NoTypeID, []Value{
		MakeRef(Location{Kind: LKLocal, FrameRef: frame, Local: 0}, types.NoTypeID),
		MakeRef(Location{Kind: LKLocal, FrameRef: frame, Local: 1}, types.NoTypeID),
	})

	_, vmErr := vm.collectTaskStatePins(MakeHandleArray(stateHandle, types.NoTypeID))
	if vmErr == nil {
		t.Fatal("expected moved local error")
	}
	if vmErr.Code != PanicUseAfterMove {
		t.Fatalf("expected %v, got %v", PanicUseAfterMove, vmErr.Code)
	}
	if got := frame.Locals[0].PinCount; got != 0 {
		t.Fatalf("expected keep pin count 0, got %d", got)
	}
	if !frame.Locals[0].IsInit {
		t.Fatal("expected keep local to remain initialized after rollback")
	}
	if frame.Locals[0].V.Kind != VKHandleString {
		t.Fatalf("expected keep local string handle, got %v", frame.Locals[0].V.Kind)
	}
	obj := vm.Heap.Get(frame.Locals[0].V.H)
	if obj == nil || obj.Freed || obj.RefCount == 0 {
		t.Fatalf("expected keep local backing object alive, got %#v", obj)
	}
}

func TestDropAsyncTasksDropsWrappedUserTaskPayloads(t *testing.T) {
	vm := New(nil, nil, nil, nil, nil)
	vm.Async = asyncrt.NewExecutor(asyncrt.Config{Deterministic: true})

	stateHandle := vm.Heap.AllocString(types.NoTypeID, "state")
	resultHandle := vm.Heap.AllocString(types.NoTypeID, "result")
	resumeHandle := vm.Heap.AllocString(types.NoTypeID, "resume")

	taskID := vm.Async.Spawn(1, &userTaskState{state: MakeHandleString(stateHandle, types.NoTypeID)})
	task := vm.Async.Task(taskID)
	if task == nil {
		t.Fatal("expected task")
	}
	task.ResultValue = MakeHandleString(resultHandle, types.NoTypeID)
	task.ResumeValue = MakeHandleString(resumeHandle, types.NoTypeID)

	vm.dropAsyncTasks()

	assertFreed := func(handle Handle, label string) {
		t.Helper()
		obj, ok := vm.Heap.lookup(handle)
		if !ok || obj == nil {
			t.Fatalf("expected %s handle %d in heap", label, handle)
		}
		if !obj.Freed || obj.RefCount != 0 {
			t.Fatalf("expected %s handle %d freed, freed=%v rc=%d", label, handle, obj.Freed, obj.RefCount)
		}
	}

	assertFreed(stateHandle, "state")
	assertFreed(resultHandle, "result")
	assertFreed(resumeHandle, "resume")
}
