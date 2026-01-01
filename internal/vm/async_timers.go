package vm

import (
	"surge/internal/asyncrt"
	"surge/internal/types"
)

type sleepState struct {
	delayMs uint64
	timerID asyncrt.TimerID
	armed   bool
}

type timeoutState struct {
	target     asyncrt.TaskID
	delayMs    uint64
	timerID    asyncrt.TimerID
	armed      bool
	resultType types.TypeID
}

func (vm *VM) pollSleepTask(task *asyncrt.Task) (asyncrt.PollOutcome, *VMError) {
	if vm == nil || task == nil {
		return asyncrt.PollOutcome{}, vm.eb.makeError(PanicUnimplemented, "missing sleep task")
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return asyncrt.PollOutcome{}, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	state, ok := task.State.(*sleepState)
	if !ok || state == nil {
		return asyncrt.PollOutcome{}, vm.eb.makeError(PanicUnimplemented, "sleep state missing")
	}
	if task.Cancelled {
		if state.armed && exec.TimerActive(state.timerID) {
			exec.TimerCancel(state.timerID)
		}
		return asyncrt.PollOutcome{Kind: asyncrt.PollDoneCancelled}, nil
	}
	if !state.armed {
		state.timerID = exec.TimerScheduleAfter(task.ID, state.delayMs)
		state.armed = true
		return asyncrt.PollOutcome{Kind: asyncrt.PollParked, ParkKey: asyncrt.TimerKey(state.timerID)}, nil
	}
	if exec.TimerActive(state.timerID) {
		return asyncrt.PollOutcome{Kind: asyncrt.PollParked, ParkKey: asyncrt.TimerKey(state.timerID)}, nil
	}
	return asyncrt.PollOutcome{Kind: asyncrt.PollDoneSuccess, Value: MakeNothing()}, nil
}

func (vm *VM) pollTimeoutTask(task *asyncrt.Task) (asyncrt.PollOutcome, *VMError) {
	if vm == nil || task == nil {
		return asyncrt.PollOutcome{}, vm.eb.makeError(PanicUnimplemented, "missing timeout task")
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return asyncrt.PollOutcome{}, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	state, ok := task.State.(*timeoutState)
	if !ok || state == nil {
		return asyncrt.PollOutcome{}, vm.eb.makeError(PanicUnimplemented, "timeout state missing")
	}
	if task.Cancelled {
		if state.armed && exec.TimerActive(state.timerID) {
			exec.TimerCancel(state.timerID)
		}
		return asyncrt.PollOutcome{Kind: asyncrt.PollDoneCancelled}, nil
	}

	target := exec.Task(state.target)
	if target == nil {
		return asyncrt.PollOutcome{}, vm.eb.makeError(PanicInvalidHandle, "timeout target missing")
	}
	if target.Status != asyncrt.TaskWaiting && target.Status != asyncrt.TaskDone {
		exec.Wake(state.target)
	}
	if target.Status == asyncrt.TaskDone {
		if state.armed && exec.TimerActive(state.timerID) {
			exec.TimerCancel(state.timerID)
		}
		result, vmErr := vm.taskResultFromTask(target, state.resultType)
		if vmErr != nil {
			return asyncrt.PollOutcome{}, vmErr
		}
		return asyncrt.PollOutcome{Kind: asyncrt.PollDoneSuccess, Value: result}, nil
	}

	if !state.armed {
		state.timerID = exec.TimerScheduleAfter(task.ID, state.delayMs)
		state.armed = true
		return asyncrt.PollOutcome{Kind: asyncrt.PollParked, ParkKey: asyncrt.JoinKey(state.target)}, nil
	}
	if exec.TimerActive(state.timerID) {
		return asyncrt.PollOutcome{Kind: asyncrt.PollParked, ParkKey: asyncrt.JoinKey(state.target)}, nil
	}

	exec.Cancel(state.target)
	exec.Wake(state.target)
	result, vmErr := vm.taskResultValue(state.resultType, asyncrt.TaskResultCancelled, Value{})
	if vmErr != nil {
		return asyncrt.PollOutcome{}, vmErr
	}
	return asyncrt.PollOutcome{Kind: asyncrt.PollDoneSuccess, Value: result}, nil
}
