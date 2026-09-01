package vm

import (
	"fmt"

	"surge/internal/asyncrt"
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
	taskType := frame.Locals[call.Dst.Local].TypeID
	payloadType, vmErr := vm.runtimeHandlePayloadType(taskType, "Task")
	if vmErr != nil {
		return vmErr
	}
	state := &userTaskState{}
	if stateErr := vm.setUserTaskState(state, stateVal); stateErr != nil {
		return stateErr
	}
	id := exec.Spawn(pollFnID, state)
	if registerErr := vm.registerAsyncTaskOwner(id, payloadType); registerErr != nil {
		return registerErr
	}
	taskVal, vmErr := vm.taskValue(id, taskType)
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
	taskType := frame.Locals[call.Dst.Local].TypeID
	payloadType, vmErr := vm.runtimeHandlePayloadType(taskType, "checkpoint Task")
	if vmErr != nil {
		return vmErr
	}
	if registerErr := vm.registerAsyncTaskOwner(id, payloadType); registerErr != nil {
		return registerErr
	}
	taskVal, vmErr := vm.taskValue(id, taskType)
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

func (vm *VM) handleSleep(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicUnimplemented, "sleep missing destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicUnimplemented, "sleep expects 1 argument")
	}
	delayVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(delayVal)

	delay, vmErr := vm.uintValueToInt(delayVal, "sleep duration out of range")
	if vmErr != nil {
		return vmErr
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	id := exec.SpawnSleep(&sleepState{delayMs: uint64(delay)}) //nolint:gosec // delay is bounded by uintValueToInt
	taskType := frame.Locals[call.Dst.Local].TypeID
	payloadType, vmErr := vm.runtimeHandlePayloadType(taskType, "sleep Task")
	if vmErr != nil {
		return vmErr
	}
	if registerErr := vm.registerAsyncTaskOwner(id, payloadType); registerErr != nil {
		return registerErr
	}
	taskVal, vmErr := vm.taskValue(id, taskType)
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.writeLocal(frame, call.Dst.Local, taskVal); vmErr != nil {
		vm.dropValue(taskVal)
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

func (vm *VM) handleTimeout(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicUnimplemented, "timeout missing destination")
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicUnimplemented, "timeout expects 2 arguments")
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	if exec.Current() != 0 {
		return vm.eb.makeError(PanicUnimplemented, "timeout requires async lowering")
	}

	taskVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	ownsTask := taskVal.IsHeap()
	defer func() {
		if ownsTask {
			vm.dropValue(taskVal)
		}
	}()
	taskID, vmErr := vm.taskIDFromValue(taskVal)
	if vmErr != nil {
		return vmErr
	}

	delayVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(delayVal)
	delay, vmErr := vm.uintValueToInt(delayVal, "timeout duration out of range")
	if vmErr != nil {
		return vmErr
	}

	resultType := frame.Locals[call.Dst.Local].TypeID
	timeoutID, vmErr := vm.spawnTimeoutTask(exec, &timeoutState{
		target:     taskID,
		delayMs:    uint64(delay), //nolint:gosec // delay is bounded by uintValueToInt
		resultType: resultType,
	})
	if vmErr != nil {
		return vmErr
	}

	for {
		if vm.Halted {
			return nil
		}
		timeoutTask := exec.Task(timeoutID)
		if timeoutTask == nil {
			return vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", timeoutID))
		}
		if timeoutTask.Status == asyncrt.TaskDone {
			result, vmErr := vm.takeTimeoutOutcome(timeoutTask, resultType)
			if vmErr != nil {
				return vmErr
			}
			if vmErr := vm.writeLocal(frame, call.Dst.Local, result); vmErr != nil {
				vm.dropValue(result)
				return vmErr
			}
			if writes != nil {
				*writes = append(*writes, LocalWrite{
					LocalID: call.Dst.Local,
					Name:    frame.Locals[call.Dst.Local].Name,
					Value:   result,
				})
			}
			return nil
		}
		ran, vmErr := vm.runReadyOne()
		if vmErr != nil {
			return vmErr
		}
		if vm.Halted {
			return nil
		}
		if !ran {
			return vm.eb.makeError(PanicUnimplemented, "async deadlock")
		}
	}
}
