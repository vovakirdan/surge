package vm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

const (
	durationOpaqueField = "__opaque"
	nanosPerMicro       = int64(1_000)
	nanosPerMilli       = int64(1_000_000)
	nanosPerSecond      = int64(1_000_000_000)
)

func (vm *VM) handleMonotonicNow(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicUnimplemented, "monotonic_now missing destination")
	}
	if len(call.Args) != 0 {
		return vm.eb.makeError(PanicUnimplemented, "monotonic_now expects no arguments")
	}
	if vm.RT == nil {
		return vm.eb.makeError(PanicUnimplemented, "runtime missing")
	}
	ns := vm.RT.MonotonicNow()
	val, vmErr := vm.durationValue(ns, frame.Locals[call.Dst.Local].TypeID)
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.writeLocal(frame, call.Dst.Local, val); vmErr != nil {
		vm.dropValue(val)
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: call.Dst.Local,
			Name:    frame.Locals[call.Dst.Local].Name,
			Value:   val,
		})
	}
	return nil
}

func (vm *VM) handleDurationMethod(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite, name string) *VMError {
	if call == nil || !call.HasDst {
		return vm.eb.makeError(PanicTypeMismatch, name+" requires a destination")
	}
	switch name {
	case "sub":
		return vm.handleDurationSub(frame, call, writes)
	case "as_seconds":
		return vm.handleDurationUnit(frame, call, writes, name, nanosPerSecond)
	case "as_millis":
		return vm.handleDurationUnit(frame, call, writes, name, nanosPerMilli)
	case "as_micros":
		return vm.handleDurationUnit(frame, call, writes, name, nanosPerMicro)
	case "as_nanos":
		return vm.handleDurationUnit(frame, call, writes, name, 1)
	default:
		return vm.eb.unsupportedIntrinsic(name)
	}
}

func (vm *VM) handleDurationSub(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "Duration.sub requires 2 arguments")
	}
	left, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(left)
	right, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(right)
	leftNanos, vmErr := vm.durationNanos(left)
	if vmErr != nil {
		return vmErr
	}
	rightNanos, vmErr := vm.durationNanos(right)
	if vmErr != nil {
		return vmErr
	}
	res, vmErr := vm.durationValue(leftNanos-rightNanos, frame.Locals[call.Dst.Local].TypeID)
	if vmErr != nil {
		return vmErr
	}
	return vm.writeDurationResult(frame, call.Dst.Local, res, writes)
}

func (vm *VM) handleDurationUnit(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite, name string, divisor int64) *VMError {
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, name+" requires 1 argument")
	}
	value, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(value)
	nanos, vmErr := vm.durationNanos(value)
	if vmErr != nil {
		return vmErr
	}
	if divisor != 1 {
		nanos /= divisor
	}
	dstLocal := call.Dst.Local
	res := MakeInt(nanos, frame.Locals[dstLocal].TypeID)
	return vm.writeDurationResult(frame, dstLocal, res, writes)
}

func (vm *VM) durationNanos(value Value) (int64, *VMError) {
	if value.Kind == VKRef || value.Kind == VKRefMut {
		loaded, vmErr := vm.loadLocationRaw(value.Loc)
		if vmErr != nil {
			return 0, vmErr
		}
		value = loaded
	}
	if value.Kind != VKHandleStruct {
		return 0, vm.eb.typeMismatch("Duration", value.Kind.String())
	}
	obj := vm.Heap.Get(value.H)
	if obj == nil {
		return 0, vm.eb.typeMismatch("Duration", "invalid handle")
	}
	if obj.Kind != OKStruct {
		return 0, vm.eb.typeMismatch("Duration", fmt.Sprintf("%v", obj.Kind))
	}
	layout, vmErr := vm.layouts.Struct(value.TypeID)
	if vmErr != nil {
		return 0, vmErr
	}
	idx, ok := layout.IndexByName[durationOpaqueField]
	if !ok {
		return 0, vm.eb.makeError(PanicTypeMismatch, "Duration missing __opaque field")
	}
	if idx < 0 || idx >= len(obj.Fields) {
		return 0, vm.eb.makeError(PanicOutOfBounds, "Duration __opaque field out of range")
	}
	field := obj.Fields[idx]
	if field.Kind != VKInt {
		return 0, vm.eb.typeMismatch("int64", field.Kind.String())
	}
	return field.Int, nil
}

func (vm *VM) durationValue(nanos int64, typeID types.TypeID) (Value, *VMError) {
	layout, vmErr := vm.layouts.Struct(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	fields := make([]Value, len(layout.FieldNames))
	for i := range fields {
		fields[i] = Value{Kind: VKInvalid}
	}
	idx, ok := layout.IndexByName[durationOpaqueField]
	if !ok {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "Duration missing __opaque field")
	}
	fieldType := layout.FieldTypes[idx]
	if fieldType == types.NoTypeID && vm.Types != nil {
		fieldType = vm.Types.Builtins().Int
	}
	fields[idx] = MakeInt(nanos, fieldType)
	h := vm.Heap.AllocStruct(typeID, fields)
	return MakeHandleStruct(h, typeID), nil
}

func (vm *VM) writeDurationResult(frame *Frame, dst mir.LocalID, value Value, writes *[]LocalWrite) *VMError {
	if vmErr := vm.writeLocal(frame, dst, value); vmErr != nil {
		if value.IsHeap() {
			vm.dropValue(value)
		}
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dst,
			Name:    frame.Locals[dst].Name,
			Value:   value,
		})
	}
	return nil
}
