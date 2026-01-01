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
	taskVal, vmErr := vm.taskValue(id, frame.Locals[call.Dst.Local].TypeID)
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
	timeoutID := exec.SpawnTimeout(&timeoutState{
		target:     taskID,
		delayMs:    uint64(delay), //nolint:gosec // delay is bounded by uintValueToInt
		resultType: resultType,
	})

	for {
		timeoutTask := exec.Task(timeoutID)
		if timeoutTask == nil {
			return vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", timeoutID))
		}
		if timeoutTask.Status == asyncrt.TaskDone {
			var result Value
			switch timeoutTask.ResultKind {
			case asyncrt.TaskResultSuccess:
				val, ok := timeoutTask.ResultValue.(Value)
				if !ok {
					return vm.eb.makeError(PanicTypeMismatch, "invalid timeout result type")
				}
				result, vmErr = vm.cloneForShare(val)
				if vmErr != nil {
					return vmErr
				}
			case asyncrt.TaskResultCancelled:
				result, vmErr = vm.taskResultValue(resultType, asyncrt.TaskResultCancelled, Value{})
				if vmErr != nil {
					return vmErr
				}
			default:
				return vm.eb.makeError(PanicUnimplemented, "unknown task result kind")
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
		if !ran {
			return vm.eb.makeError(PanicUnimplemented, "async deadlock")
		}
	}
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

func (vm *VM) handleScopeEnter(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicUnimplemented, "rt_scope_enter missing destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicUnimplemented, "rt_scope_enter expects 1 argument")
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
	if arg.Kind != VKBool {
		return vm.eb.typeMismatch("bool", arg.Kind.String())
	}
	failfast := arg.Bool
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	owner := exec.Current()
	if owner == 0 {
		return vm.eb.makeError(PanicUnimplemented, "rt_scope_enter without current task")
	}
	scopeID := exec.EnterScope(owner, failfast)
	dstLocal := call.Dst.Local
	typeID := frame.Locals[dstLocal].TypeID
	scopeVal := MakeInt(int64(scopeID), typeID) //nolint:gosec // ScopeID is bounded by executor
	if vmErr := vm.writeLocal(frame, dstLocal, scopeVal); vmErr != nil {
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   scopeVal,
		})
	}
	return nil
}

func (vm *VM) handleScopeRegisterChild(frame *Frame, call *mir.CallInstr) *VMError {
	if call == nil {
		return vm.eb.makeError(PanicTypeMismatch, "rt_scope_register_child requires a call")
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_scope_register_child expects 2 arguments")
	}
	scopeVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	ownsScope := scopeVal.IsHeap()
	defer func() {
		if ownsScope {
			vm.dropValue(scopeVal)
		}
	}()
	scopeID, vmErr := vm.scopeIDFromValue(scopeVal)
	if vmErr != nil {
		return vmErr
	}
	taskVal, vmErr := vm.evalOperand(frame, &call.Args[1])
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
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	exec.RegisterChild(scopeID, taskID)
	return nil
}

func (vm *VM) handleScopeCancelAll(frame *Frame, call *mir.CallInstr) *VMError {
	if call == nil {
		return vm.eb.makeError(PanicTypeMismatch, "rt_scope_cancel_all requires a call")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_scope_cancel_all expects 1 argument")
	}
	scopeVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	ownsScope := scopeVal.IsHeap()
	defer func() {
		if ownsScope {
			vm.dropValue(scopeVal)
		}
	}()
	scopeID, vmErr := vm.scopeIDFromValue(scopeVal)
	if vmErr != nil {
		return vmErr
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	exec.CancelAllChildren(scopeID)
	return nil
}

func (vm *VM) handleScopeJoinAll(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicUnimplemented, "rt_scope_join_all missing destination")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicUnimplemented, "rt_scope_join_all expects 1 argument")
	}
	scopeVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	ownsScope := scopeVal.IsHeap()
	defer func() {
		if ownsScope {
			vm.dropValue(scopeVal)
		}
	}()
	scopeID, vmErr := vm.scopeIDFromValue(scopeVal)
	if vmErr != nil {
		return vmErr
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	done, pending, failfast := exec.JoinAllChildrenBlocking(scopeID)
	if !done {
		vm.asyncPendingParkKey = asyncrt.JoinKey(pending)
		failfast = false
	}
	dstLocal := call.Dst.Local
	typeID := frame.Locals[dstLocal].TypeID
	resVal := MakeBool(failfast, typeID)
	if vmErr := vm.writeLocal(frame, dstLocal, resVal); vmErr != nil {
		return vmErr
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

func (vm *VM) handleScopeExit(frame *Frame, call *mir.CallInstr) *VMError {
	if call == nil {
		return vm.eb.makeError(PanicTypeMismatch, "rt_scope_exit requires a call")
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_scope_exit expects 1 argument")
	}
	scopeVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	ownsScope := scopeVal.IsHeap()
	defer func() {
		if ownsScope {
			vm.dropValue(scopeVal)
		}
	}()
	scopeID, vmErr := vm.scopeIDFromValue(scopeVal)
	if vmErr != nil {
		return vmErr
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	exec.ExitScope(scopeID)
	return nil
}
