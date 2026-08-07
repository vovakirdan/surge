package vm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// readLocal reads a local variable, checking initialization and move status.
func (vm *VM) readLocal(frame *Frame, id mir.LocalID) (Value, *VMError) {
	if int(id) < 0 || int(id) >= len(frame.Locals) {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", id))
	}

	slot := &frame.Locals[id]

	if !slot.IsInit {
		return Value{}, vm.eb.useBeforeInit(slot.Name)
	}

	if slot.IsDropped {
		return Value{}, vm.eb.makeError(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: local %q used after drop", slot.Name))
	}

	if slot.IsMoved {
		return Value{}, vm.eb.useAfterMove(slot.Name)
	}

	return slot.V, nil
}

// readGlobal reads a global variable, checking initialization and move status.
func (vm *VM) readGlobal(id mir.GlobalID) (Value, *VMError) {
	if int(id) < 0 || int(id) >= len(vm.Globals) {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid global id %d", id))
	}

	slot := &vm.Globals[id]

	if !slot.IsInit {
		return Value{}, vm.eb.useBeforeInit(slot.Name)
	}

	if slot.IsDropped {
		return Value{}, vm.eb.makeError(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: global %q used after drop", slot.Name))
	}

	if slot.IsMoved {
		return Value{}, vm.eb.useAfterMove(slot.Name)
	}

	return slot.V, nil
}

// coerceToSlotType settles the difference between the type a value carries and
// the type the slot it is going into declares.
//
// Both slot writers ask the same three questions, so they ask them in one
// place. A value that carries no type adopts the slot's; a union adopts the
// slot's union by CONVERSION rather than by relabelling, because the two
// layouts put the same arm in different places; and `nothing` written into a
// slot whose union has a nothing arm becomes that arm, which is the one case
// where a value of no type at all acquires a representation.
func (vm *VM) coerceToSlotType(frame *Frame, val Value, expectedType types.TypeID) (Value, *VMError) {
	if expectedType == types.NoTypeID {
		return val, nil
	}
	if val.TypeID == types.NoTypeID {
		val.TypeID = expectedType
		if val.IsHeap() && val.H != 0 {
			if obj := vm.Heap.Get(val.H); obj != nil && obj.TypeID == types.NoTypeID {
				obj.TypeID = expectedType
			}
		}
	}
	if ref, isTag := vm.isTagStorage(val); isTag {
		converted, changed, vmErr := vm.retagUnion(frame, ref, expectedType)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if changed {
			return converted, nil
		}
		unwrapped, applied, vmErr := vm.unwrapTypeArm(frame, ref, expectedType)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if applied {
			return unwrapped, nil
		}
		return val, nil
	}
	if wrapped, applied, vmErr := vm.wrapTypeArm(frame, val, expectedType); vmErr != nil {
		return Value{}, vmErr
	} else if applied {
		return wrapped, nil
	}
	if val.Kind == VKNothing && vm.tagLayouts != nil {
		if tagLayout, ok := vm.tagLayouts.Layout(vm.valueType(expectedType)); ok && tagLayout != nil {
			if tc, ok := tagLayout.CaseByName("nothing"); ok {
				return vm.buildTag(frame, expectedType, tc.TagSym, nil)
			}
		}
	}
	return val, nil
}

// writeLocal writes a value to a local variable.
func (vm *VM) writeLocal(frame *Frame, id mir.LocalID, val Value) *VMError {
	if int(id) < 0 || int(id) >= len(frame.Locals) {
		return vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", id))
	}

	expectedType := frame.Locals[id].TypeID
	val, vmErr := vm.coerceToSlotType(frame, val, expectedType)
	if vmErr != nil {
		return vmErr
	}

	slot := &frame.Locals[id]
	// A composite slot is DISCRIMINATED from every other kind: its value lives
	// in the activation's arena, and the slot names those bytes rather than
	// holding the value. Writing one is a move into storage this slot already
	// owns, not a replacement of what it points at.
	storage, composite, vmErr := vm.slotStorage(frame, id)
	if vmErr != nil {
		return vmErr
	}
	if composite {
		live := slot.IsInit && !slot.IsMoved && !slot.IsDropped
		if moveErr := vm.moveComposite(frame, storage, val, live); moveErr != nil {
			return moveErr
		}
		slot.V = MakeComposite(storage)
		slot.IsInit = true
		slot.IsMoved = false
		slot.IsDropped = false
		return nil
	}
	if slot.IsInit && !slot.IsMoved && !slot.IsDropped {
		vm.dropValue(slot.V)
	}
	slot.V = val
	slot.IsInit = true
	slot.IsMoved = false
	slot.IsDropped = false
	return nil
}

// writeGlobal writes a value to a global variable.
func (vm *VM) writeGlobal(id mir.GlobalID, val Value) *VMError {
	if int(id) < 0 || int(id) >= len(vm.Globals) {
		return vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid global id %d", id))
	}

	expectedType := vm.Globals[id].TypeID
	val, vmErr := vm.coerceToSlotType(vm.currentFrame(), val, expectedType)
	if vmErr != nil {
		return vmErr
	}

	slot := &vm.Globals[id]
	storage, composite, vmErr := vm.globalStorage(id)
	if vmErr != nil {
		return vmErr
	}
	if composite {
		live := slot.IsInit && !slot.IsMoved && !slot.IsDropped
		if moveErr := vm.moveComposite(vm.currentFrame(), storage, val, live); moveErr != nil {
			return moveErr
		}
		slot.V = MakeComposite(storage)
		slot.IsInit = true
		slot.IsMoved = false
		slot.IsDropped = false
		return nil
	}
	if slot.IsInit && !slot.IsMoved && !slot.IsDropped {
		vm.dropValue(slot.V)
	}
	slot.V = val
	slot.IsInit = true
	slot.IsMoved = false
	slot.IsDropped = false
	return nil
}

// moveLocal marks a local as moved.
func (vm *VM) moveLocal(frame *Frame, id mir.LocalID) {
	if int(id) < 0 || int(id) >= len(frame.Locals) {
		return
	}
	frame.Locals[id].IsMoved = true
}

// moveGlobal marks a global as moved.
func (vm *VM) moveGlobal(id mir.GlobalID) {
	if int(id) < 0 || int(id) >= len(vm.Globals) {
		return
	}
	vm.Globals[id].IsMoved = true
}

func (vm *VM) valueType(id types.TypeID) types.TypeID {
	if id == types.NoTypeID || vm.Types == nil {
		return id
	}
	seen := 0
	for id != types.NoTypeID && seen < 32 {
		seen++
		tt, ok := vm.Types.Lookup(id)
		if !ok {
			return id
		}
		switch tt.Kind {
		case types.KindAlias:
			target, ok := vm.Types.AliasTarget(id)
			if !ok || target == types.NoTypeID || target == id {
				return id
			}
			id = target
		case types.KindOwn, types.KindReference, types.KindPointer:
			id = tt.Elem
		default:
			return id
		}
	}
	return id
}
