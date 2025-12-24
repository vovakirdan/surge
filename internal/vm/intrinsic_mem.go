package vm

import "surge/internal/mir"

func (vm *VM) handleRtAlloc(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_alloc requires 2 arguments")
	}
	sizeVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(sizeVal)
	alignVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(alignVal)

	size, vmErr := vm.uintValueToInt(sizeVal, "alloc size out of range")
	if vmErr != nil {
		return vmErr
	}
	align, vmErr := vm.uintValueToInt(alignVal, "alloc align out of range")
	if vmErr != nil {
		return vmErr
	}
	handle, vmErr := vm.rawAlloc(size, align)
	if vmErr != nil {
		return vmErr
	}
	if !call.HasDst {
		return vm.rawFree(handle, size, align)
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	ptr := MakePtr(Location{Kind: LKRawBytes, Handle: handle}, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, ptr); vmErr != nil {
		if freeErr := vm.rawFree(handle, size, align); freeErr != nil {
			return freeErr
		}
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   ptr,
		})
	}
	return nil
}

func (vm *VM) handleRtFree(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	_ = writes
	if len(call.Args) != 3 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_free requires 3 arguments")
	}
	ptrVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(ptrVal)
	if ptrVal.Kind != VKPtr {
		return vm.eb.typeMismatch("*byte", ptrVal.Kind.String())
	}
	sizeVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(sizeVal)
	alignVal, vmErr := vm.evalOperand(frame, &call.Args[2])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(alignVal)

	size, vmErr := vm.uintValueToInt(sizeVal, "free size out of range")
	if vmErr != nil {
		return vmErr
	}
	align, vmErr := vm.uintValueToInt(alignVal, "free align out of range")
	if vmErr != nil {
		return vmErr
	}
	if ptrVal.Loc.Kind != LKRawBytes {
		return vm.eb.invalidLocation("rt_free requires raw pointer")
	}
	return vm.rawFree(ptrVal.Loc.Handle, size, align)
}

func (vm *VM) handleRtRealloc(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if len(call.Args) != 4 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_realloc requires 4 arguments")
	}
	ptrVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(ptrVal)
	if ptrVal.Kind != VKPtr {
		return vm.eb.typeMismatch("*byte", ptrVal.Kind.String())
	}
	oldSizeVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(oldSizeVal)
	newSizeVal, vmErr := vm.evalOperand(frame, &call.Args[2])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(newSizeVal)
	alignVal, vmErr := vm.evalOperand(frame, &call.Args[3])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(alignVal)

	oldSize, vmErr := vm.uintValueToInt(oldSizeVal, "old size out of range")
	if vmErr != nil {
		return vmErr
	}
	newSize, vmErr := vm.uintValueToInt(newSizeVal, "new size out of range")
	if vmErr != nil {
		return vmErr
	}
	align, vmErr := vm.uintValueToInt(alignVal, "realloc align out of range")
	if vmErr != nil {
		return vmErr
	}
	if ptrVal.Loc.Kind != LKRawBytes {
		return vm.eb.invalidLocation("rt_realloc requires raw pointer")
	}
	newHandle, vmErr := vm.rawRealloc(ptrVal.Loc.Handle, oldSize, newSize, align)
	if vmErr != nil {
		return vmErr
	}
	if !call.HasDst {
		return vm.rawFree(newHandle, newSize, align)
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	ptr := MakePtr(Location{Kind: LKRawBytes, Handle: newHandle}, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, ptr); vmErr != nil {
		if freeErr := vm.rawFree(newHandle, newSize, align); freeErr != nil {
			return freeErr
		}
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   ptr,
		})
	}
	return nil
}

func (vm *VM) handleRtMemcpy(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	_ = writes
	if len(call.Args) != 3 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_memcpy requires 3 arguments")
	}
	dstVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(dstVal)
	srcVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(srcVal)
	nVal, vmErr := vm.evalOperand(frame, &call.Args[2])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(nVal)
	if dstVal.Kind != VKPtr {
		return vm.eb.typeMismatch("*byte", dstVal.Kind.String())
	}
	if srcVal.Kind != VKPtr {
		return vm.eb.typeMismatch("*byte", srcVal.Kind.String())
	}
	n, vmErr := vm.uintValueToInt(nVal, "memcpy length out of range")
	if vmErr != nil {
		return vmErr
	}
	if n == 0 {
		return nil
	}
	srcRange, srcOK, vmErr := vm.pointerRange(srcVal, n)
	if vmErr != nil {
		return vmErr
	}
	dstRange, dstOK, vmErr := vm.pointerRange(dstVal, n)
	if vmErr != nil {
		return vmErr
	}
	if srcOK && dstOK && srcRange.kind == dstRange.kind && srcRange.handle == dstRange.handle {
		if rangesOverlap(srcRange.start, srcRange.end, dstRange.start, dstRange.end) {
			return vm.eb.makeError(PanicInvalidLocation, "rt_memcpy overlapping ranges")
		}
	}
	data, vmErr := vm.readBytesFromPointer(srcVal, n)
	if vmErr != nil {
		return vmErr
	}
	return vm.writeBytesToPointer(dstVal, data)
}

func (vm *VM) handleRtMemmove(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	_ = writes
	if len(call.Args) != 3 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_memmove requires 3 arguments")
	}
	dstVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(dstVal)
	srcVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(srcVal)
	nVal, vmErr := vm.evalOperand(frame, &call.Args[2])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(nVal)
	if dstVal.Kind != VKPtr {
		return vm.eb.typeMismatch("*byte", dstVal.Kind.String())
	}
	if srcVal.Kind != VKPtr {
		return vm.eb.typeMismatch("*byte", srcVal.Kind.String())
	}
	n, vmErr := vm.uintValueToInt(nVal, "memmove length out of range")
	if vmErr != nil {
		return vmErr
	}
	if n == 0 {
		return nil
	}
	data, vmErr := vm.readBytesFromPointer(srcVal, n)
	if vmErr != nil {
		return vmErr
	}
	return vm.writeBytesToPointer(dstVal, data)
}
