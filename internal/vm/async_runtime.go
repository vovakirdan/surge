package vm

import (
	"fmt"

	"surge/internal/asyncrt"
	"surge/internal/mir"
	"surge/internal/types"
)

func (vm *VM) ensureExecutor() *asyncrt.Executor {
	if vm == nil {
		return nil
	}
	if vm.Async == nil {
		vm.Async = asyncrt.NewExecutor(asyncrt.Config{Deterministic: true})
	}
	return vm.Async
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

func (vm *VM) pollTask(id asyncrt.TaskID) (bool, Value, *VMError) {
	exec := vm.ensureExecutor()
	if exec == nil {
		return false, Value{}, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	task := exec.Task(id)
	if task == nil {
		return false, Value{}, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", id))
	}
	if task.Status == asyncrt.TaskDone {
		res, _ := task.Result.(Value) //nolint:errcheck // type assertion, not error
		out, vmErr := vm.cloneForShare(res)
		if vmErr != nil {
			return false, Value{}, vmErr
		}
		return true, out, nil
	}

	exec.SetCurrent(id)
	defer exec.SetCurrent(0)

	task.Status = asyncrt.TaskRunning
	switch task.Kind {
	case asyncrt.TaskKindCheckpoint:
		if task.CheckpointPolled() {
			res := MakeNothing()
			exec.MarkDone(id, res)
			vm.releaseTaskState(task)
			out, vmErr := vm.cloneForShare(res)
			if vmErr != nil {
				return false, Value{}, vmErr
			}
			return true, out, nil
		}
		task.MarkCheckpointPolled()
		exec.Wake(id)
		return false, Value{}, nil
	default:
		ready, val, vmErr := vm.pollUserTask(task)
		if vmErr != nil {
			return false, Value{}, vmErr
		}
		if ready {
			exec.MarkDone(id, val)
			vm.releaseTaskState(task)
			out, vmErr := vm.cloneForShare(val)
			if vmErr != nil {
				return false, Value{}, vmErr
			}
			return true, out, nil
		}
		exec.Wake(id)
		return false, Value{}, nil
	}
}

func (vm *VM) pollUserTask(task *asyncrt.Task) (bool, Value, *VMError) {
	if vm == nil || task == nil {
		return false, Value{}, vm.eb.makeError(PanicUnimplemented, "missing task")
	}
	if vm.M == nil {
		return false, Value{}, vm.eb.makeError(PanicUnimplemented, "missing module")
	}
	fn := vm.M.Funcs[mir.FuncID(task.PollFuncID)] //nolint:gosec // PollFuncID is bounded by module
	if fn == nil {
		return false, Value{}, vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("missing poll function %d", task.PollFuncID))
	}
	ret, vmErr := vm.runFunction(fn, nil)
	if vmErr != nil {
		return false, Value{}, vmErr
	}
	return vm.interpretPollResult(ret)
}

func (vm *VM) interpretPollResult(val Value) (bool, Value, *VMError) {
	if val.Kind == VKNothing {
		return false, Value{}, nil
	}
	if val.Kind != VKHandleTag {
		return false, Value{}, vm.eb.typeMismatch("Poll", val.Kind.String())
	}
	defer vm.dropValue(val)
	layout, vmErr := vm.tagLayoutFor(val.TypeID)
	if vmErr != nil {
		return false, Value{}, vmErr
	}
	obj := vm.Heap.Get(val.H)
	if obj == nil || obj.Kind != OKTag {
		return false, Value{}, vm.eb.typeMismatch("tag", fmt.Sprintf("%v", obj.Kind))
	}
	tagName, _ := vm.tagNameForSym(layout, obj.Tag.TagSym)
	switch tagName {
	case "Some":
		if len(obj.Tag.Fields) == 0 {
			return false, Value{}, vm.eb.makeError(PanicTypeMismatch, "Some tag missing payload")
		}
		out, vmErr := vm.cloneForShare(obj.Tag.Fields[0])
		if vmErr != nil {
			return false, Value{}, vmErr
		}
		return true, out, nil
	case "nothing":
		return false, Value{}, nil
	default:
		return false, Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("unknown poll tag %q", tagName))
	}
}

func (vm *VM) runReadyOne() (bool, *VMError) {
	exec := vm.ensureExecutor()
	if exec == nil {
		return false, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	id, ok := exec.Dequeue()
	if !ok {
		return false, nil
	}
	ready, res, vmErr := vm.pollTask(id)
	if vmErr != nil {
		return true, vmErr
	}
	if ready {
		vm.dropValue(res)
	}
	return true, nil
}

func (vm *VM) runUntilDone(id asyncrt.TaskID) (Value, *VMError) {
	exec := vm.ensureExecutor()
	if exec == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "async executor missing")
	}
	exec.Wake(id)
	for {
		task := exec.Task(id)
		if task == nil {
			return Value{}, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid task id %d", id))
		}
		if task.Status == asyncrt.TaskDone {
			res, _ := task.Result.(Value) //nolint:errcheck // type assertion, not error
			out, vmErr := vm.cloneForShare(res)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return out, nil
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

func (vm *VM) runFunction(fn *mir.Func, args []Value) (Value, *VMError) {
	if vm == nil || fn == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "missing function")
	}
	savedStack := vm.Stack
	savedHalted := vm.Halted
	savedStarted := vm.started
	savedCapture := vm.captureReturn

	var ret Value
	vm.captureReturn = &ret
	vm.Stack = nil
	vm.Halted = false
	vm.started = true

	frame := NewFrame(fn)
	for i := range args {
		vmErr := vm.writeLocal(frame, mir.LocalID(i), args[i]) //nolint:gosec // i is bounded by args length
		if vmErr != nil {
			vm.Stack = savedStack
			vm.Halted = savedHalted
			vm.started = savedStarted
			vm.captureReturn = savedCapture
			return Value{}, vmErr
		}
	}
	vm.Stack = append(vm.Stack, *frame)

	for len(vm.Stack) > 0 && !vm.Halted {
		vmErr := vm.Step()
		if vmErr != nil {
			vm.Stack = savedStack
			vm.Halted = savedHalted
			vm.started = savedStarted
			vm.captureReturn = savedCapture
			return Value{}, vmErr
		}
	}

	vm.Stack = savedStack
	vm.Halted = savedHalted
	vm.started = savedStarted
	vm.captureReturn = savedCapture

	return ret, nil
}
