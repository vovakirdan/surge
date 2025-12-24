package vm

import (
	"fmt"

	"surge/internal/mir"
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
	if arrVal.Kind != VKHandleArray {
		return vm.eb.typeMismatch("array", arrVal.Kind.String())
	}
	view, vmErr := vm.arrayViewFromHandle(arrVal.H)
	if vmErr != nil {
		return vmErr
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	h := vm.Heap.AllocArrayIterRange(dstType, view.baseHandle, view.start, view.length)
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
	if obj.Range.Kind != RangeArrayIter {
		return vm.eb.unimplemented("range descriptor iteration")
	}

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
	baseObj := vm.Heap.Get(base)
	if baseObj.Kind != OKArray {
		return vm.eb.typeMismatch("array", fmt.Sprintf("%v", baseObj.Kind))
	}
	idx := obj.Range.ArrayStart + obj.Range.ArrayIndex
	if idx < 0 || idx >= len(baseObj.Arr) {
		return vm.eb.makeError(PanicOutOfBounds, "range iterator index out of bounds")
	}

	elem, vmErr := vm.cloneForShare(baseObj.Arr[idx])
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
}
