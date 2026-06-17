package vm

import (
	"syscall"

	"surge/internal/mir"
	"surge/internal/types"
)

func (vm *VM) handleNetReadBytes(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_net_read_bytes requires 2 arguments")
	}
	connVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(connVal)
	handle, vmErr := vm.netConnHandleFromValue(connVal)
	if vmErr != nil {
		return vmErr
	}
	capVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(capVal)
	capacity, vmErr := vm.uintValueToInt(capVal, "net read cap out of range")
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}

	entry := vm.netConns[handle]
	if entry == nil || entry.closed {
		return vm.netWriteError(frame, dstLocal, errType, netErrNotConnected, writes)
	}
	var data []byte
	if capacity > 0 {
		data = make([]byte, capacity)
		var n int
		var err error
		for {
			n, err = syscall.Read(entry.fd, data)
			if err == syscall.EINTR {
				continue
			}
			break
		}
		if err != nil {
			return vm.netWriteError(frame, dstLocal, errType, netErrorCodeFromErr(err), writes)
		}
		data = data[:n]
	}

	arrType, elemType, vmErr := vm.netByteArrayPayloadType(dstType)
	if vmErr != nil {
		return vmErr
	}
	elems := make([]Value, len(data))
	for i, b := range data {
		elems[i] = MakeInt(int64(b), elemType)
	}
	arrHandle := vm.Heap.AllocArray(arrType, elems)
	arrVal := MakeHandleArray(arrHandle, arrType)
	return vm.netWriteSuccess(frame, dstLocal, dstType, arrVal, writes)
}

func (vm *VM) handleNetWriteBytes(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 4 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_net_write_bytes requires 4 arguments")
	}
	connVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(connVal)
	handle, vmErr := vm.netConnHandleFromValue(connVal)
	if vmErr != nil {
		return vmErr
	}
	arrVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arrVal)
	offsetVal, vmErr := vm.evalOperand(frame, &call.Args[2])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(offsetVal)
	lenVal, vmErr := vm.evalOperand(frame, &call.Args[3])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(lenVal)
	offset, vmErr := vm.uintValueToInt(offsetVal, "net write offset out of range")
	if vmErr != nil {
		return vmErr
	}
	length, vmErr := vm.uintValueToInt(lenVal, "net write length out of range")
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}

	entry := vm.netConns[handle]
	if entry == nil || entry.closed {
		return vm.netWriteError(frame, dstLocal, errType, netErrNotConnected, writes)
	}
	if arrVal.Kind == VKRef || arrVal.Kind == VKRefMut {
		loaded, loadErr := vm.loadLocationRaw(arrVal.Loc)
		if loadErr != nil {
			return loadErr
		}
		arrVal = loaded
	}
	if arrVal.Kind != VKHandleArray {
		return vm.eb.typeMismatch("byte[]", arrVal.Kind.String())
	}
	view, vmErr := vm.arrayViewFromHandle(arrVal.H)
	if vmErr != nil {
		return vmErr
	}
	if offset > view.length || length > view.length-offset {
		return vm.netWriteError(frame, dstLocal, errType, netErrIo, writes)
	}
	data := make([]byte, length)
	for i := range length {
		b, convErr := vm.valueToUint8(view.baseObj.Arr[view.start+offset+i])
		if convErr != nil {
			return convErr
		}
		data[i] = b
	}
	var n int
	var err error
	if len(data) > 0 {
		for {
			n, err = syscall.Write(entry.fd, data)
			if err == syscall.EINTR {
				continue
			}
			break
		}
		if err != nil {
			return vm.netWriteError(frame, dstLocal, errType, netErrorCodeFromErr(err), writes)
		}
	}

	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		return vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	countVal, makeErr := vm.makeUintForType(tc.PayloadTypes[0], uint64(n)) //nolint:gosec // n from syscall.Write is non-negative.
	if makeErr != nil {
		return makeErr
	}
	return vm.netWriteSuccess(frame, dstLocal, dstType, countVal, writes)
}

func (vm *VM) netByteArrayPayloadType(dstType types.TypeID) (arrayType, elementType types.TypeID, vmErr *VMError) {
	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		return types.NoTypeID, types.NoTypeID, vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		return types.NoTypeID, types.NoTypeID, vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	arrayType = tc.PayloadTypes[0]
	baseArrType := vm.valueType(arrayType)
	elementType, ok = vm.Types.ArrayInfo(baseArrType)
	if !ok || vm.valueType(elementType) != vm.Types.Builtins().Uint8 {
		return types.NoTypeID, types.NoTypeID, vm.eb.makeError(PanicTypeMismatch, "rt_net_read_bytes requires byte[] payload")
	}
	return arrayType, elementType, nil
}
