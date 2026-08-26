package vm

import (
	"fmt"

	"surge/internal/asyncrt"
	"surge/internal/mir"
	"surge/internal/types"
)

type asyncExit struct {
	set     bool
	kind    asyncrt.PollOutcomeKind
	parkKey asyncrt.WakerKey
	state   Value
	pins    taskStatePins
	value   Value
}

type userTaskState struct {
	state Value
	pins  taskStatePins
}

func (vm *VM) ensureExecutor() *asyncrt.Executor[Value] {
	if vm == nil {
		return nil
	}
	if vm.Async == nil {
		cfg := vm.AsyncConfig
		if !cfg.Fuzz && !cfg.Deterministic && cfg.Seed == 0 {
			cfg.Deterministic = true
		}
		if cfg.TimerMode == asyncrt.TimerModeReal && cfg.Clock == nil {
			cfg.Clock = &asyncrt.RealClock[Value]{NowFunc: vm.monotonicNowMs}
		}
		vm.Async = asyncrt.NewExecutor[Value](cfg)
	}
	return vm.Async
}

func (vm *VM) ensureUserTaskState(task *asyncrt.Task[Value]) *userTaskState {
	if task == nil {
		return nil
	}
	if state, ok := task.State.(*userTaskState); ok && state != nil {
		return state
	}
	state := &userTaskState{}
	if val, ok := task.State.(Value); ok {
		if vmErr := vm.setUserTaskState(state, val); vmErr != nil {
			vm.panic(vmErr.Code, vmErr.Message)
		}
	}
	task.State = state
	return state
}

func (vm *VM) currentTaskCancelled() bool {
	if vm == nil {
		return false
	}
	exec := vm.ensureExecutor()
	if exec == nil {
		return false
	}
	id := exec.Current()
	if id == 0 {
		return false
	}
	task := exec.Task(id)
	if task == nil {
		return false
	}
	return task.Cancelled
}

func (vm *VM) taskIDFromValue(val Value) (asyncrt.TaskID, *VMError) {
	word, vmErr := vm.resourceWord(val, "Task", "task id")
	if vmErr != nil {
		return 0, vmErr
	}
	if word < 0 {
		return 0, vm.eb.makeError(PanicInvalidHandle, "negative task id")
	}
	return asyncrt.TaskID(word), nil
}

func (vm *VM) channelIDFromValue(val Value) (asyncrt.ChannelID, *VMError) {
	word, vmErr := vm.resourceWord(val, "Channel", "channel id")
	if vmErr != nil {
		return 0, vmErr
	}
	if word < 0 {
		return 0, vm.eb.makeError(PanicInvalidHandle, "negative channel id")
	}
	return asyncrt.ChannelID(word), nil
}

func (vm *VM) scopeIDFromValue(val Value) (asyncrt.ScopeID, *VMError) {
	if val.Kind == VKRef || val.Kind == VKRefMut {
		loaded, vmErr := vm.loadLocationRaw(val.Loc)
		if vmErr != nil {
			return 0, vmErr
		}
		val = loaded
	}
	switch val.Kind {
	case VKInt:
		if val.Int < 0 {
			return 0, vm.eb.makeError(PanicInvalidHandle, "negative scope id")
		}
		return asyncrt.ScopeID(val.Int), nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok || n < 0 {
			return 0, vm.eb.makeError(PanicInvalidHandle, "scope id out of range")
		}
		return asyncrt.ScopeID(n), nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok || n > ^uint64(0)>>1 {
			return 0, vm.eb.makeError(PanicInvalidHandle, "scope id out of range")
		}
		return asyncrt.ScopeID(n), nil
	default:
		return 0, vm.eb.typeMismatch("scope id", val.Kind.String())
	}
}

func (vm *VM) int64FromValue(val Value, context string) (int64, *VMError) {
	switch val.Kind {
	case VKInt:
		return val.Int, nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok {
			if context == "" {
				context = "int value out of range"
			}
			return 0, vm.eb.makeError(PanicInvalidNumericConversion, context)
		}
		return n, nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok || n > ^uint64(0)>>1 {
			if context == "" {
				context = "int value out of range"
			}
			return 0, vm.eb.makeError(PanicInvalidNumericConversion, context)
		}
		return int64(n), nil
	default:
		return 0, vm.eb.typeMismatch("int", val.Kind.String())
	}
}

