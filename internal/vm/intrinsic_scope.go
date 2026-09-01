package vm

import (
	"surge/internal/asyncrt"
	"surge/internal/mir"
)

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
