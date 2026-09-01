package vm

import (
	"fmt"

	"surge/internal/asyncrt"
	"surge/internal/mir"
	"surge/internal/types"
)

func (vm *VM) pollTask(task *asyncrt.Task[asyncPayload]) (asyncrt.PollOutcome[asyncPayload], *VMError) {
	if vm == nil {
		return asyncrt.PollOutcome[asyncPayload]{}, nil
	}
	if task == nil {
		return asyncrt.PollOutcome[asyncPayload]{}, vm.eb.makeError(PanicUnimplemented, "missing task")
	}
	if task.Status == asyncrt.TaskDone {
		kind := asyncrt.PollDoneSuccess
		if task.ResultKind == asyncrt.TaskResultCancelled {
			kind = asyncrt.PollDoneCancelled
		}
		return asyncrt.PollOutcome[asyncPayload]{Kind: kind, Value: task.ResultValue}, nil
	}
	switch task.Kind {
	case asyncrt.TaskKindCheckpoint:
		if task.Cancelled {
			return asyncrt.PollOutcome[asyncPayload]{Kind: asyncrt.PollDoneCancelled}, nil
		}
		if task.CheckpointPolled() {
			payload, vmErr := vm.stageAsyncTaskResult(task.ID, MakeNothing())
			if vmErr != nil {
				return asyncrt.PollOutcome[asyncPayload]{}, vmErr
			}
			return asyncrt.PollOutcome[asyncPayload]{Kind: asyncrt.PollDoneSuccess, Value: payload}, nil
		}
		task.MarkCheckpointPolled()
		return asyncrt.PollOutcome[asyncPayload]{Kind: asyncrt.PollYielded}, nil
	case asyncrt.TaskKindSleep:
		return vm.pollSleepTask(task)
	case asyncrt.TaskKindTimeout:
		return vm.pollTimeoutTask(task)
	default:
		outcome, vmErr := vm.pollUserTask(task)
		if vmErr != nil {
			return asyncrt.PollOutcome[asyncPayload]{}, vmErr
		}
		if outcome.Kind == asyncrt.PollDoneSuccess || outcome.Kind == asyncrt.PollDoneCancelled {
			vm.releaseTaskState(task)
		}
		return outcome, nil
	}
}

func (vm *VM) pollUserTask(task *asyncrt.Task[asyncPayload]) (outcome asyncrt.PollOutcome[asyncPayload], vmErr *VMError) {
	if vm == nil {
		return asyncrt.PollOutcome[asyncPayload]{}, nil
	}
	if task == nil {
		return asyncrt.PollOutcome[asyncPayload]{}, vm.eb.makeError(PanicUnimplemented, "missing task")
	}
	if vm.M == nil {
		return asyncrt.PollOutcome[asyncPayload]{}, vm.eb.makeError(PanicUnimplemented, "missing module")
	}
	fn := vm.M.Funcs[mir.FuncID(task.PollFuncID)] //nolint:gosec // PollFuncID is bounded by module
	if fn == nil {
		return asyncrt.PollOutcome[asyncPayload]{}, vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("missing poll function %d", task.PollFuncID))
	}
	state := vm.ensureUserTaskState(task)
	outcome, stateOut, statePins, vmErr := vm.runPoll(fn)
	if vmErr != nil {
		return asyncrt.PollOutcome[asyncPayload]{}, vmErr
	}
	if stateOut.Kind == VKInvalid {
		stateOut = Value{}
	}
	vm.setUserTaskStateWithPins(state, stateOut, statePins)
	task.State = state
	return outcome, nil
}

