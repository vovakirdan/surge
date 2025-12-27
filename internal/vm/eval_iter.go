package vm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

func (vm *VM) evalIterInit(frame *Frame, init *mir.IterInit) (Value, *VMError) {
	if init == nil {
		return Value{}, vm.eb.unimplemented("nil iter_init")
	}
	iterVal, vmErr := vm.evalOperand(frame, &init.Iterable)
	if vmErr != nil {
		return Value{}, vmErr
	}
	ownsIterVal := true
	defer func() {
		if ownsIterVal {
			vm.dropValue(iterVal)
		}
	}()

	target := iterVal
	if target.Kind == VKRef || target.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(target.Loc)
		if loadErr != nil {
			return Value{}, loadErr
		}
		target = v
	}

	switch target.Kind {
	case VKHandleArray:
		view, vmErr := vm.arrayViewFromHandle(target.H)
		if vmErr != nil {
			return Value{}, vmErr
		}
		h := vm.Heap.AllocArrayIterRange(types.NoTypeID, view.baseHandle, view.start, view.length)
		return MakeHandleRange(h, types.NoTypeID), nil
	case VKHandleRange:
		if iterVal.Kind == VKHandleRange {
			ownsIterVal = false
			return iterVal, nil
		}
		owned, vmErr := vm.cloneForShare(target)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return owned, nil
	default:
		return Value{}, vm.eb.typeMismatch("iterable", target.Kind.String())
	}
}

func (vm *VM) evalIterNext(frame *Frame, next *mir.IterNext) (Value, *VMError) {
	if next == nil {
		return Value{}, vm.eb.unimplemented("nil iter_next")
	}
	iterVal, vmErr := vm.evalOperand(frame, &next.Iter)
	if vmErr != nil {
		return Value{}, vmErr
	}
	defer vm.dropValue(iterVal)

	target := iterVal
	if target.Kind == VKRef || target.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(target.Loc)
		if loadErr != nil {
			return Value{}, loadErr
		}
		target = v
	}
	if target.Kind != VKHandleRange {
		return Value{}, vm.eb.typeMismatch("range", target.Kind.String())
	}

	obj := vm.Heap.Get(target.H)
	if obj == nil {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, "invalid range handle")
	}
	if obj.Kind != OKRange {
		return Value{}, vm.eb.typeMismatch("range", fmt.Sprintf("%v", obj.Kind))
	}
	elemType := vm.rangeElemType(next.Iter.Type)
	if elemType == types.NoTypeID {
		elemType = vm.rangeElemType(iterVal.TypeID)
	}
	if elemType == types.NoTypeID {
		elemType = vm.rangeElemType(target.TypeID)
	}

	switch obj.Range.Kind {
	case RangeArrayIter:
		if obj.Range.ArrayIndex >= obj.Range.ArrayLen {
			return MakeNothing(), nil
		}

		base := obj.Range.ArrayBase
		if base == 0 {
			return Value{}, vm.eb.makeError(PanicOutOfBounds, "range iterator missing base array")
		}
		baseObj := vm.Heap.Get(base)
		if baseObj.Kind != OKArray {
			return Value{}, vm.eb.typeMismatch("array", fmt.Sprintf("%v", baseObj.Kind))
		}
		idx := obj.Range.ArrayStart + obj.Range.ArrayIndex
		if idx < 0 || idx >= len(baseObj.Arr) {
			return Value{}, vm.eb.makeError(PanicOutOfBounds, "range iterator index out of bounds")
		}

		elem, vmErr := vm.cloneForShare(baseObj.Arr[idx])
		if vmErr != nil {
			return Value{}, vmErr
		}
		obj.Range.ArrayIndex++

		optType := vm.optionTypeForElem(elemType)
		res, vmErr := vm.makeOptionSome(optType, elem)
		if vmErr != nil {
			vm.dropValue(elem)
			return Value{}, vmErr
		}
		return res, nil
	case RangeDescriptor:
		elem, ok, vmErr := vm.rangeDescriptorNextValue(&obj.Range, elemType)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if !ok {
			return MakeNothing(), nil
		}
		optType := vm.optionTypeForElem(elemType)
		res, vmErr := vm.makeOptionSome(optType, elem)
		if vmErr != nil {
			vm.dropValue(elem)
			return Value{}, vmErr
		}
		return res, nil
	default:
		return Value{}, vm.eb.unimplemented("range iterator kind")
	}
}

func (vm *VM) rangeElemType(rangeType types.TypeID) types.TypeID {
	if vm.Types == nil || rangeType == types.NoTypeID {
		return types.NoTypeID
	}
	rangeType = vm.valueType(rangeType)
	info, ok := vm.Types.StructInfo(rangeType)
	if !ok || info == nil || len(info.TypeArgs) != 1 {
		return types.NoTypeID
	}
	if vm.Types.Strings != nil {
		rangeName := vm.Types.Strings.Intern("Range")
		if info.Name != rangeName {
			return types.NoTypeID
		}
	}
	return info.TypeArgs[0]
}

