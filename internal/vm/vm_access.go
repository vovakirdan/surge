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

// writeLocal writes a value to a local variable.
func (vm *VM) writeLocal(frame *Frame, id mir.LocalID, val Value) *VMError {
	if int(id) < 0 || int(id) >= len(frame.Locals) {
		return vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", id))
	}

	expectedType := frame.Locals[id].TypeID
	if val.TypeID == types.NoTypeID && expectedType != types.NoTypeID {
		val.TypeID = expectedType
		if val.IsHeap() && val.H != 0 {
			if obj := vm.Heap.Get(val.H); obj != nil && obj.TypeID == types.NoTypeID {
				obj.TypeID = expectedType
			}
		}
	}
	if expectedType != types.NoTypeID {
		if retagged, ok := vm.retagUnionValue(val, expectedType); ok {
			val = retagged
		}
	}
	if val.Kind == VKNothing && expectedType != types.NoTypeID && vm.tagLayouts != nil {
		if tagLayout, ok := vm.tagLayouts.Layout(vm.valueType(expectedType)); ok && tagLayout != nil {
			if tc, ok := tagLayout.CaseByName("nothing"); ok {
				h := vm.Heap.AllocTag(expectedType, tc.TagSym, nil)
				val = MakeHandleTag(h, expectedType)
			}
		}
	}

	slot := &frame.Locals[id]
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
	if val.TypeID == types.NoTypeID && expectedType != types.NoTypeID {
		val.TypeID = expectedType
		if val.IsHeap() && val.H != 0 {
			if obj := vm.Heap.Get(val.H); obj != nil && obj.TypeID == types.NoTypeID {
				obj.TypeID = expectedType
			}
		}
	}
	if expectedType != types.NoTypeID {
		if retagged, ok := vm.retagUnionValue(val, expectedType); ok {
			val = retagged
		}
	}
	if val.Kind == VKNothing && expectedType != types.NoTypeID && vm.tagLayouts != nil {
		if tagLayout, ok := vm.tagLayouts.Layout(vm.valueType(expectedType)); ok && tagLayout != nil {
			if tc, ok := tagLayout.CaseByName("nothing"); ok {
				h := vm.Heap.AllocTag(expectedType, tc.TagSym, nil)
				val = MakeHandleTag(h, expectedType)
			}
		}
	}

	slot := &vm.Globals[id]
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
