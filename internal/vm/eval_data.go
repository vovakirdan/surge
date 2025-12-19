package vm

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/mir"
	"surge/internal/types"
)

func (vm *VM) evalArrayLit(frame *Frame, lit *mir.ArrayLit) (Value, *VMError) {
	if lit == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "nil array literal")
	}
	elems := make([]Value, 0, len(lit.Elems))
	for i := range lit.Elems {
		v, vmErr := vm.evalOperand(frame, &lit.Elems[i])
		if vmErr != nil {
			return Value{}, vmErr
		}
		elems = append(elems, v)
	}

	h := vm.Heap.AllocArray(types.NoTypeID, elems)
	return MakeHandleArray(h, types.NoTypeID), nil
}

// evalIndex evaluates an index operation.
func (vm *VM) evalIndex(obj, idx Value) (Value, *VMError) {
	maxIndex := int(^uint(0) >> 1)
	maxInt := int64(maxIndex)
	maxUint := uint64(^uint(0) >> 1)
	var index int
	switch idx.Kind {
	case VKInt:
		if idx.Int < 0 || idx.Int > maxInt {
			return Value{}, vm.eb.outOfBounds(maxIndex, 0)
		}
		n, err := safecast.Conv[int](idx.Int)
		if err != nil {
			return Value{}, vm.eb.outOfBounds(maxIndex, 0)
		}
		index = n
	case VKBigInt:
		i, vmErr := vm.mustBigInt(idx)
		if vmErr != nil {
			return Value{}, vmErr
		}
		n, ok := i.Int64()
		if !ok || n < 0 || n > maxInt {
			return Value{}, vm.eb.outOfBounds(maxIndex, 0)
		}
		ni, err := safecast.Conv[int](n)
		if err != nil {
			return Value{}, vm.eb.outOfBounds(maxIndex, 0)
		}
		index = ni
	case VKBigUint:
		u, vmErr := vm.mustBigUint(idx)
		if vmErr != nil {
			return Value{}, vmErr
		}
		n, ok := u.Uint64()
		if !ok || n > maxUint {
			return Value{}, vm.eb.outOfBounds(maxIndex, 0)
		}
		ni, err := safecast.Conv[int](n)
		if err != nil {
			return Value{}, vm.eb.outOfBounds(maxIndex, 0)
		}
		index = ni
	default:
		return Value{}, vm.eb.typeMismatch("int", idx.Kind.String())
	}

	if obj.Kind != VKHandleArray {
		return Value{}, vm.eb.typeMismatch("array", obj.Kind.String())
	}
	arrObj := vm.Heap.Get(obj.H)
	if arrObj == nil {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, "invalid array handle")
	}
	if arrObj.Kind != OKArray {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("expected array handle, got %v", arrObj.Kind))
	}
	if index < 0 || index >= len(arrObj.Arr) {
		return Value{}, vm.eb.outOfBounds(index, len(arrObj.Arr))
	}
	return vm.cloneForShare(arrObj.Arr[index])
}

func (vm *VM) evalStructLit(frame *Frame, lit *mir.StructLit) (Value, *VMError) {
	if lit == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "nil struct literal")
	}
	layout, vmErr := vm.layouts.Struct(lit.TypeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	fields := make([]Value, len(layout.FieldNames))
	for i := range fields {
		fields[i] = Value{Kind: VKInvalid}
	}
	for i := range lit.Fields {
		f := &lit.Fields[i]
		val, vmErr := vm.evalOperand(frame, &f.Value)
		if vmErr != nil {
			return Value{}, vmErr
		}
		idx, ok := layout.IndexByName[f.Name]
		if !ok {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("struct type#%d has no field %q", layout.TypeID, f.Name))
		}
		fields[idx] = val
	}
	h := vm.Heap.AllocStruct(layout.TypeID, fields)
	return MakeHandleStruct(h, lit.TypeID), nil
}

func (vm *VM) evalFieldAccess(frame *Frame, fa *mir.FieldAccess) (Value, *VMError) {
	if fa == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "nil field access")
	}
	if fa.FieldIdx >= 0 {
		return Value{}, vm.eb.unimplemented("tuple field access")
	}
	if fa.FieldName == "" {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "missing field name")
	}
	obj, vmErr := vm.evalOperand(frame, &fa.Object)
	if vmErr != nil {
		return Value{}, vmErr
	}
	defer vm.dropValue(obj)
	if obj.Kind != VKHandleStruct {
		return Value{}, vm.eb.typeMismatch("struct", obj.Kind.String())
	}
	sobj := vm.Heap.Get(obj.H)
	if sobj == nil {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, "invalid struct handle")
	}
	if sobj.Kind != OKStruct {
		return Value{}, vm.eb.typeMismatch("struct", fmt.Sprintf("%v", sobj.Kind))
	}
	layout, vmErr := vm.layouts.Struct(sobj.TypeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	idx, ok := layout.IndexByName[fa.FieldName]
	if !ok {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("unknown field %q on type#%d", fa.FieldName, sobj.TypeID))
	}
	if idx < 0 || idx >= len(sobj.Fields) {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("field index %d out of bounds for type#%d", idx, sobj.TypeID))
	}
	return vm.cloneForShare(sobj.Fields[idx])
}

func (vm *VM) cloneForShare(v Value) (Value, *VMError) {
	if vm == nil || vm.Heap == nil {
		return v, nil
	}
	if v.IsHeap() && v.H != 0 {
		vm.Heap.Retain(v.H)
	}
	return v, nil
}
