package vm

import (
	"fmt"

	"surge/internal/mir"
)

// A task's async state lives in ONE place for the whole life of the task: its
// home.
//
// A poll names the state through its `__state` local, and a child task that
// borrows a local the async split promoted into the state holds a location INTO
// it -- a `__resident$` field (internal/mir/async_resident_places.go). That
// location is sound only while the state stays where it was: docs/RUNTIME_V2.md,
// owner ruling 2026-09-02 ("a captured borrow needs address-stable storage"),
// and section 7 of runtime-v2-epics/23-storage-model-and-typed-carrier-abi.md
// ("Stable task activation storage": every task activation has one region whose
// fields keep a fixed offset for the life of the activation). Native code has
// that by construction, because `__task_state` returns the pointer to the one
// heap frame. The VM used to MOVE the state into each poll's own arena, zeroing
// the bytes it came from without changing their generation, so a child's
// location read zeros after its parent's next resume (RV2-DEBT-372).
//
// So the state moves exactly once, when the task is created, into an arena the
// task owns; every poll's `__state` slot then NAMES that arena instead of
// receiving a copy; and when the task's state is released the home is retired,
// so a location that outlived it fails the generation check -- section 7's
// "stale locations fail deterministically" -- instead of reading zeros.

// homeTaskState moves a new task's start state into storage that belongs to the
// task, and returns the state as it now lives there together with that storage.
// A state that is not an inline composite has no address to keep: it is
// returned as it is, with no home.
func (vm *VM) homeTaskState(frame *Frame, state Value) (Value, *Arena, *VMError) {
	src, ok := state.Storage()
	if !ok {
		return state, nil, nil
	}
	size, err := vm.storageSizeOf(src.TypeID)
	if err != nil {
		return Value{}, nil, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	home := newArena(&StoragePlan{Size: size, Align: src.Align}, 1)
	dst, err := vm.storageRefAt(home, 0, src.TypeID)
	if err != nil {
		return Value{}, nil, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	if vmErr := vm.moveComposite(frame, dst, state, false); vmErr != nil {
		return Value{}, nil, vmErr
	}
	return MakeComposite(dst), home, nil
}

// installTaskState makes a poll's `__state` slot NAME the task's state where it
// lives, rather than move it into the poll's own arena. Everything that reads
// the slot reads slot.V -- a field projection, the move a yield or a return
// makes of it, the frame's drops -- so every one of them reaches the task's
// storage and nothing reaches a copy. A state that is not an inline composite
// is written as any value is.
func (vm *VM) installTaskState(frame *Frame, id mir.LocalID, state Value) *VMError {
	ref, ok := state.Storage()
	if !ok {
		return vm.writeLocal(frame, id, state)
	}
	if int(id) < 0 || int(id) >= len(frame.Locals) {
		return vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", id))
	}
	if ref.Arena == frame.storage {
		return vm.eb.invalidLocation("a task state already lives in the arena of the poll that is resuming it")
	}
	slot := &frame.Locals[id]
	// writeLocal would have coerced the value to the slot's type. An alias takes
	// the state as it is, so it takes only a state of the slot's own type,
	// compared resolved as copyPreflight compares the two sides of a copy.
	if vm.valueType(ref.TypeID) != vm.valueType(slot.TypeID) {
		return vm.eb.typeMismatch(fmt.Sprintf("task state of type#%d", slot.TypeID), fmt.Sprintf("type#%d", ref.TypeID))
	}
	if slot.IsInit && !slot.IsMoved && !slot.IsDropped {
		return vm.eb.invalidLocation(fmt.Sprintf("task state slot %q is already live", slot.Name))
	}
	slot.V = state
	slot.IsInit = true
	slot.IsMoved = false
	slot.IsDropped = false
	return nil
}

// retireHome retires a task's home once its state has been released.
//
// The home is retired whatever pins it. A pin keeps bytes for a reader that is
// owed them; a home whose state is gone owes nothing, and the only reader left
// is a location that outlived its owner, which must be refused rather than read
// as zeros. Its pins are left counted: whoever holds one still gives it back.
//
// Such a location may still be HELD when the home goes: a child parked while
// its parent was cancelled wakes to the same cancel after the parent's state
// was released, and reaches its own yield still holding its `&l`. Holding is
// not a use, so the pin collector passes a stale location over
// (taskStatePinCollector.visitStorage); only a dereference is refused. Keeping
// the home until that pin comes back instead would let such a child read zeros.
func (s *userTaskState) retireHome() {
	if s == nil || s.home == nil {
		return
	}
	s.home.invalidate()
	s.home = nil
}
