package llvm

import (
	"fmt"

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
