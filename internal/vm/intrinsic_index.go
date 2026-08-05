package vm

import (
	"fortio.org/safecast"

	"surge/internal/mir"
	"surge/internal/types"
)

// handleIndex handles the __index intrinsic.
func (vm *VM) handleIndex(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return vm.eb.makeError(PanicTypeMismatch, "__index requires a destination")
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "__index requires 2 arguments")
	}
	objVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(objVal)
	idxVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(idxVal)
	if objVal.Kind == VKRef || objVal.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(objVal.Loc)
		if loadErr != nil {
			return loadErr
		}
		objVal = v
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	var res Value
	if isReferenceType(vm.Types, dstType) {
		if objVal.Kind != VKHandleArray {
			return vm.eb.typeMismatch("array", objVal.Kind.String())
		}
		view, viewErr := vm.arrayViewFromHandle(objVal.H)
		if viewErr != nil {
			return viewErr
		}
		index, indexErr := vm.arrayIndexFromValue(idxVal, view.length)
		if indexErr != nil {
			return indexErr
		}
		index32, err := safecast.Conv[int32](index)
		if err != nil {
			return vm.eb.invalidLocation("array index overflow")
		}
		res = MakeRef(Location{Kind: LKArrayElem, Handle: objVal.H, Index: index32}, dstType)
	} else {
		res, vmErr = vm.evalIndex(objVal, idxVal)
		if vmErr != nil {
			return vmErr
		}
	}
	if res.TypeID == types.NoTypeID {
		res.TypeID = dstType
	}
	if vmErr := vm.writeLocal(frame, dstLocal, res); vmErr != nil {
		if res.IsHeap() {
			vm.dropValue(res)
		}
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   res,
		})
	}
	return nil
}

// handleIndexSet handles the __index_set intrinsic.
func (vm *VM) handleIndexSet(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	_ = writes
	if len(call.Args) != 3 {
		return vm.eb.makeError(PanicTypeMismatch, "__index_set requires 3 arguments")
	}
	objVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(objVal)
	idxVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(idxVal)
	val, vmErr := vm.evalOperand(frame, &call.Args[2])
	if vmErr != nil {
		return vmErr
	}

	if objVal.Kind == VKRef || objVal.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(objVal.Loc)
		if loadErr != nil {
			vm.dropValue(val)
			return loadErr
		}
		objVal = v
	}
	if objVal.Kind != VKHandleArray {
		vm.dropValue(val)
		return vm.eb.typeMismatch("array", objVal.Kind.String())
	}
	view, vmErr := vm.arrayViewFromHandle(objVal.H)
	if vmErr != nil {
		vm.dropValue(val)
		return vmErr
	}
	idx, vmErr := vm.arrayIndexFromValue(idxVal, view.length)
	if vmErr != nil {
		vm.dropValue(val)
		return vmErr
	}
	idx32, err := safecast.Conv[int32](idx)
	if err != nil {
		vm.dropValue(val)
		return vm.eb.invalidLocation("array index overflow")
	}
	loc := Location{Kind: LKArrayElem, Handle: objVal.H, Index: idx32, IsMut: true}
	if vmErr := vm.storeLocation(loc, val); vmErr != nil {
		vm.dropValue(val)
		return vmErr
	}
	return nil
}
