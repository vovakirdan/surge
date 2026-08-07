package vm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// evalArrayLit builds an array literal.
//
// A FIXED array is a value composite and is built into its own extent; a
// dynamic array is a heap object and keeps its handle. Which one this is comes
// from the DESTINATION type, for the same reason a tuple literal needs one:
// MIR records an ArrayLit as its elements and nothing else, so the literal has
// no type of its own to decide with.
func (vm *VM) evalArrayLit(frame *Frame, lit *mir.ArrayLit, dstType types.TypeID) (Value, *VMError) {
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
	if dstType != types.NoTypeID && vm.isValueCompositeType(dstType) {
		return vm.buildStruct(frame, dstType, elems)
	}

	h := vm.Heap.AllocArray(types.NoTypeID, elems)
	return MakeHandleArray(h, types.NoTypeID), nil
}

// evalTupleLit builds a tuple into its own extent.
//
// The type comes from the DESTINATION because MIR does not record one: a
// TupleLit is a list of elements and nothing else. Under a box that was
// survivable — the old path allocated with types.NoTypeID, which is to say the
// VM tracked no composite type at all and the members were whatever they
// happened to be. Exact layout has no such option, so the type the destination
// declares is the only type the tuple can have.
func (vm *VM) evalTupleLit(frame *Frame, lit *mir.TupleLit, dstType types.TypeID) (Value, *VMError) {
	if lit == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "nil tuple literal")
	}
	if len(lit.Elems) == 0 {
		return MakeNothing(), nil
	}
	if dstType == types.NoTypeID {
		return Value{}, vm.eb.makeError(PanicTypeMismatch,
			"a tuple literal has no type of its own and its destination declares none")
	}
	dst, vmErr := vm.buildComposite(frame, dstType)
	if vmErr != nil {
		return Value{}, vmErr
	}
	for i := range lit.Elems {
		v, vmErr := vm.evalOperand(frame, &lit.Elems[i])
		if vmErr != nil {
			return Value{}, vmErr
		}
		if vmErr := vm.initCompositeMember(frame, dst, i, v); vmErr != nil {
			return Value{}, vmErr
		}
	}
	return MakeComposite(dst), nil
}

// evalIndex evaluates an index operation.
func (vm *VM) evalIndex(obj, idx Value) (Value, *VMError) {
	if obj.Kind == VKRef || obj.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(obj.Loc)
		if loadErr != nil {
			return Value{}, loadErr
		}
		obj = v
	}
	switch obj.Kind {
	case VKHandleArray:
		return vm.evalArrayIndex(obj, idx)
	case VKHandleString:
		return vm.evalStringIndex(obj, idx)
	case VKComposite:
		if res, handled, vmErr := vm.evalBytesViewIndex(obj, idx); handled {
			return res, vmErr
		}
		if owner, ok := obj.Storage(); ok {
			return vm.evalStorageIndex(owner, idx)
		}
	default:
	}
	return Value{}, vm.eb.typeMismatch("indexable value", obj.Kind.String())
}