func (vm *VM) optionTypeForElem(elem types.TypeID) types.TypeID {
	if vm.Types == nil || vm.Types.Strings == nil || elem == types.NoTypeID {
		return types.NoTypeID
	}
	optionName := vm.Types.Strings.Intern("Option")
	if id, ok := vm.Types.FindUnionInstance(optionName, []types.TypeID{elem}); ok {
		return id
	}
	return types.NoTypeID
}

func (vm *VM) rangeDescriptorNextValue(r *RangeObject, elemType types.TypeID) (Value, bool, *VMError) {
	if r == nil {
		return Value{}, false, vm.eb.typeMismatch("range", "nil")
	}
	if !r.HasStart {
		startVal := vm.rangeZeroValue(elemType)
		r.Start = startVal
		r.HasStart = true
	}

	cur := r.Start
	if cur.Kind == VKInvalid {
		return Value{}, false, vm.eb.typeMismatch("int", cur.Kind.String())
	}
	if cur.TypeID == types.NoTypeID && elemType != types.NoTypeID {
		cur.TypeID = elemType
	}
	if r.HasEnd {
		if r.End.Kind == VKInvalid {
			return Value{}, false, vm.eb.typeMismatch("int", r.End.Kind.String())
		}
		cmp, vmErr := vm.rangeValueCompare(cur, r.End)
		if vmErr != nil {
			return Value{}, false, vmErr
		}
		if (!r.Inclusive && cmp >= 0) || (r.Inclusive && cmp > 0) {
			return Value{}, false, nil
		}
	}

	nextVal, vmErr := vm.rangeValueAddOne(cur, elemType)
	if vmErr != nil {
		return Value{}, false, vmErr
	}
	r.Start = nextVal
	return cur, true, nil
}

func (vm *VM) rangeValueCompare(left, right Value) (int, *VMError) {
	la, vmErr := vm.rangeValueToBigInt(left)
	if vmErr != nil {
		return 0, vmErr
	}
	lb, vmErr := vm.rangeValueToBigInt(right)
	if vmErr != nil {
		return 0, vmErr
	}
	return la.Cmp(lb), nil
}

func (vm *VM) rangeValueToBigInt(v Value) (bignum.BigInt, *VMError) {
	switch v.Kind {
	case VKInt:
		return bignum.IntFromInt64(v.Int), nil
	case VKBigInt:
		return vm.mustBigInt(v)
	case VKBigUint:
		u, vmErr := vm.mustBigUint(v)
		if vmErr != nil {
			return bignum.BigInt{}, vmErr
		}
		return bignum.BigInt{Limbs: u.Limbs}, nil
	default:
		return bignum.BigInt{}, vm.eb.typeMismatch("int", v.Kind.String())
	}
}

func (vm *VM) rangeValueAddOne(cur Value, elemType types.TypeID) (Value, *VMError) {
	if cur.TypeID == types.NoTypeID && elemType != types.NoTypeID {
		cur.TypeID = elemType
	}
	switch cur.Kind {
	case VKInt:
		res, ok := AddInt64Checked(cur.Int, 1)
		if !ok {
			if vm.isUnboundedInt(elemType) {
				base := bignum.IntFromInt64(cur.Int)
				resBig, err := bignum.IntAdd(base, bignum.IntFromInt64(1))
				if err != nil {
					return Value{}, vm.bignumErr(err)
				}
				return vm.makeBigInt(cur.TypeID, resBig), nil
			}
			return Value{}, vm.eb.intOverflow()
		}
		return MakeInt(res, cur.TypeID), nil
	case VKBigInt:
		base, vmErr := vm.mustBigInt(cur)
		if vmErr != nil {
			return Value{}, vmErr
		}
		resBig, err := bignum.IntAdd(base, bignum.IntFromInt64(1))
		if err != nil {
			return Value{}, vm.bignumErr(err)
		}
		return vm.makeBigInt(cur.TypeID, resBig), nil
	case VKBigUint:
		base, vmErr := vm.mustBigUint(cur)
		if vmErr != nil {
			return Value{}, vmErr
		}
		resBig, err := bignum.UintAddSmall(base, 1)
		if err != nil {
			return Value{}, vm.bignumErr(err)
		}
		return vm.makeBigUint(cur.TypeID, resBig), nil
	default:
		return Value{}, vm.eb.typeMismatch("int", cur.Kind.String())
	}
}

func (vm *VM) rangeZeroValue(elemType types.TypeID) Value {
	if vm.isUnboundedInt(elemType) {
		return vm.makeBigInt(elemType, bignum.IntFromInt64(0))
	}
	return MakeInt(0, elemType)
}

func (vm *VM) isUnboundedInt(typeID types.TypeID) bool {
	if vm.Types == nil || typeID == types.NoTypeID {
		return false
	}
	tt, ok := vm.Types.Lookup(typeID)
	if !ok {
		return false
	}
	return tt.Kind == types.KindInt && tt.Width == types.WidthAny
}