func (vm *VM) taskValue(id asyncrt.TaskID, typeID types.TypeID) (Value, *VMError) {
	val, vmErr := vm.resourceValue(int64(id), typeID, "Task") //nolint:gosec // TaskID is bounded by the executor
	if vmErr != nil {
		return Value{}, vmErr
	}
	// One handed-out task word is one entitlement to that task's result.
	vm.taskHandleCreated(id)
	return val, nil
}

func (vm *VM) channelValue(id asyncrt.ChannelID, typeID types.TypeID) (Value, *VMError) {
	return vm.resourceValue(int64(id), typeID, "Channel") //nolint:gosec // ChannelID is bounded by the executor
}

func (vm *VM) isTaskType(typeID types.TypeID) bool {
	if vm == nil || vm.Types == nil || typeID == types.NoTypeID {
		return false
	}
	typeID = resolveAlias(vm.Types, typeID)
	tt, ok := vm.Types.Lookup(typeID)
	if !ok || tt.Kind != types.KindStruct {
		return false
	}
	info, ok := vm.Types.StructInfo(typeID)
	if !ok || info == nil || vm.Types.Strings == nil {
		return false
	}
	name, ok := vm.Types.Strings.Lookup(info.Name)
	return ok && name == "Task"
}

func (vm *VM) isChannelType(typeID types.TypeID) bool {
	if vm == nil || vm.Types == nil || typeID == types.NoTypeID {
		return false
	}
	typeID = vm.valueType(typeID)
	tt, ok := vm.Types.Lookup(typeID)
	if !ok || tt.Kind != types.KindStruct {
		if info, aliasOK := vm.Types.AliasInfo(typeID); aliasOK && info != nil && vm.Types.Strings != nil {
			name, nameOK := vm.Types.Strings.Lookup(info.Name)
			return nameOK && name == "Channel"
		}
		return false
	}
	info, ok := vm.Types.StructInfo(typeID)
	if !ok || info == nil || vm.Types.Strings == nil {
		return false
	}
	name, ok := vm.Types.Strings.Lookup(info.Name)
	return ok && name == "Channel"
}

func (vm *VM) taskResultFromTask(task *asyncrt.Task[Value], resultType types.TypeID) (Value, *VMError) {
	if task == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "missing task result")
	}
	switch task.ResultKind {
	case asyncrt.TaskResultSuccess:
		return vm.taskResultValue(resultType, asyncrt.TaskResultSuccess, task.ResultValue)
	case asyncrt.TaskResultCancelled:
		return vm.taskResultValue(resultType, asyncrt.TaskResultCancelled, Value{})
	default:
		return Value{}, vm.eb.makeError(PanicUnimplemented, "unknown task result kind")
	}
}

func (vm *VM) taskResultValue(resultType types.TypeID, kind asyncrt.TaskResultKind, value Value) (Value, *VMError) {
	layout, vmErr := vm.tagLayoutFor(resultType)
	if vmErr != nil {
		return Value{}, vmErr
	}
	var (
		tagName string
		fields  []Value
	)
	switch kind {
	case asyncrt.TaskResultSuccess:
		tagName = "Success"
		tc, ok := layout.CaseByName(tagName)
		if !ok {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "TaskResult missing Success tag")
		}
		if len(tc.PayloadTypes) != 1 {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "TaskResult Success expects payload")
		}
		payload, vmErr := vm.cloneForShare(value)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if tc.PayloadTypes[0] != types.NoTypeID {
			payload.TypeID = tc.PayloadTypes[0]
		}
		fields = []Value{payload}
		return vm.buildTag(vm.currentFrame(), resultType, tc.TagSym, fields)
	case asyncrt.TaskResultCancelled:
		tagName = "Cancelled"
		tc, ok := layout.CaseByName(tagName)
		if !ok {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "TaskResult missing Cancelled tag")
		}
		if len(tc.PayloadTypes) != 0 {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "TaskResult Cancelled expects no payload")
		}
		return vm.buildTag(vm.currentFrame(), resultType, tc.TagSym, nil)
	default:
		return Value{}, vm.eb.makeError(PanicUnimplemented, "unknown task result kind")
	}
}

