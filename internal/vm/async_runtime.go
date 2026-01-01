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
	value   Value
}

func (vm *VM) ensureExecutor() *asyncrt.Executor {
	if vm == nil {
		return nil
	}
	if vm.Async == nil {
		cfg := vm.AsyncConfig
		if !cfg.Fuzz && !cfg.Deterministic && cfg.Seed == 0 {
			cfg.Deterministic = true
		}
		if cfg.TimerMode == asyncrt.TimerModeReal && cfg.Clock == nil {
			cfg.Clock = &asyncrt.RealClock{NowFunc: vm.monotonicNowMs}
		}
		vm.Async = asyncrt.NewExecutor(cfg)
	}
	return vm.Async
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
			return 0, vm.eb.makeError(PanicInvalidHandle, "negative task id")
		}
		return asyncrt.TaskID(val.Int), nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok || n < 0 {
			return 0, vm.eb.makeError(PanicInvalidHandle, "task id out of range")
		}
		return asyncrt.TaskID(n), nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok || n > ^uint64(0)>>1 {
			return 0, vm.eb.makeError(PanicInvalidHandle, "task id out of range")
		}
		return asyncrt.TaskID(n), nil
	case VKHandleStruct:
		obj := vm.Heap.Get(val.H)
		if obj == nil || obj.Kind != OKStruct {
			return 0, vm.eb.typeMismatch("struct", fmt.Sprintf("%v", obj.Kind))
		}
		layout, vmErr := vm.layouts.Struct(val.TypeID)
		if vmErr != nil {
			return 0, vmErr
		}
		idx, ok := layout.IndexByName["__opaque"]
		if !ok {
			return 0, vm.eb.makeError(PanicTypeMismatch, "Task missing __opaque field")
		}
		if idx < 0 || idx >= len(obj.Fields) {
			return 0, vm.eb.makeError(PanicOutOfBounds, "Task __opaque field out of range")
		}
		return vm.taskIDFromValue(obj.Fields[idx])
	default:
		return 0, vm.eb.typeMismatch("Task", val.Kind.String())
	}
}

func (vm *VM) channelIDFromValue(val Value) (asyncrt.ChannelID, *VMError) {
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
			return 0, vm.eb.makeError(PanicInvalidHandle, "negative channel id")
		}
		return asyncrt.ChannelID(val.Int), nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok || n < 0 {
			return 0, vm.eb.makeError(PanicInvalidHandle, "channel id out of range")
		}
		return asyncrt.ChannelID(n), nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok || n > ^uint64(0)>>1 {
			return 0, vm.eb.makeError(PanicInvalidHandle, "channel id out of range")
		}
		return asyncrt.ChannelID(n), nil
	case VKHandleStruct:
		obj := vm.Heap.Get(val.H)
		if obj == nil || obj.Kind != OKStruct {
			return 0, vm.eb.typeMismatch("struct", fmt.Sprintf("%v", obj.Kind))
		}
		layout, vmErr := vm.layouts.Struct(val.TypeID)
		if vmErr != nil {
			return 0, vmErr
		}
		idx, ok := layout.IndexByName["__opaque"]
		if !ok {
			return 0, vm.eb.makeError(PanicTypeMismatch, "Channel missing __opaque field")
		}
		if idx < 0 || idx >= len(obj.Fields) {
			return 0, vm.eb.makeError(PanicOutOfBounds, "Channel __opaque field out of range")
		}
		return vm.channelIDFromValue(obj.Fields[idx])
	default:
		return 0, vm.eb.typeMismatch("Channel", val.Kind.String())
	}
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
	layout, vmErr := vm.layouts.Struct(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	fields := make([]Value, len(layout.FieldNames))
	for i := range fields {
		fields[i] = Value{Kind: VKInvalid}
	}
	idx, ok := layout.IndexByName["__opaque"]
	if !ok {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "Task missing __opaque field")
	}
	fieldType := layout.FieldTypes[idx]
	if fieldType == types.NoTypeID && vm.Types != nil {
		fieldType = vm.Types.Builtins().Int
	}
	fields[idx] = MakeInt(int64(id), fieldType) //nolint:gosec // TaskID is bounded by executor
	h := vm.Heap.AllocStruct(typeID, fields)
	return MakeHandleStruct(h, typeID), nil
}

