package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

func (fe *funcEmitter) emitAsyncReturnBits(
	value string,
	valueLLVMType string,
	valueType types.TypeID,
) (string, error) {
	if mir.IsDirectFarTaskType(fe.emitter.types, valueType) {
		if valueLLVMType != "ptr" {
			return "", fmt.Errorf("far Task async return must lower as ptr, got %s", valueLLVMType)
		}
		fmt.Fprintf(&fe.emitter.buf,
			"  call void @rt_far_task_prepare_return(ptr %s)\n", value)
	}
	return fe.emitValueToI64(value, valueLLVMType, valueType)
}
