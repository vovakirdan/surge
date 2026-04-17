package vm

import (
	"surge/internal/mir"
	"surge/internal/types"
)

const (
	entropyErrUnavailable uint64 = 1
	entropyErrBackend     uint64 = 2
)

func entropyErrorMessage(code uint64) string {
	switch code {
	case entropyErrUnavailable:
		return "Unavailable"
	default:
		return "Backend"
	}
}

func (vm *VM) entropyErrorValue(errType types.TypeID, code uint64) (Value, *VMError) {
	return vm.makeErrorLikeValue(errType, entropyErrorMessage(code), code)
}

func (vm *VM) entropySuccessValue(dstType types.TypeID, payload Value) (Value, *VMError) {
	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		return Value{}, vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	payloadType := tc.PayloadTypes[0]
	if payload.TypeID == types.NoTypeID && payloadType != types.NoTypeID {
		payload.TypeID = payloadType
	}
	if payload.TypeID != types.NoTypeID && vm.valueType(payload.TypeID) != vm.valueType(payloadType) {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "Erring Success payload type mismatch")
	}
	h := vm.Heap.AllocTag(dstType, tc.TagSym, []Value{payload})
	return MakeHandleTag(h, dstType), nil
}

func (vm *VM) entropyWriteError(frame *Frame, dstLocal mir.LocalID, errType types.TypeID, code uint64, writes *[]LocalWrite) *VMError {
	errVal, vmErr := vm.entropyErrorValue(errType, code)
	if vmErr != nil {
		return vmErr
	}
	if writeErr := vm.writeLocal(frame, dstLocal, errVal); writeErr != nil {
		vm.dropValue(errVal)
		return writeErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   errVal,
		})
	}
	return nil
}

func (vm *VM) entropyWriteSuccess(frame *Frame, dstLocal mir.LocalID, dstType types.TypeID, payload Value, writes *[]LocalWrite) *VMError {
	resVal, vmErr := vm.entropySuccessValue(dstType, payload)
	if vmErr != nil {
		vm.dropValue(payload)
		return vmErr
	}
	if writeErr := vm.writeLocal(frame, dstLocal, resVal); writeErr != nil {
		vm.dropValue(resVal)
		return writeErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   resVal,
		})
	}
	return nil
}

func (vm *VM) handleRtEntropyBytes(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_entropy_bytes requires 1 argument")
	}
	if vm.RT == nil {
		return vm.eb.makeError(PanicUnimplemented, "runtime missing")
	}

	lenVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(lenVal)

	n, vmErr := vm.uintValueToInt(lenVal, "entropy length out of range")
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	errType, vmErr := vm.erringErrorType(dstType)
	if vmErr != nil {
		return vmErr
	}

	data, err := vm.RT.EntropyBytes(n)
	if err != nil {
		return vm.entropyWriteError(frame, dstLocal, errType, entropyErrorCode(err), writes)
	}

	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		return vmErr
	}
	tc, ok := layout.CaseByName("Success")
	if !ok || len(tc.PayloadTypes) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "Erring missing Success tag payload")
	}
	arrType := tc.PayloadTypes[0]
	elemType, ok := vm.Types.ArrayInfo(arrType)
	if !ok {
		return vm.eb.makeError(PanicTypeMismatch, "rt_entropy_bytes requires byte[] payload")
	}
	elems := make([]Value, len(data))
	for i, b := range data {
		elems[i] = MakeInt(int64(b), elemType)
	}
	arrHandle := vm.Heap.AllocArray(arrType, elems)
	arrVal := MakeHandleArray(arrHandle, arrType)
	return vm.entropyWriteSuccess(frame, dstLocal, dstType, arrVal, writes)
}