func (vm *VM) channelValue(id asyncrt.ChannelID, typeID types.TypeID) (Value, *VMError) {
	layout, vmErr := vm.layouts.Struct(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	fields := make([]Value, len(layout.FieldNames))
	for i := range fields {
		fields[i] = Value{Kind: VKInvalid}
	}
	idx, ok := layout.IndexByName["__opaque"]
	if !ok {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "Channel missing __opaque field")
	}
	fieldType := layout.FieldTypes[idx]
	if fieldType == types.NoTypeID && vm.Types != nil {
		fieldType = vm.Types.Builtins().Int
	}
	fields[idx] = MakeInt(int64(id), fieldType) //nolint:gosec // ChannelID is bounded by executor
	h := vm.Heap.AllocStruct(typeID, fields)
	return MakeHandleStruct(h, typeID), nil
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

func (vm *VM) taskResultFromTask(task *asyncrt.Task, resultType types.TypeID) (Value, *VMError) {
	if task == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "missing task result")
	}
	switch task.ResultKind {
	case asyncrt.TaskResultSuccess:
		res, ok := task.ResultValue.(Value)
		if !ok {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "invalid task result type")
		}
		return vm.taskResultValue(resultType, asyncrt.TaskResultSuccess, res)
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
		h := vm.Heap.AllocTag(resultType, tc.TagSym, fields)
		return MakeHandleTag(h, resultType), nil
	case asyncrt.TaskResultCancelled:
		tagName = "Cancelled"
		tc, ok := layout.CaseByName(tagName)
		if !ok {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "TaskResult missing Cancelled tag")
		}
		if len(tc.PayloadTypes) != 0 {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "TaskResult Cancelled expects no payload")
		}
		h := vm.Heap.AllocTag(resultType, tc.TagSym, nil)
		return MakeHandleTag(h, resultType), nil
	default:
		return Value{}, vm.eb.makeError(PanicUnimplemented, "unknown task result kind")
	}
}

func (vm *VM) pollTask(task *asyncrt.Task) (asyncrt.PollOutcome, *VMError) {
	if vm == nil {
		return asyncrt.PollOutcome{}, nil
	}
	if task == nil {
		return asyncrt.PollOutcome{}, vm.eb.makeError(PanicUnimplemented, "missing task")
	}
	if task.Status == asyncrt.TaskDone {
		kind := asyncrt.PollDoneSuccess
		if task.ResultKind == asyncrt.TaskResultCancelled {
			kind = asyncrt.PollDoneCancelled
		}
		return asyncrt.PollOutcome{Kind: kind, Value: task.ResultValue}, nil
	}
	switch task.Kind {
	case asyncrt.TaskKindCheckpoint:
		if task.CheckpointPolled() {
			return asyncrt.PollOutcome{Kind: asyncrt.PollDoneSuccess, Value: MakeNothing()}, nil
		}
		task.MarkCheckpointPolled()
		return asyncrt.PollOutcome{Kind: asyncrt.PollYielded}, nil
	case asyncrt.TaskKindSleep:
		return vm.pollSleepTask(task)
	case asyncrt.TaskKindTimeout:
		return vm.pollTimeoutTask(task)
	default:
		outcome, stateOut, vmErr := vm.pollUserTask(task)
		if vmErr != nil {
			return asyncrt.PollOutcome{}, vmErr
		}
		if stateOut.Kind != VKInvalid {
			task.State = stateOut
		} else {
			task.State = nil
		}
		if outcome.Kind == asyncrt.PollDoneSuccess || outcome.Kind == asyncrt.PollDoneCancelled {
			vm.releaseTaskState(task)
		}
		return outcome, nil
	}
}

