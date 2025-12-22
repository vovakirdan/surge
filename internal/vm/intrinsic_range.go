package vm

import (
	"surge/internal/mir"
)

// handleRangeIntNew handles the rt_range_int_new intrinsic.
func (vm *VM) handleRangeIntNew(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 3 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_range_int_new requires 3 arguments")
	}
	startVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(startVal)
	endVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(endVal)
	incVal, vmErr := vm.evalOperand(frame, &call.Args[2])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(incVal)
	if incVal.Kind != VKBool {
		return vm.eb.typeMismatch("bool", incVal.Kind.String())
	}
	startStored, vmErr := vm.cloneForShare(startVal)
	if vmErr != nil {
		return vmErr
	}
	endStored, vmErr := vm.cloneForShare(endVal)
	if vmErr != nil {
		vm.dropValue(startStored)
		return vmErr
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	h := vm.Heap.AllocRange(dstType, startStored, endStored, true, true, incVal.Bool)
	val := MakeHandleRange(h, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		vm.Heap.Release(h)
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   val,
	})
	return nil
}

// handleRangeIntFromStart handles the rt_range_int_from_start intrinsic.
func (vm *VM) handleRangeIntFromStart(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_range_int_from_start requires 2 arguments")
	}
	startVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(startVal)
	incVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(incVal)
	if incVal.Kind != VKBool {
		return vm.eb.typeMismatch("bool", incVal.Kind.String())
	}
	startStored, vmErr := vm.cloneForShare(startVal)
	if vmErr != nil {
		return vmErr
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	h := vm.Heap.AllocRange(dstType, startStored, Value{}, true, false, incVal.Bool)
	val := MakeHandleRange(h, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		vm.Heap.Release(h)
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   val,
	})
	return nil
}

// handleRangeIntToEnd handles the rt_range_int_to_end intrinsic.
func (vm *VM) handleRangeIntToEnd(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_range_int_to_end requires 2 arguments")
	}
	endVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(endVal)
	incVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(incVal)
	if incVal.Kind != VKBool {
		return vm.eb.typeMismatch("bool", incVal.Kind.String())
	}
	endStored, vmErr := vm.cloneForShare(endVal)
	if vmErr != nil {
		return vmErr
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	h := vm.Heap.AllocRange(dstType, Value{}, endStored, false, true, incVal.Bool)
	val := MakeHandleRange(h, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		vm.Heap.Release(h)
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   val,
	})
	return nil
}

// handleRangeIntFull handles the rt_range_int_full intrinsic.
func (vm *VM) handleRangeIntFull(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_range_int_full requires 1 argument")
	}
	incVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(incVal)
	if incVal.Kind != VKBool {
		return vm.eb.typeMismatch("bool", incVal.Kind.String())
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	h := vm.Heap.AllocRange(dstType, Value{}, Value{}, false, false, incVal.Bool)
	val := MakeHandleRange(h, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		vm.Heap.Release(h)
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   val,
	})
	return nil
}
