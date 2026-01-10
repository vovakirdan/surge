package llvm

import (
	"fmt"
	"strconv"
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

func (fe *funcEmitter) emitValueToI64(val, valTy string, typeID types.TypeID) (string, error) {
	switch valTy {
	case "i64":
		return val, nil
	case "ptr":
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = ptrtoint ptr %s to i64\n", out, val)
		return out, nil
	case "double":
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = bitcast double %s to i64\n", out, val)
		return out, nil
	case "float":
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = bitcast float %s to i32\n", tmp, val)
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = zext i32 %s to i64\n", out, tmp)
		return out, nil
	case "half":
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = bitcast half %s to i16\n", tmp, val)
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = zext i16 %s to i64\n", out, tmp)
		return out, nil
	}
	if strings.HasPrefix(valTy, "i") {
		width, err := strconv.Atoi(strings.TrimPrefix(valTy, "i"))
		if err != nil || width <= 0 {
			return "", fmt.Errorf("invalid integer type %s", valTy)
		}
		if width == 64 {
			return val, nil
		}
		op := "zext"
		if info, ok := intInfo(fe.emitter.types, typeID); ok && info.signed {
			op = "sext"
		}
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s to i64\n", out, op, valTy, val)
		return out, nil
	}
	return "", fmt.Errorf("unsupported value type %s for async payload", valTy)
}

func (fe *funcEmitter) emitI64ToValue(bits string, typeID types.TypeID) (value, valueTy string, err error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return "", "", fmt.Errorf("missing type info")
	}
	llvmTy, err := llvmValueType(fe.emitter.types, typeID)
	if err != nil {
		return "", "", err
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
	argType := operandValueType(fe.emitter.types, &call.Args[0])
	if !isTaskType(fe.emitter.types, argType) {
		return true, fmt.Errorf("cancel requires Task handle")
	}
	val, valTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return true, err
	}
	if valTy != "ptr" {
		return true, fmt.Errorf("cancel expects Task pointer, got %s", valTy)
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_task_cancel(ptr %s)\n", val)
	return true, nil
}