func (vm *VM) pollUserTask(task *asyncrt.Task) (outcome asyncrt.PollOutcome, stateOut Value, vmErr *VMError) {
	if vm == nil {
		return asyncrt.PollOutcome{}, Value{}, nil
	}
	if task == nil {
		return asyncrt.PollOutcome{}, Value{}, vm.eb.makeError(PanicUnimplemented, "missing task")
	}
	if vm.M == nil {
		return asyncrt.PollOutcome{}, Value{}, vm.eb.makeError(PanicUnimplemented, "missing module")
	}
	fn := vm.M.Funcs[mir.FuncID(task.PollFuncID)] //nolint:gosec // PollFuncID is bounded by module
	if fn == nil {
		return asyncrt.PollOutcome{}, Value{}, vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("missing poll function %d", task.PollFuncID))
	}
	outcome, stateOut, vmErr = vm.runPoll(fn)
	if vmErr != nil {
		return asyncrt.PollOutcome{}, Value{}, vmErr
	}
	return outcome, stateOut, nil
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
	switch outcome.Kind {
	case asyncrt.PollDoneSuccess:
		exec.MarkDone(id, asyncrt.TaskResultSuccess, outcome.Value)
	case asyncrt.PollDoneCancelled:
		exec.MarkDone(id, asyncrt.TaskResultCancelled, nil)
	case asyncrt.PollYielded:
		exec.Yield(id)
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
		if !ran {
			return Value{}, vm.eb.makeError(PanicUnimplemented, "async deadlock")
		}
	}
}

func (vm *VM) releaseTaskState(task *asyncrt.Task) {
	if vm == nil || task == nil {
		return
	}
	if v, ok := task.State.(Value); ok {
		vm.dropValue(v)
	}
	task.State = nil
}

func (vm *VM) runPoll(fn *mir.Func) (outcome asyncrt.PollOutcome, stateOut Value, vmErr *VMError) {
	if vm == nil {
		return asyncrt.PollOutcome{}, Value{}, nil
	}
	if fn == nil {
		return asyncrt.PollOutcome{}, Value{}, vm.eb.makeError(PanicUnimplemented, "missing poll function")
	}
	savedStack := vm.Stack
	savedHalted := vm.Halted
	savedStarted := vm.started
	savedCapture := vm.captureReturn
	savedAsync := vm.asyncCapture
	savedPendingParkKey := vm.asyncPendingParkKey

	exit := asyncExit{}
	vm.asyncCapture = &exit
	vm.asyncPendingParkKey = asyncrt.WakerKey{}
	vm.captureReturn = nil
	vm.Stack = nil
	vm.Halted = false
	vm.started = true

	frame := NewFrame(fn)
	vm.Stack = append(vm.Stack, *frame)

	for len(vm.Stack) > 0 && !vm.Halted {
		if vmErr := vm.Step(); vmErr != nil {
			vm.Stack = savedStack
			vm.Halted = savedHalted
			vm.started = savedStarted
			vm.captureReturn = savedCapture
			vm.asyncCapture = savedAsync
			vm.asyncPendingParkKey = savedPendingParkKey
			return asyncrt.PollOutcome{}, Value{}, vmErr
		}
		if exit.set {
			break
		}
	}

	vm.Stack = savedStack
	vm.Halted = savedHalted
	vm.started = savedStarted
	vm.captureReturn = savedCapture
	vm.asyncCapture = savedAsync
	vm.asyncPendingParkKey = savedPendingParkKey

	if !exit.set {
		return asyncrt.PollOutcome{}, Value{}, vm.eb.makeError(PanicUnimplemented, "poll function exited without async terminator")
	}

	outcome = asyncrt.PollOutcome{Kind: exit.kind, Value: exit.value, ParkKey: exit.parkKey}
	return outcome, exit.state, nil
}