func (vm *VM) evalArrayIndex(obj, idx Value) (Value, *VMError) {
	view, vmErr := vm.arrayViewFromHandle(obj.H)
	if vmErr != nil {
		return Value{}, vmErr
	}
	if idx.Kind == VKHandleRange {
		r, rangeErr := vm.rangeFromValue(idx)
		if rangeErr != nil {
			return Value{}, rangeErr
		}
		start, end, rangeErr := vm.rangeBounds(r, view.length)
		if rangeErr != nil {
			return Value{}, rangeErr
		}
		if start > end {
			start = end
		}
		baseStart := view.start + start
		length := end - start
		capacity := view.length - start
		h := vm.Heap.AllocArraySlice(types.NoTypeID, view.baseHandle, baseStart, length, capacity)
		return MakeHandleArray(h, types.NoTypeID), nil
	}
	index, vmErr := vm.arrayIndexFromValue(idx, view.length)
	if vmErr != nil {
		return Value{}, vmErr
	}
	baseIndex := view.start + index
	val, vmErr := vm.cloneForShare(view.baseObj.Arr[baseIndex])
	if vmErr != nil {
		return Value{}, vmErr
	}
	if vm.Types != nil {
		elemType, ok := vm.Types.ArrayInfo(vm.valueType(obj.TypeID))
		if !ok && view.baseObj != nil {
			elemType, ok = vm.Types.ArrayInfo(view.baseObj.TypeID)
		}
		if ok {
			if retagged, ok := vm.retagUnionValue(val, elemType); ok {
				val = retagged
			}
		}
	}
	return val, nil
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
		if start == end {
			h := vm.Heap.AllocStringWithCPLen(obj.TypeID, "", 0)
			return MakeHandleString(h, obj.TypeID), nil
		}
		byteLen := vm.byteLenForRange(strObj, start, end)
		h := vm.Heap.AllocStringSlice(obj.TypeID, obj.H, start, end-start, byteLen)
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
	r, ok := vm.codePointAtObj(strObj, int(index64))
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
	owner, ok := obj.Storage()
	if !ok {
		return Value{}, true, vm.eb.typeMismatch("struct", obj.Kind.String())
	}
	ptrVal, vmErr := vm.peekMember(owner, info.ptrIdx)
	if vmErr != nil {
		return Value{}, true, vmErr
	}
	lenVal, vmErr := vm.peekMember(owner, info.lenIdx)
	if vmErr != nil {
		return Value{}, true, vmErr
	}

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
	if strObj == nil {
		return Value{}, true, vm.eb.makeError(PanicOutOfBounds, "invalid string handle in bytes view")
	}
	if strObj.Kind != OKString {
		return Value{}, true, vm.eb.typeMismatch("string bytes pointer", fmt.Sprintf("%v", strObj.Kind))
	}
	offset := int(ptrVal.Loc.ByteOffset)
	pos := offset + index
	end := offset + length
	s := vm.stringBytes(strObj)
	if offset < 0 || pos < 0 || end < offset || end > len(s) {
		return Value{}, true, vm.eb.outOfBounds(pos, len(s))
	}
	b := s[pos]
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
	if obj == nil {
		return nil, vm.eb.makeError(PanicOutOfBounds, "invalid range handle")
	}
	if obj.Kind != OKRange {
		return nil, vm.eb.typeMismatch("range", fmt.Sprintf("%v", obj.Kind))
	}
	if obj.Range.Kind != RangeDescriptor {
		return nil, vm.eb.typeMismatch("range descriptor", "range iterator")
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
	// The literal builds INTO its own extent rather than collecting values and
	// boxing them afterwards, so there is no moment at which the struct exists
	// as anything other than its bytes.
	dst, vmErr := vm.buildComposite(frame, lit.TypeID)
	if vmErr != nil {
		return Value{}, vmErr
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
		if vmErr := vm.initCompositeMember(frame, dst, idx, val); vmErr != nil {
			return Value{}, vmErr
		}
	}
	return MakeComposite(dst), nil
}

// initCompositeMember writes one member of a composite that is being built.
//
// A composite member MOVES into its own extent inside the owner rather than
// becoming a second value hanging off it, which is what makes a nested
// composite one contiguous thing instead of a tree of boxes.
func (vm *VM) initCompositeMember(frame *Frame, owner StorageRef, index int, val Value) *VMError {
	member, vmErr := vm.memberStorage(owner, index)
	if vmErr != nil {
		return vmErr
	}
	return vm.storeStorage(frame, member, val)
}

func (vm *VM) evalFieldAccess(frame *Frame, fa *mir.FieldAccess) (Value, *VMError) {
	if fa == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "nil field access")
	}
	obj, owned, vmErr := vm.fieldAccessObject(frame, fa)
	if vmErr != nil {
		return Value{}, vmErr
	}
	if owned {
		defer vm.dropValue(obj)
	}
	target := obj
	if obj.Kind == VKRef || obj.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(obj.Loc)
		if loadErr != nil {
			return Value{}, loadErr
		}
		target = v
	}
	owner, ok := target.Storage()
	if !ok {
		return Value{}, vm.eb.typeMismatch("struct", target.Kind.String())
	}
	idx := fa.FieldIdx
	if fa.FieldName != "" {
		idx = -1
	}
	if idx < 0 {
		if fa.FieldName == "" {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "missing field name")
		}
		layout, vmErr := vm.layouts.Struct(owner.TypeID)
		if vmErr != nil {
			return Value{}, vmErr
		}
		var found bool
		idx, found = layout.IndexByName[fa.FieldName]
		if !found {
			return Value{}, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("unknown field %q on type#%d", fa.FieldName, owner.TypeID))
		}
	}
	if fa.MoveOut {
		field, vmErr := vm.memberStorage(owner, idx)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return vm.takeMember(frame, field)
	}
	// Reading a field yields a VALUE, so it is a copy. Handing back the
	// member's own reference would be right for a borrow and wrong here: the
	// two would be one value under two names, and the language says a composite
	// read is a copy.
	return vm.readMember(frame, owner, idx)
}

