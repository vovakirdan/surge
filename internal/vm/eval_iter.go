package vm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
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
	if obj.Range.Kind != RangeArrayIter {
		return Value{}, vm.eb.unimplemented("range descriptor iteration")
	}

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

	elemType := vm.rangeElemType(next.Iter.Type)
	if elemType == types.NoTypeID {
		elemType = vm.rangeElemType(iterVal.TypeID)
	}
	optType := vm.optionTypeForElem(elemType)
	res, vmErr := vm.makeOptionSome(optType, elem)
	if vmErr != nil {
		vm.dropValue(elem)
		return Value{}, vmErr
	}
	return res, nil
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