func (vm *VM) pollTask(task *asyncrt.Task[Value]) (asyncrt.PollOutcome[Value], *VMError) {
	if vm == nil {
		return asyncrt.PollOutcome[Value]{}, nil
	}
	if task == nil {
		return asyncrt.PollOutcome[Value]{}, vm.eb.makeError(PanicUnimplemented, "missing task")
	}
	if task.Status == asyncrt.TaskDone {
		kind := asyncrt.PollDoneSuccess
		if task.ResultKind == asyncrt.TaskResultCancelled {
			kind = asyncrt.PollDoneCancelled
		}
		return asyncrt.PollOutcome[Value]{Kind: kind, Value: task.ResultValue}, nil
	}
	switch task.Kind {
	case asyncrt.TaskKindCheckpoint:
		if task.Cancelled {
			return asyncrt.PollOutcome[Value]{Kind: asyncrt.PollDoneCancelled}, nil
		}
		if task.CheckpointPolled() {
			return asyncrt.PollOutcome[Value]{Kind: asyncrt.PollDoneSuccess, Value: MakeNothing()}, nil
		}
		task.MarkCheckpointPolled()
		return asyncrt.PollOutcome[Value]{Kind: asyncrt.PollYielded}, nil
	case asyncrt.TaskKindSleep:
		return vm.pollSleepTask(task)
	case asyncrt.TaskKindTimeout:
		return vm.pollTimeoutTask(task)
	default:
		outcome, vmErr := vm.pollUserTask(task)
		if vmErr != nil {
			return asyncrt.PollOutcome[Value]{}, vmErr
		}
		if outcome.Kind == asyncrt.PollDoneSuccess || outcome.Kind == asyncrt.PollDoneCancelled {
			vm.releaseTaskState(task)
		}
		return outcome, nil
	}
}

func (vm *VM) pollUserTask(task *asyncrt.Task[Value]) (outcome asyncrt.PollOutcome[Value], vmErr *VMError) {
	if vm == nil {
		return asyncrt.PollOutcome[Value]{}, nil
	}
	if task == nil {
		return asyncrt.PollOutcome[Value]{}, vm.eb.makeError(PanicUnimplemented, "missing task")
	}
	if vm.M == nil {
		return asyncrt.PollOutcome[Value]{}, vm.eb.makeError(PanicUnimplemented, "missing module")
	}
	fn := vm.M.Funcs[mir.FuncID(task.PollFuncID)] //nolint:gosec // PollFuncID is bounded by module
	if fn == nil {
		return asyncrt.PollOutcome[Value]{}, vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("missing poll function %d", task.PollFuncID))
	}
	state := vm.ensureUserTaskState(task)
	outcome, stateOut, statePins, vmErr := vm.runPoll(fn)
	if vmErr != nil {
		return asyncrt.PollOutcome[Value]{}, vmErr
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
		exec.MarkDone(id, asyncrt.TaskResultSuccess, outcome.Value)
		// Completion is the second moment a result can become unclaimable: the
		// cohort may already have emptied while the task was still running.
		vm.taskCompleted(id)
	case asyncrt.PollDoneCancelled:
		exec.MarkDone(id, asyncrt.TaskResultCancelled, Value{})
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

func (vm *VM) releaseTaskState(task *asyncrt.Task[Value]) {
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

func (vm *VM) runPoll(fn *mir.Func) (outcome asyncrt.PollOutcome[Value], stateOut Value, statePins taskStatePins, vmErr *VMError) {
	if vm == nil {
		return asyncrt.PollOutcome[Value]{}, Value{}, taskStatePins{}, nil
	}
	if fn == nil {
		return asyncrt.PollOutcome[Value]{}, Value{}, taskStatePins{}, vm.eb.makeError(PanicUnimplemented, "missing poll function")
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
			return asyncrt.PollOutcome[Value]{}, Value{}, taskStatePins{}, vmErr
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
			return asyncrt.PollOutcome[Value]{}, Value{}, taskStatePins{}, nil
		}
		vm.finishShutdown(checkLeaks)
		return asyncrt.PollOutcome[Value]{}, Value{}, taskStatePins{}, nil
	}

	if !exit.set {
		return asyncrt.PollOutcome[Value]{}, Value{}, taskStatePins{}, vm.eb.makeError(PanicUnimplemented, "poll function exited without async terminator")
	}

	outcome = asyncrt.PollOutcome[Value]{Kind: exit.kind, Value: exit.value, ParkKey: exit.parkKey}
	return outcome, exit.state, exit.pins, nil
}
