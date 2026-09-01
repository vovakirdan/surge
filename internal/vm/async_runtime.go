package vm

import (
	"surge/internal/asyncrt"
	"surge/internal/types"
)

type asyncExit struct {
	set     bool
	kind    asyncrt.PollOutcomeKind
	parkKey asyncrt.WakerKey
	state   Value
	pins    taskStatePins
	value   asyncPayload
}

type userTaskState struct {
	state Value
	pins  taskStatePins
}

func (vm *VM) ensureExecutor() *asyncrt.Executor[asyncPayload] {
	if vm == nil {
		return nil
	}
	if vm.Async == nil {
		cfg := vm.AsyncConfig
		if !cfg.Fuzz && !cfg.Deterministic && cfg.Seed == 0 {
			cfg.Deterministic = true
		}
		if cfg.TimerMode == asyncrt.TimerModeReal && cfg.Clock == nil {
			cfg.Clock = &asyncrt.RealClock[asyncPayload]{NowFunc: vm.monotonicNowMs}
		}
		vm.Async = asyncrt.NewExecutor[asyncPayload](cfg)
	}
	return vm.Async
}

func (vm *VM) ensureUserTaskState(task *asyncrt.Task[asyncPayload]) *userTaskState {
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
