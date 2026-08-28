package llvm

import (
	"fmt"
	"strings"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (fe *funcEmitter) taskResultInfo(resultType types.TypeID) (successIdx int, payloadType types.TypeID, err error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return -1, types.NoTypeID, fmt.Errorf("missing type info")
	}
	if resultType == types.NoTypeID {
		return -1, types.NoTypeID, fmt.Errorf("missing task result type")
	}
	resultType = resolveValueType(fe.emitter.types, resultType)
	successCaseIdx, successMeta, successErr := fe.emitter.tagCaseMeta(resultType, "Success", symbols.NoSymbolID)
	if successErr != nil {
		return -1, types.NoTypeID, successErr
	}
	if len(successMeta.PayloadTypes) != 1 {
		return -1, types.NoTypeID, fmt.Errorf("TaskResult::Success expects single payload")
	}
	_, cancelMeta, cancelErr := fe.emitter.tagCaseMeta(resultType, "Cancelled", symbols.NoSymbolID)
	if cancelErr != nil {
		return -1, types.NoTypeID, cancelErr
	}
	if len(cancelMeta.PayloadTypes) != 0 {
		return -1, types.NoTypeID, fmt.Errorf("TaskResult::Cancelled expects no payload")
	}
	return successCaseIdx, successMeta.PayloadTypes[0], nil
}

func (fe *funcEmitter) emitI64ToValue(bits string, typeID types.TypeID) (value, valueTy string, err error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return "", "", fmt.Errorf("missing type info")
	}
	llvmTy, err := fe.emitter.llvmValueType(typeID)
	if err != nil {
		return "", "", err
	}
	if fe.emitter.hasInlineStorage(typeID) {
		// The bits address the transport allocation the producer copied into.
		// Taking the value out of it and releasing it here is what returns the
		// value to ordinary storage; nothing downstream sees the allocation.
		allocation := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = inttoptr i64 %s to ptr\n", allocation, bits)
		storage, err := fe.emitStorageAlloca(typeID)
		if err != nil {
			return "", "", err
		}
		if err := fe.emitAdoptFromTransportAllocation(allocation, storage, typeID); err != nil {
			return "", "", err
		}
		return storage, handleType, nil
	}
	switch llvmTy {
	case "ptr":
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = inttoptr i64 %s to ptr\n", out, bits)
		return out, "ptr", nil
	case "double":
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = bitcast i64 %s to double\n", out, bits)
		return out, "double", nil
	case "float":
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = trunc i64 %s to i32\n", tmp, bits)
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = bitcast i32 %s to float\n", out, tmp)
		return out, "float", nil
	case "half":
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = trunc i64 %s to i16\n", tmp, bits)
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = bitcast i16 %s to half\n", out, tmp)
		return out, "half", nil
	case "i64":
		return bits, "i64", nil
	}
	if strings.HasPrefix(llvmTy, "i") {
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = trunc i64 %s to %s\n", out, bits, llvmTy)
		return out, llvmTy, nil
	}
	return "", "", fmt.Errorf("unsupported async payload type %s", llvmTy)
}

func (fe *funcEmitter) emitTaskCancelIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "cancel" {
		return false, nil
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("cancel requires 1 argument")
	}
	val, err := fe.emitTaskHandleOperand(&call.Args[0])
	if err != nil {
		return true, err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_task_cancel(ptr %s)\n", val)
	return true, nil
}
