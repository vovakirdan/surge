package vm

import (
	"surge/internal/asyncrt"
	"surge/internal/types"
)

func (vm *VM) taskResultFromTask(task *asyncrt.Task[asyncPayload], resultType types.TypeID) (Value, *VMError) {
	if task == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "missing task result")
	}
	switch task.ResultKind {
	case asyncrt.TaskResultSuccess:
		clone := vm.taskHasOtherAskers(task.ID)
		result, vmErr := vm.makeTaskResultSuccessFromAsync(resultType, task.ResultValue, clone)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if !clone {
			task.ResultValue = asyncPayload{}
		}
		return result, nil
	case asyncrt.TaskResultCancelled:
		return vm.taskResultCancelledValue(resultType)
	default:
		return Value{}, vm.eb.makeError(PanicUnimplemented, "unknown task result kind")
	}
}

func (vm *VM) taskResultCancelledValue(resultType types.TypeID) (Value, *VMError) {
	layout, vmErr := vm.tagLayoutFor(resultType)
	if vmErr != nil {
		return Value{}, vmErr
	}
	tc, ok := layout.CaseByName("Cancelled")
	if !ok {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "TaskResult missing Cancelled tag")
	}
	if len(tc.PayloadTypes) != 0 {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "TaskResult Cancelled expects no payload")
	}
	return vm.buildTag(vm.currentFrame(), resultType, tc.TagSym, nil)
}

// spawnTimeoutTask starts the runtime task a `timeout(t, ms)` operation drives,
// and records the claim that operation holds on its result.
//
// The claim is what makes the task's result owned rather than abandoned. A
// timeout task is spawned by the runtime and named by nothing in source, so no
// handle is ever created for it and no handle is ever freed for it; without a
// claim its cohort reads empty from the moment it exists, and completion
// therefore releases the result out from under the operation that asked for it.
// Registering a handle instead was tried in RV2-DEBT-167's lane and detonated
// on the spot, because a handle also has to be given up by something and
// nothing owns this one.
func (vm *VM) spawnTimeoutTask(
	exec *asyncrt.Executor[asyncPayload],
	state *timeoutState,
) (asyncrt.TaskID, *VMError) {
	id := exec.SpawnTimeout(state)
	if vmErr := vm.registerAsyncTaskOwner(id, state.resultType); vmErr != nil {
		return 0, vmErr
	}
	vm.taskClaimTaken(id)
	return id, nil
}

// takeTimeoutOutcome takes a completed timeout task's result and retires the
// claim in the same step.
//
// The order is the model's: the value is taken FIRST and the claim retired
// afterwards, so the canonical result is pinned for exactly as long as somebody
// is reading it. Retiring first would release the value this call is about to
// duplicate.
func (vm *VM) takeTimeoutOutcome(task *asyncrt.Task[asyncPayload], resultType types.TypeID) (Value, *VMError) {
	if task == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "missing timeout task")
	}
	defer vm.taskClaimRetired(task.ID)
	switch task.ResultKind {
	case asyncrt.TaskResultSuccess:
		destination, vmErr := vm.buildComposite(vm.currentFrame(), resultType)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if vmErr := vm.moveAsyncPayloadIntoStorage(task.ResultValue, destination); vmErr != nil {
			if releaseErr := vm.releaseTemporary(vm.currentFrame(), destination); releaseErr != nil {
				return Value{}, releaseErr
			}
			return Value{}, vmErr
		}
		task.ResultValue = asyncPayload{}
		return MakeComposite(destination), nil
	case asyncrt.TaskResultCancelled:
		return vm.taskResultCancelledValue(resultType)
	default:
		return Value{}, vm.eb.makeError(PanicUnimplemented, "unknown task result kind")
	}
}
