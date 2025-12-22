package vm

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/mir"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// handleLen handles the __len intrinsic.
func (vm *VM) handleLen(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return vm.eb.makeError(PanicTypeMismatch, "__len requires a destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "__len requires 1 argument")
	}
	arg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arg)
	if arg.Kind == VKRef || arg.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(arg.Loc)
		if loadErr != nil {
			return loadErr
		}
		arg = v
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	switch arg.Kind {
	case VKHandleString:
		obj := vm.Heap.Get(arg.H)
		u64, err := safecast.Conv[uint64](vm.stringCPLen(obj))
		if err != nil {
			return vm.eb.invalidNumericConversion("string length out of range")
		}
		val := vm.makeBigUint(dstType, bignum.UintFromUint64(u64))
		if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})
	case VKHandleArray:
		obj := vm.Heap.Get(arg.H)
		if obj.Kind != OKArray {
			return vm.eb.typeMismatch("array", fmt.Sprintf("%v", obj.Kind))
		}
		u64, err := safecast.Conv[uint64](len(obj.Arr))
		if err != nil {
			return vm.eb.invalidNumericConversion("array length out of range")
		}
		val := vm.makeBigUint(dstType, bignum.UintFromUint64(u64))
		if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})
	case VKHandleStruct:
		info, vmErr := vm.bytesViewLayout(arg.TypeID)
		if vmErr != nil {
			return vmErr
		}
		if !info.ok {
			return vm.eb.typeMismatch("len-compatible", arg.Kind.String())
		}
		obj := vm.Heap.Get(arg.H)
		if obj.Kind != OKStruct {
			return vm.eb.typeMismatch("struct", fmt.Sprintf("%v", obj.Kind))
		}
		if info.ownerIdx < 0 || info.ownerIdx >= len(obj.Fields) || info.lenIdx < 0 || info.lenIdx >= len(obj.Fields) {
			return vm.eb.makeError(PanicOutOfBounds, "bytes view layout mismatch")
		}
		lenVal, vmErr := vm.cloneForShare(obj.Fields[info.lenIdx])
		if vmErr != nil {
			return vmErr
		}
		lenVal.TypeID = dstType
		if vmErr := vm.writeLocal(frame, dstLocal, lenVal); vmErr != nil {
			vm.dropValue(lenVal)
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   lenVal,
		})
	default:
		return vm.eb.typeMismatch("len-compatible", arg.Kind.String())
	}
	return nil
}

// handleClone handles the __clone intrinsic.
func (vm *VM) handleClone(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return vm.eb.makeError(PanicTypeMismatch, "__clone requires a destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "__clone requires 1 argument")
	}
	arg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arg)
	if arg.Kind == VKRef || arg.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(arg.Loc)
		if loadErr != nil {
			return loadErr
		}
		arg = v
	}
	if arg.Kind != VKHandleString {
		return vm.eb.typeMismatch("string", arg.Kind.String())
	}
	clone, vmErr := vm.cloneForShare(arg)
	if vmErr != nil {
		return vmErr
	}
	dstLocal := call.Dst.Local
	if vmErr := vm.writeLocal(frame, dstLocal, clone); vmErr != nil {
		vm.dropValue(clone)
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   clone,
	})
	return nil
}

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
	res, vmErr := vm.evalIndex(objVal, idxVal)
	if vmErr != nil {
		return vmErr
	}
	dstLocal := call.Dst.Local
	if res.TypeID == types.NoTypeID {
		res.TypeID = frame.Locals[dstLocal].TypeID
	}
	if vmErr := vm.writeLocal(frame, dstLocal, res); vmErr != nil {
		if res.IsHeap() {
			vm.dropValue(res)
		}
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   res,
	})
	return nil
}

// handleTo handles the __to intrinsic.
func (vm *VM) handleTo(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return vm.eb.makeError(PanicTypeMismatch, "__to requires a destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "__to requires 1 argument")
	}
	srcVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	dstLocal := call.Dst.Local
	dstTy := frame.Locals[dstLocal].TypeID

	converted, vmErr := vm.evalIntrinsicTo(srcVal, dstTy)
	if vmErr != nil {
		vm.dropValue(srcVal)
		return vmErr
	}
	vmErr = vm.writeLocal(frame, dstLocal, converted)
	if vmErr != nil {
		if !(srcVal.IsHeap() && converted.IsHeap() && srcVal.Kind == converted.Kind && srcVal.H == converted.H) {
			vm.dropValue(srcVal)
		}
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   converted,
	})
	if !(srcVal.IsHeap() && converted.IsHeap() && srcVal.Kind == converted.Kind && srcVal.H == converted.H) {
		vm.dropValue(srcVal)
	}
	return nil
}
