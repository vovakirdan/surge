package vm

import (
	"fmt"

	"surge/internal/mir"

	"surge/internal/types"
)

func (vm *VM) handleArrayRange(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "__range requires 1 argument")
	}
	arrVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arrVal)
	if arrVal.Kind == VKRef || arrVal.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(arrVal.Loc)
		if loadErr != nil {
			return loadErr
		}
		arrVal = v
	}
	// A fixed array is a composite in ordinary storage and has no handle for the
	// range to hold. Giving it a slice header over its own extent first means the
	// range needs to know nothing about arenas: iterating `f` and iterating
	// `f[[0..n]]` become the same code from here on.
	var sliceHandle Handle
	switch arrVal.Kind {
	case VKHandleArray:
	case VKComposite:
		owner, ok := arrVal.Storage()
		if !ok {
			return vm.eb.typeMismatch("array", arrVal.Kind.String())
		}
		base, baseErr := vm.arrayViewFromComposite(owner)
		if baseErr != nil {
			return baseErr
		}
		sliceHandle = vm.Heap.AllocArraySliceStorage(types.NoTypeID, owner, 0, base.length)
		defer vm.Heap.Release(sliceHandle)
		arrVal = MakeHandleArray(sliceHandle, types.NoTypeID)
	default:
		return vm.eb.typeMismatch("array", arrVal.Kind.String())
	}
	view, vmErr := vm.arrayViewFromHandle(arrVal.H)
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	rangeBase := view.baseHandle
	if rangeBase == 0 {
		// An arena-backed view has no base object; the range holds the slice
		// header, whose own reference is dropped by the deferred release above
		// once AllocArrayIterRange has taken its count.
		rangeBase = arrVal.H
	}
	h := vm.Heap.AllocArrayIterRange(dstType, rangeBase, view.start, view.length)
	val := MakeHandleRange(h, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		vm.Heap.Release(h)
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})
	}
	return nil
}

func (vm *VM) handleRangeNext(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "next requires 1 argument")
	}
	rangeVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(rangeVal)
	if rangeVal.Kind == VKRef || rangeVal.Kind == VKRefMut {
		v, loadErr := vm.loadLocationRaw(rangeVal.Loc)
		if loadErr != nil {
			return loadErr
		}
		rangeVal = v
	}
	if rangeVal.Kind != VKHandleRange {
		return vm.eb.typeMismatch("range", rangeVal.Kind.String())
	}

	obj := vm.Heap.Get(rangeVal.H)
	if obj.Kind != OKRange {
		return vm.eb.typeMismatch("range", fmt.Sprintf("%v", obj.Kind))
	}
	switch obj.Range.Kind {
	case RangeArrayIter:
		if obj.Range.ArrayIndex >= obj.Range.ArrayLen {
			if !call.HasDst {
				return nil
			}
			dstLocal := call.Dst.Local
			res := MakeNothing()
			if err := vm.writeLocal(frame, dstLocal, res); err != nil {
				return err
			}
			if writes != nil {
				*writes = append(*writes, LocalWrite{
					LocalID: dstLocal,
					Name:    frame.Locals[dstLocal].Name,
					Value:   res,
				})
			}
			return nil
		}

		base := obj.Range.ArrayBase
		if base == 0 {
			return vm.eb.makeError(PanicOutOfBounds, "range iterator missing base array")
		}
		baseView, vmErr := vm.arrayViewFromHandle(base)
		if vmErr != nil {
			return vmErr
		}
		idx := obj.Range.ArrayStart + obj.Range.ArrayIndex
		if idx < 0 || idx >= baseView.length {
			return vm.eb.makeError(PanicOutOfBounds, "range iterator index out of bounds")
		}

		elem, vmErr := vm.viewElemValue(frame, baseView, idx)
		if vmErr != nil {
			return vmErr
		}
		obj.Range.ArrayIndex++

		if !call.HasDst {
			vm.dropValue(elem)
			return nil
		}

		dstLocal := call.Dst.Local
		dstType := frame.Locals[dstLocal].TypeID
		res, vmErr := vm.makeOptionSome(dstType, elem)
		if vmErr != nil {
			vm.dropValue(elem)
			return vmErr
		}
		if err := vm.writeLocal(frame, dstLocal, res); err != nil {
			vm.dropValue(res)
			return err
		}
		if writes != nil {
			*writes = append(*writes, LocalWrite{
				LocalID: dstLocal,
				Name:    frame.Locals[dstLocal].Name,
				Value:   res,
			})
		}
		return nil
	case RangeDescriptor:
		elemType := vm.rangeElemType(rangeVal.TypeID)
		elem, ok, vmErr := vm.rangeDescriptorNextValue(&obj.Range, elemType)
		if vmErr != nil {
			return vmErr
		}
		if !ok {
			if !call.HasDst {
				return nil
			}
			dstLocal := call.Dst.Local
			res := MakeNothing()
			if err := vm.writeLocal(frame, dstLocal, res); err != nil {
				return err
			}
			if writes != nil {
				*writes = append(*writes, LocalWrite{
					LocalID: dstLocal,
					Name:    frame.Locals[dstLocal].Name,
					Value:   res,
				})
			}
			return nil
		}

		if !call.HasDst {
			vm.dropValue(elem)
			return nil
		}

		dstLocal := call.Dst.Local
		dstType := frame.Locals[dstLocal].TypeID
		res, vmErr := vm.makeOptionSome(dstType, elem)
		if vmErr != nil {
			vm.dropValue(elem)
			return vmErr
		}
		if err := vm.writeLocal(frame, dstLocal, res); err != nil {
			vm.dropValue(res)
			return err
		}
		if writes != nil {
			*writes = append(*writes, LocalWrite{
				LocalID: dstLocal,
				Name:    frame.Locals[dstLocal].Name,
				Value:   res,
			})
		}
		return nil
	default:
		return vm.eb.unimplemented("range iterator kind")
	}
}
