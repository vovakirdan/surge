package vm

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/mir"
	"surge/internal/types"
)

func (vm *VM) handleArrayReserve(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	_ = writes
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_array_reserve requires 2 arguments")
	}
	arrVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arrVal)
	capVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(capVal)

	arrObj, vmErr := vm.arrayOwnedFromValue(arrVal)
	if vmErr != nil {
		return vmErr
	}
	newCap, vmErr := vm.uintValueToInt(capVal, "array capacity out of range")
	if vmErr != nil {
		return vmErr
	}
	if newCap <= cap(arrObj.Arr) {
		return nil
	}
	if newCap < len(arrObj.Arr) {
		newCap = len(arrObj.Arr)
	}
	grown := growArrayCapacity(cap(arrObj.Arr), newCap)
	newArr := make([]Value, len(arrObj.Arr), grown)
	copy(newArr, arrObj.Arr)
	arrObj.Arr = newArr
	return nil
}

func (vm *VM) handleArrayPush(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	_ = writes
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_array_push requires 2 arguments")
	}
	arrVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arrVal)
	pushVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}

	arrObj, vmErr := vm.arrayOwnedFromValue(arrVal)
	if vmErr != nil {
		vm.dropValue(pushVal)
		return vmErr
	}

	if len(arrObj.Arr) == cap(arrObj.Arr) {
		grown := growArrayCapacity(cap(arrObj.Arr), len(arrObj.Arr)+1)
		newArr := make([]Value, len(arrObj.Arr), grown)
		copy(newArr, arrObj.Arr)
		arrObj.Arr = newArr
	}

	idx := len(arrObj.Arr)
	arrObj.Arr = arrObj.Arr[:idx+1]
	arrObj.Arr[idx] = pushVal
	return nil
}

func (vm *VM) handleArrayPop(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_array_pop requires 1 argument")
	}
	arrVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arrVal)

	arrObj, vmErr := vm.arrayOwnedFromValue(arrVal)
	if vmErr != nil {
		return vmErr
	}

	if len(arrObj.Arr) == 0 {
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

	lastIdx := len(arrObj.Arr) - 1
	elem := arrObj.Arr[lastIdx]
	arrObj.Arr[lastIdx] = Value{}
	arrObj.Arr = arrObj.Arr[:lastIdx]

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

func (vm *VM) handleArrayGetMut(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_array_get_mut requires 2 arguments")
	}
	arrVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arrVal)
	idxVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(idxVal)

	if arrVal.Kind == VKRef || arrVal.Kind == VKRefMut {
		loaded, loadErr := vm.loadLocationRaw(arrVal.Loc)
		if loadErr != nil {
			return loadErr
		}
		arrVal = loaded
	}
	if arrVal.Kind != VKHandleArray {
		return vm.eb.typeMismatch("array", arrVal.Kind.String())
	}
	view, vmErr := vm.arrayViewFromHandle(arrVal.H)
	if vmErr != nil {
		return vmErr
	}
	idx, vmErr := vm.arrayIndexFromValue(idxVal, view.length)
	if vmErr != nil {
		return vmErr
	}
	baseIdx := view.start + idx
	idx32, err := safecast.Conv[int32](baseIdx)
	if err != nil {
		return vm.eb.invalidLocation("array index overflow")
	}

	elemType := types.NoTypeID
	if vm.Types != nil && arrVal.TypeID != types.NoTypeID {
		if t, ok := vm.Types.ArrayInfo(arrVal.TypeID); ok {
			elemType = t
		} else if t, _, ok := vm.Types.ArrayFixedInfo(arrVal.TypeID); ok {
			elemType = t
		} else if tt, ok := vm.Types.Lookup(arrVal.TypeID); ok && tt.Kind == types.KindArray {
			elemType = tt.Elem
		}
	}
	refType := types.NoTypeID
	if vm.Types != nil && elemType != types.NoTypeID {
		refType = vm.Types.Intern(types.MakeReference(elemType, true))
	}
	ref := MakeRefMut(Location{Kind: LKArrayElem, Handle: view.baseHandle, Index: idx32}, refType)

	dstLocal := call.Dst.Local
	if err := vm.writeLocal(frame, dstLocal, ref); err != nil {
		return err
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   ref,
		})
	}
	return nil
}

func (vm *VM) makeOptionSome(typeID types.TypeID, elem Value) (Value, *VMError) {
	if typeID == types.NoTypeID {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "invalid Option<T> type")
	}
	layout, vmErr := vm.tagLayoutFor(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	tc, ok := layout.CaseByName("Some")
	if !ok {
		return Value{}, vm.eb.unknownTagLayout(fmt.Sprintf("unknown tag %q in type#%d layout", "Some", layout.TypeID))
	}
	if len(tc.PayloadTypes) != 1 {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("tag %q expects 1 payload value, got %d", tc.TagName, len(tc.PayloadTypes)))
	}
	if elem.TypeID == types.NoTypeID && tc.PayloadTypes[0] != types.NoTypeID {
		elem.TypeID = tc.PayloadTypes[0]
	}
	h := vm.Heap.AllocTag(typeID, tc.TagSym, []Value{elem})
	return MakeHandleTag(h, typeID), nil
}

func (vm *VM) makeOptionNothing(typeID types.TypeID) (Value, *VMError) {
	if typeID == types.NoTypeID {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "invalid Option<T> type")
	}
	layout, vmErr := vm.tagLayoutFor(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	tc, ok := layout.CaseByName("nothing")
	if !ok {
		return Value{}, vm.eb.unknownTagLayout(fmt.Sprintf("unknown tag %q in type#%d layout", "nothing", layout.TypeID))
	}
	if len(tc.PayloadTypes) != 0 {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("tag %q expects 0 payload values, got %d", tc.TagName, len(tc.PayloadTypes)))
	}
	h := vm.Heap.AllocTag(typeID, tc.TagSym, nil)
	return MakeHandleTag(h, typeID), nil
}
