package vm

import (
	"fmt"

	"surge/internal/mir"
)

func (vm *VM) handleTaskCreate(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicUnimplemented, "__task_create missing destination")
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicUnimplemented, "__task_create expects 2 arguments")
	}
	pollFnVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	stateVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	pollFnID, vmErr := vm.int64FromValue(pollFnVal, "poll function id out of range")
	vm.dropValue(pollFnVal)
	if vmErr != nil {
		return vmErr
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	id := exec.Spawn(pollFnID, stateVal)
	taskVal, vmErr := vm.taskValue(id, frame.Locals[call.Dst.Local].TypeID)
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.writeLocal(frame, call.Dst.Local, taskVal); vmErr != nil {
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: call.Dst.Local,
			Name:    frame.Locals[call.Dst.Local].Name,
			Value:   taskVal,
		})
	}
	return nil
}

func (vm *VM) handleCheckpoint(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicUnimplemented, "checkpoint missing destination")
	}
	if len(call.Args) != 0 {
		return vm.eb.makeError(PanicUnimplemented, "checkpoint expects no arguments")
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	id := exec.SpawnCheckpoint()
	taskVal, vmErr := vm.taskValue(id, frame.Locals[call.Dst.Local].TypeID)
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.writeLocal(frame, call.Dst.Local, taskVal); vmErr != nil {
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: call.Dst.Local,
			Name:    frame.Locals[call.Dst.Local].Name,
			Value:   taskVal,
		})
	}
	return nil
}

func (vm *VM) handleTaskClone(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicTypeMismatch, "clone requires a destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "clone requires 1 argument")
	}
	arg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	ownsArg := arg.IsHeap()
	defer func() {
		if ownsArg {
			vm.dropValue(arg)
		}
	}()
	taskID, vmErr := vm.taskIDFromValue(arg)
	if vmErr != nil {
		return vmErr
	}
	dstLocal := call.Dst.Local
	taskVal, vmErr := vm.taskValue(taskID, frame.Locals[dstLocal].TypeID)
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.writeLocal(frame, dstLocal, taskVal); vmErr != nil {
		vm.dropValue(taskVal)
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   taskVal,
		})
	}
	return nil
}

func (vm *VM) handleTaskCancel(frame *Frame, call *mir.CallInstr) *VMError {
	if call == nil {
		return vm.eb.makeError(PanicTypeMismatch, "cancel requires a call")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "cancel requires 1 argument")
	}
	arg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	ownsArg := arg.IsHeap()
	defer func() {
		if ownsArg {
			vm.dropValue(arg)
		}
	}()
	taskID, vmErr := vm.taskIDFromValue(arg)
	if vmErr != nil {
		return vmErr
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	exec.Cancel(taskID)
	return nil
}

func (vm *VM) handleTaskState(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicUnimplemented, "__task_state missing destination")
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	current := exec.Current()
	if current == 0 {
		return vm.eb.makeError(PanicUnimplemented, "__task_state without current task")
	}
	task := exec.Task(current)
	if task == nil {
		return vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", current))
	}
	stateVal, ok := task.State.(Value)
	if !ok {
		return vm.eb.makeError(PanicUnimplemented, "__task_state missing state")
	}
	task.State = nil
	if vmErr := vm.writeLocal(frame, call.Dst.Local, stateVal); vmErr != nil {
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: call.Dst.Local,
			Name:    frame.Locals[call.Dst.Local].Name,
			Value:   stateVal,
		})
	}
	return nil
}
