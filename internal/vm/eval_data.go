package vm

import (
	"fmt"

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
	switch obj.Kind {
	case VKHandleArray:
		return vm.evalArrayIndex(obj, idx)
	case VKHandleString:
		return vm.evalStringIndex(obj, idx)
	case VKHandleStruct:
		if res, handled, vmErr := vm.evalBytesViewIndex(obj, idx); handled {
			return res, vmErr
		}
	default:
	}
	return Value{}, vm.eb.typeMismatch("indexable value", obj.Kind.String())
}

func (vm *VM) evalArrayIndex(obj, idx Value) (Value, *VMError) {
	arrObj := vm.Heap.Get(obj.H)
	if arrObj == nil {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, "invalid array handle")
	}
	if arrObj.Kind != OKArray {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("expected array handle, got %v", arrObj.Kind))
	}
	if idx.Kind == VKHandleRange {
		r, vmErr := vm.rangeFromValue(idx)
		if vmErr != nil {
			return Value{}, vmErr
		}
		start, end, vmErr := vm.rangeBounds(r, len(arrObj.Arr))
		if vmErr != nil {
			return Value{}, vmErr
		}
		if start > end {
			start = end
		}
		elems := make([]Value, 0, end-start)
		for i := start; i < end; i++ {
			v, vmErr := vm.cloneForShare(arrObj.Arr[i])
			if vmErr != nil {
				for _, el := range elems {
					vm.dropValue(el)
				}
				return Value{}, vmErr
			}
			elems = append(elems, v)
		}
		h := vm.Heap.AllocArray(types.NoTypeID, elems)
		return MakeHandleArray(h, types.NoTypeID), nil
	}
	index, vmErr := vm.nonNegativeIndexValue(idx)
	if vmErr != nil {
		return Value{}, vmErr
	}
	if index < 0 || index >= len(arrObj.Arr) {
		return Value{}, vm.eb.outOfBounds(index, len(arrObj.Arr))
	}
	return vm.cloneForShare(arrObj.Arr[index])
}

func (vm *VM) evalStringIndex(obj, idx Value) (Value, *VMError) {
	strObj := vm.Heap.Get(obj.H)
	if strObj == nil {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, "invalid string handle")
	}
	if strObj.Kind != OKString {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("expected string handle, got %v", strObj.Kind))
	}
	if idx.Kind == VKHandleRange {
		r, vmErr := vm.rangeFromValue(idx)
		if vmErr != nil {
			return Value{}, vmErr
		}
		cpLen := vm.stringCPLen(strObj)
		start, end, vmErr := vm.rangeBounds(r, cpLen)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if start > end {
			start = end
		}
		byteStart, byteEnd := byteOffsetsForCodePoints(strObj.Str, start, end)
		sub := strObj.Str[byteStart:byteEnd]
		h := vm.Heap.AllocStringWithCPLen(obj.TypeID, sub, end-start)
		return MakeHandleString(h, obj.TypeID), nil
	}

	var index64 int64
	switch idx.Kind {
	case VKInt:
		index64 = idx.Int
	case VKBigInt:
		i, vmErr := vm.mustBigInt(idx)
		if vmErr != nil {
			return Value{}, vmErr
		}
		n, ok := i.Int64()
		if !ok {
			return Value{}, vm.eb.outOfBounds(int(^uint(0)>>1), vm.stringCPLen(strObj))
		}
		index64 = n
	default:
		return Value{}, vm.eb.typeMismatch("int", idx.Kind.String())
	}

	cpLen := vm.stringCPLen(strObj)
	if index64 < 0 {
		index64 += int64(cpLen)
	}
	if index64 < 0 || index64 >= int64(cpLen) {
		return Value{}, vm.eb.outOfBounds(int(index64), cpLen)
	}
	r, ok := codePointAt(strObj.Str, int(index64))
	if !ok {
		return Value{}, vm.eb.outOfBounds(int(index64), cpLen)
	}
	typeID := types.NoTypeID
	if vm.Types != nil {
		typeID = vm.Types.Builtins().Uint32
	}
	return MakeInt(int64(r), typeID), nil
}

func (vm *VM) evalBytesViewIndex(obj, idx Value) (Value, bool, *VMError) {
	info, vmErr := vm.bytesViewLayout(obj.TypeID)
	if vmErr != nil {
		return Value{}, true, vmErr
	}
	if !info.ok {
		return Value{}, false, nil
	}
	sobj := vm.Heap.Get(obj.H)
	if sobj == nil {
		return Value{}, true, vm.eb.makeError(PanicOutOfBounds, "invalid struct handle")
	}
	if sobj.Kind != OKStruct {
		return Value{}, true, vm.eb.typeMismatch("struct", fmt.Sprintf("%v", sobj.Kind))
	}
	if info.ownerIdx < 0 || info.ownerIdx >= len(sobj.Fields) || info.ptrIdx < 0 || info.ptrIdx >= len(sobj.Fields) || info.lenIdx < 0 || info.lenIdx >= len(sobj.Fields) {
		return Value{}, true, vm.eb.makeError(PanicOutOfBounds, "bytes view layout mismatch")
	}

	ptrVal := sobj.Fields[info.ptrIdx]
	lenVal := sobj.Fields[info.lenIdx]
	_ = sobj.Fields[info.ownerIdx]

	index, vmErr := vm.nonNegativeIndexValue(idx)
	if vmErr != nil {
		return Value{}, true, vmErr
	}
	length, vmErr := vm.uintValueToInt(lenVal, "bytes view length out of range")
	if vmErr != nil {
		return Value{}, true, vmErr
	}
	if index < 0 || index >= length {
		return Value{}, true, vm.eb.outOfBounds(index, length)
	}
	if ptrVal.Kind != VKPtr || ptrVal.Loc.Kind != LKStringBytes {
		return Value{}, true, vm.eb.invalidLocation("bytes view pointer is not string bytes")
	}
	strObj := vm.Heap.Get(ptrVal.Loc.Handle)
	if strObj.Kind != OKString {
		return Value{}, true, vm.eb.typeMismatch("string bytes pointer", fmt.Sprintf("%v", strObj.Kind))
	}
	offset := int(ptrVal.Loc.ByteOffset)
	pos := offset + index
	end := offset + length
	if offset < 0 || pos < 0 || end < offset || end > len(strObj.Str) {
		return Value{}, true, vm.eb.outOfBounds(pos, len(strObj.Str))
	}
	b := strObj.Str[pos]
	typeID := types.NoTypeID
	if vm.Types != nil {
		typeID = vm.Types.Builtins().Uint8
	}
	return MakeInt(int64(b), typeID), true, nil
}

func (vm *VM) rangeFromValue(v Value) (*RangeObject, *VMError) {
	if v.Kind != VKHandleRange {
		return nil, vm.eb.typeMismatch("range", v.Kind.String())
	}
	obj := vm.Heap.Get(v.H)
	if obj.Kind != OKRange {
		return nil, vm.eb.typeMismatch("range", fmt.Sprintf("%v", obj.Kind))
	}
	return &obj.Range, nil
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