// fieldAccessObject reads the container a field access projects into, and says
// whether the caller owns what came back.
//
// A move-out EMPTIES the container it names, so it has to name the container
// ITSELF. Reading the object as a value cannot do that any more: a composite
// read is a copy, so the take would empty the copy while the original went on
// holding the member. Nothing is miscounted at that moment — the copy and the
// original each hold one reference — but the container's residual drop then
// zeroes an extent it believes was emptied, and what the member held is left
// with nothing that names it. That is the leak, and it is a leak the boxed
// representation could not have: a copy of a struct was a second reference to
// one box, so a take through it reached the original.
//
// The distinction only arises for the value-reading operand kinds. A borrow
// already names the container — the reference is followed to the storage it
// points at — and a move already transfers the container's own extent rather
// than duplicating it, so both reach the original as they stand.
func (vm *VM) fieldAccessObject(frame *Frame, fa *mir.FieldAccess) (Value, bool, *VMError) {
	if !fa.MoveOut || !operandReadsAPlaceByValue(fa.Object.Kind) {
		value, vmErr := vm.evalOperand(frame, &fa.Object)
		return value, true, vmErr
	}
	loc, vmErr := vm.EvalPlace(frame, fa.Object.Place)
	if vmErr != nil {
		return Value{}, false, vmErr
	}
	// Uncounted on purpose: this is a look at where the container lives, not a
	// second holder of it. The take that follows is what moves anything, and it
	// moves out of exactly these bytes.
	value, vmErr := vm.loadLocationRaw(loc)
	if vmErr != nil {
		return Value{}, false, vmErr
	}
	return value, false, nil
}

// operandReadsAPlaceByValue reports whether an operand kind names a place and
// answers with a VALUE read out of it, as opposed to a reference to it or a
// transfer of it.
func operandReadsAPlaceByValue(kind mir.OperandKind) bool {
	switch kind {
	case mir.OperandCopy, mir.OperandRetain, mir.OperandCopyValue:
		return true
	default:
		return false
	}
}

// takeMember reads a member OUT of its owner, leaving the owner not holding it.
//
// The two halves are one operation for the same reason a store is: between a
// read that has happened and a clearing that has not, the member has two
// owners, and the container's drop would release what the reader now holds.
// A composite member is copied into a temporary and its own extent released,
// which transfers what it owned rather than duplicating it; anything else is
// decoded WITHOUT counting a second holder and its extent zeroed, because the
// container's drop has already been narrowed to the members that stayed.
func (vm *VM) takeMember(frame *Frame, member StorageRef) (Value, *VMError) {
	if vm.storageCellKind(member.TypeID) == cellComposite {
		taken, vmErr := vm.buildComposite(frame, member.TypeID)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if vmErr := vm.moveComposite(frame, taken, MakeComposite(member), false); vmErr != nil {
			return Value{}, vmErr
		}
		return MakeComposite(taken), nil
	}
	shape, err := vm.memberAt(member.Offset, member.TypeID)
	if err != nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	taken, err := vm.storageReadCell(member, shape)
	if err != nil {
		return Value{}, vm.eb.makeError(PanicRCUseAfterFree, err.Error())
	}
	if err := vm.storageZero(member); err != nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return taken, nil
}

func (vm *VM) cloneForShare(v Value) (Value, *VMError) {
	if vm == nil || vm.Heap == nil {
		return v, nil
	}
	if v.IsHeap() && v.H != 0 {
		obj, ok := vm.Heap.lookup(v.H)
		if !ok || obj == nil {
			return Value{}, vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid handle %d", v.H))
		}
		if obj.Freed || obj.RefCount == 0 {
			return Value{}, vm.eb.makeError(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: handle %d (alloc=%d)", v.H, obj.AllocID))
		}
		vm.Heap.Retain(v.H)
	}
	return v, nil
}

// evalStorageIndex reads one element of a fixed array held in storage.
//
// The read is a COPY, like every other read of a member: an element handed back
// as its own reference would be the array under a second name.
func (vm *VM) evalStorageIndex(owner StorageRef, idx Value) (Value, *VMError) {
	members, err := vm.compositeMembers(owner.TypeID)
	if err != nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	index, vmErr := vm.arrayIndexFromValue(idx, len(members))
	if vmErr != nil {
		return Value{}, vmErr
	}
	return vm.readMember(vm.currentFrame(), owner, index)
}