func (vm *VM) runReadyOne() (bool, *VMError) {
	exec := vm.ensureExecutor()
	if exec == nil {
		return false, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	id, ok := exec.NextReady()
	if !ok {
		return false, nil
	}
	task := exec.Task(id)
	if task == nil {
		return true, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", id))
	}
	exec.SetCurrent(id)
	task.Status = asyncrt.TaskRunning
	outcome, vmErr := vm.pollTask(task)
	if vmErr != nil {
		exec.SetCurrent(0)
		return true, vmErr
	}
	if vm.Halted {
		exec.SetCurrent(0)
		return true, nil
	}
	switch outcome.Kind {
	case asyncrt.PollDoneSuccess:
		// RV2-DEBT-263: the kind belongs to the commit, not to the terminator.
		// A task cancelled after its last suspension point arrives here
		// carrying Success -- execTermAsyncReturn publishes PollDoneSuccess
		// unconditionally and pollTask's DONE fast path answers from the
		// TARGET, so the body never had a suspension left to see the cancel at.
		// A value the commit refuses comes back HERE, because the executor is
		// generic over its payload and only this lane can destroy one. It is
		// destroyed at the commit, which is where the native runtime destroys
		// it too (rt_task_result_refuse) -- the two lanes agree on the moment,
		// not just on the answer.
		if refused, ok := exec.MarkDone(id, asyncrt.TaskResultSuccess, outcome.Value); ok {
			vm.dropAsyncPayload(refused)
		}
		// Completion is the second moment a result can become unclaimable: the
		// cohort may already have emptied while the task was still running.
		vm.taskCompleted(id)
	case asyncrt.PollDoneCancelled:
		exec.MarkDone(id, asyncrt.TaskResultCancelled, asyncPayload{})
		vm.taskCompleted(id)
	case asyncrt.PollYielded:
		exec.Yield(id)
		exec.TickVirtual()
	case asyncrt.PollParked:
		if !outcome.ParkKey.IsValid() {
			exec.SetCurrent(0)
			return true, vm.eb.makeError(PanicUnimplemented, "async park missing key")
		}
		exec.ParkCurrent(outcome.ParkKey)
	default:
		exec.SetCurrent(0)
		return true, vm.eb.makeError(PanicUnimplemented, "unknown poll outcome")
	}
	exec.SetCurrent(0)
	return true, nil
}

func (vm *VM) runUntilDone(id asyncrt.TaskID, resultType types.TypeID) (Value, *VMError) {
	exec := vm.ensureExecutor()
	if exec == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	if task := exec.Task(id); task == nil {
		return Value{}, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", id))
	} else if task.Status != asyncrt.TaskWaiting {
		exec.Wake(id)
	}
	for {
		if vm.Halted {
			return Value{}, nil
		}
		task := exec.Task(id)
		if task == nil {
			return Value{}, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", id))
		}
		if task.Status == asyncrt.TaskDone {
			return vm.taskResultFromTask(task, resultType)
		}
		ran, vmErr := vm.runReadyOne()
		if vmErr != nil {
			return Value{}, vmErr
		}
		if vm.Halted {
			return Value{}, nil
		}
		if !ran {
			return Value{}, vm.eb.makeError(PanicUnimplemented, "async deadlock")
		}
	}
}

func (vm *VM) releaseTaskState(task *asyncrt.Task[asyncPayload]) {
	if vm == nil || task == nil {
		return
	}
	if state, ok := task.State.(*userTaskState); ok && state != nil {
		if state.state.Kind != VKInvalid {
			vm.dropValue(state.state)
		}
		vm.releaseTaskStatePins(state.pins)
		state.state = Value{}
		state.pins = taskStatePins{}
		task.State = nil
		return
	}
	if v, ok := task.State.(Value); ok {
		vm.dropValue(v)
	}
	task.State = nil
}

func (vm *VM) runPoll(fn *mir.Func) (outcome asyncrt.PollOutcome[asyncPayload], stateOut Value, statePins taskStatePins, vmErr *VMError) {
	if vm == nil {
		return asyncrt.PollOutcome[asyncPayload]{}, Value{}, taskStatePins{}, nil
	}
	if fn == nil {
		return asyncrt.PollOutcome[asyncPayload]{}, Value{}, taskStatePins{}, vm.eb.makeError(PanicUnimplemented, "missing poll function")
	}
	savedStack := vm.Stack
	savedHalted := vm.Halted
	savedStarted := vm.started
	savedCapture := vm.captureReturn
	savedAsync := vm.asyncCapture
	savedPendingParkKey := vm.asyncPendingParkKey
	savedDeferredShutdown := vm.deferredShutdown

	exit := asyncExit{}
	vm.pollDepth++
	defer func() {
		vm.pollDepth--
	}()
	vm.asyncCapture = &exit
	vm.asyncPendingParkKey = asyncrt.WakerKey{}
	vm.captureReturn = nil
	vm.deferredShutdown = shutdownState{}
	vm.Halted = false
	vm.started = true

	frame := vm.activate(fn)
	vm.Stack = []*Frame{frame}

	for len(vm.Stack) > 0 && !vm.Halted {
		if vmErr := vm.Step(); vmErr != nil {
			vm.Stack = savedStack
			vm.Halted = savedHalted
			vm.started = savedStarted
			vm.captureReturn = savedCapture
			vm.asyncCapture = savedAsync
			vm.asyncPendingParkKey = savedPendingParkKey
			vm.deferredShutdown = savedDeferredShutdown
			return asyncrt.PollOutcome[asyncPayload]{}, Value{}, taskStatePins{}, vmErr
		}
		if exit.set {
			break
		}
	}

	deferredShutdown := vm.deferredShutdown
	vm.Stack = savedStack
	vm.Halted = savedHalted
	vm.started = savedStarted
	vm.captureReturn = savedCapture
	vm.asyncCapture = savedAsync
	vm.asyncPendingParkKey = savedPendingParkKey
	vm.deferredShutdown = savedDeferredShutdown

	if deferredShutdown.active {
		checkLeaks := deferredShutdown.checkLeaks || savedDeferredShutdown.checkLeaks
		if savedAsync != nil {
			vm.Halted = true
			vm.deferredShutdown.active = true
			vm.deferredShutdown.checkLeaks = checkLeaks
			return asyncrt.PollOutcome[asyncPayload]{}, Value{}, taskStatePins{}, nil
		}
		vm.finishShutdown(checkLeaks)
		return asyncrt.PollOutcome[asyncPayload]{}, Value{}, taskStatePins{}, nil
	}

	if !exit.set {
		return asyncrt.PollOutcome[asyncPayload]{}, Value{}, taskStatePins{}, vm.eb.makeError(PanicUnimplemented, "poll function exited without async terminator")
	}

	outcome = asyncrt.PollOutcome[asyncPayload]{Kind: exit.kind, Value: exit.value, ParkKey: exit.parkKey}
	return outcome, exit.state, exit.pins, nil
}
