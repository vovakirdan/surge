package vm

import (
	"surge/internal/asyncrt"
	"surge/internal/mir"
)

func (vm *VM) selectTaskID(frame *Frame, op mir.Operand) (asyncrt.TaskID, *VMError) {
	val, vmErr := vm.evalOperand(frame, &op)
	if vmErr != nil {
		return 0, vmErr
	}
	taskID, vmErr := vm.taskIDFromValue(val)
	vm.dropValue(val)
	if vmErr != nil {
		return 0, vmErr
	}
	return taskID, nil
}

func (vm *VM) selectChannelID(frame *Frame, op mir.Operand) (asyncrt.ChannelID, *VMError) {
	val, vmErr := vm.evalOperand(frame, &op)
	if vmErr != nil {
		return 0, vmErr
	}
	chID, vmErr := vm.channelIDFromValue(val)
	vm.dropValue(val)
	if vmErr != nil {
		return 0, vmErr
	}
	return chID, nil
}
