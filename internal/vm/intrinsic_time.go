package vm

import (
	"surge/internal/mir"
	"surge/internal/types"
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

func (vm *VM) durationValue(nanos int64, typeID types.TypeID) (Value, *VMError) {
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
