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

func (vm *VM) handleArrayAppendRawBytes(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	_ = writes
	if len(call.Args) != 3 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_array_append_raw_bytes requires 3 arguments")
	}
	arrVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arrVal)
	ptrVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(ptrVal)
	lenVal, vmErr := vm.evalOperand(frame, &call.Args[2])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(lenVal)

	if ptrVal.Kind != VKPtr {
		return vm.eb.typeMismatch("*byte", ptrVal.Kind.String())
	}
	n, vmErr := vm.uintValueToInt(lenVal, "array append length out of range")
	if vmErr != nil {
		return vmErr
	}
	if n == 0 {
		return nil
	}
	data, vmErr := vm.readBytesFromPointer(ptrVal, n)
	if vmErr != nil {
		return vmErr
	}
	arrObj, vmErr := vm.arrayOwnedFromValue(arrVal)
	if vmErr != nil {
		return vmErr
	}

	oldLen := len(arrObj.Arr)
	newLen := oldLen + len(data)
	if newLen < oldLen {
		return vm.eb.invalidNumericConversion("array length out of range")
	}
	if newLen > cap(arrObj.Arr) {
		grown := growArrayCapacity(cap(arrObj.Arr), newLen)
		next := make([]Value, newLen, grown)
		copy(next, arrObj.Arr)
		arrObj.Arr = next
	} else {
		arrObj.Arr = arrObj.Arr[:newLen]
	}

	elemType := types.NoTypeID
	if vm.Types != nil {
		elemType = vm.Types.Builtins().Uint8
	}
	for i, b := range data {
		arrObj.Arr[oldLen+i] = MakeInt(int64(b), elemType)
	}
	return nil
}

func (vm *VM) handleByteArrayAppendRange(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	_ = writes
	if len(call.Args) != 4 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_byte_array_append_range requires 4 arguments")
	}
	dstVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(dstVal)
	srcVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(srcVal)
	startVal, vmErr := vm.evalOperand(frame, &call.Args[2])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(startVal)
	lenVal, vmErr := vm.evalOperand(frame, &call.Args[3])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(lenVal)

	start, vmErr := vm.uintValueToInt(startVal, "byte array append start out of range")
	if vmErr != nil {
		return vmErr
	}
	length, vmErr := vm.uintValueToInt(lenVal, "byte array append length out of range")
	if vmErr != nil {
		return vmErr
	}
	if srcVal.Kind == VKRef || srcVal.Kind == VKRefMut {
		loaded, loadErr := vm.loadLocationRaw(srcVal.Loc)
		if loadErr != nil {
			return loadErr
		}
		srcVal = loaded
	}
	if srcVal.Kind != VKHandleArray {
		return vm.eb.typeMismatch("byte[]", srcVal.Kind.String())
	}
	srcView, vmErr := vm.arrayViewFromHandle(srcVal.H)
	if vmErr != nil {
		return vmErr
	}
	if start > srcView.length || length > srcView.length-start {
		return vm.eb.outOfBounds(start+length, srcView.length)
	}
	if length == 0 {
		return nil
	}

	data := make([]byte, length)
	for i := range length {
		b, convErr := vm.valueToUint8(srcView.baseObj.Arr[srcView.start+start+i])
		if convErr != nil {
			return convErr
		}
		data[i] = b
	}

	dstObj, vmErr := vm.arrayOwnedFromValue(dstVal)
	if vmErr != nil {
		return vmErr
	}
	oldLen := len(dstObj.Arr)
	newLen := oldLen + length
	if newLen < oldLen {
		return vm.eb.invalidNumericConversion("array length out of range")
	}
	if newLen > cap(dstObj.Arr) {
		grown := growArrayCapacity(cap(dstObj.Arr), newLen)
		next := make([]Value, newLen, grown)
		copy(next, dstObj.Arr)
		dstObj.Arr = next
	} else {
		dstObj.Arr = dstObj.Arr[:newLen]
	}

	elemType := types.NoTypeID
	if vm.Types != nil {
		elemType = vm.Types.Builtins().Uint8
	}
	for i, b := range data {
		dstObj.Arr[oldLen+i] = MakeInt(int64(b), elemType)
	}
	return nil
}

func (vm *VM) handleByteArrayDropPrefix(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	_ = writes
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_byte_array_drop_prefix requires 2 arguments")
	}
	arrVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arrVal)
	countVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(countVal)

	count, vmErr := vm.uintValueToInt(countVal, "byte array drop prefix count out of range")
	if vmErr != nil {
		return vmErr
	}
	if count == 0 {
		return nil
	}
	arrObj, vmErr := vm.arrayOwnedFromValue(arrVal)
	if vmErr != nil {
		return vmErr
	}
	if count > len(arrObj.Arr) {
		return vm.eb.outOfBounds(count, len(arrObj.Arr))
	}
	for i := range count {
		vm.dropValue(arrObj.Arr[i])
		arrObj.Arr[i] = Value{}
	}
	if count == len(arrObj.Arr) {
		arrObj.Arr = arrObj.Arr[:0]
		return nil
	}
	newLen := len(arrObj.Arr) - count
	for i := range newLen {
		arrObj.Arr[i] = arrObj.Arr[count+i]
	}
	for i := newLen; i < len(arrObj.Arr); i++ {
		arrObj.Arr[i] = Value{}
	}
	arrObj.Arr = arrObj.Arr[:newLen]
	return nil
}

func (vm *VM) handleByteParseUint64Token(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if len(call.Args) != 5 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_byte_parse_uint64_token requires 5 arguments")
	}
	dataVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(dataVal)
	startVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(startVal)
	endVal, vmErr := vm.evalOperand(frame, &call.Args[2])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(endVal)
	valueRef, vmErr := vm.evalOperand(frame, &call.Args[3])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(valueRef)
	nextRef, vmErr := vm.evalOperand(frame, &call.Args[4])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(nextRef)

	ok := false
	var value uint64
	next := 0
	start, startErr := vm.uintValueToInt(startVal, "byte parse start out of range")
	end, endErr := vm.uintValueToInt(endVal, "byte parse end out of range")
	if startErr != nil || endErr != nil {
		ok = false
	} else {
		next = start
		if dataVal.Kind == VKRef || dataVal.Kind == VKRefMut {
			loaded, loadErr := vm.loadLocationRaw(dataVal.Loc)
			if loadErr != nil {
				return loadErr
			}
			dataVal = loaded
		}
		if dataVal.Kind != VKHandleArray {
			return vm.eb.typeMismatch("byte[]", dataVal.Kind.String())
		}
		view, viewErr := vm.arrayViewFromHandle(dataVal.H)
		if viewErr != nil {
			return viewErr
		}
		if start <= end && start <= view.length && end <= view.length {
			next = end
			i := start
			for i < end {
				b, convErr := vm.valueToUint8(view.baseObj.Arr[view.start+i])
				if convErr != nil {
					return convErr
				}
				if b != 32 && b != 9 && b != 10 && b != 13 {
					break
				}
				i++
			}
			if i < end {
				value, next, ok, vmErr = vm.parseUint64DigitsInByteView(view, i, end)
				if vmErr != nil {
					return vmErr
				}
			}
		}
	}

	if valueRef.Kind != VKRefMut {
		return vm.eb.typeMismatch("&mut uint64", valueRef.Kind.String())
	}
	if nextRef.Kind != VKRefMut {
		return vm.eb.typeMismatch("&mut uint64", nextRef.Kind.String())
	}
	if vmErr := vm.storeLocation(valueRef.Loc, MakeInt(asInt64(value), vm.refElemType(valueRef.TypeID))); vmErr != nil {
		return vmErr
	}
	if vmErr := vm.storeLocation(nextRef.Loc, MakeInt(int64(next), vm.refElemType(nextRef.TypeID))); vmErr != nil {
		return vmErr
	}
	if call.HasDst {
		dstLocal := call.Dst.Local
		res := MakeBool(ok, frame.Locals[dstLocal].TypeID)
		if vmErr := vm.writeLocal(frame, dstLocal, res); vmErr != nil {
			return vmErr
		}
		if writes != nil {
			*writes = append(*writes, LocalWrite{
				LocalID: dstLocal,
				Name:    frame.Locals[dstLocal].Name,
				Value:   res,
			})
		}
	}
	return nil
}

func (vm *VM) parseUint64DigitsInByteView(view arrayView, start, end int) (value uint64, next int, ok bool, vmErr *VMError) {
	const maxUint64 = ^uint64(0)
	sawDigit := false
	i := start
	for i < end {
		b, vmErr := vm.valueToUint8(view.baseObj.Arr[view.start+i])
		if vmErr != nil {
			return 0, start, false, vmErr
		}
		if b >= '0' && b <= '9' {
			digit := uint64(b - '0')
			if value > maxUint64/10 || (value == maxUint64/10 && digit > maxUint64%10) {
				return 0, start, false, nil
			}
			value = value*10 + digit
			sawDigit = true
			i++
			continue
		}
		if b == 32 || b == 9 || b == 10 || b == 13 {
			break
		}
		return 0, start, false, nil
	}
	if !sawDigit {
		return 0, start, false, nil
	}
	return value, i, true, nil
}

func (vm *VM) refElemType(typeID types.TypeID) types.TypeID {
	if vm == nil || vm.Types == nil || typeID == types.NoTypeID {
		return types.NoTypeID
	}
	tt, ok := vm.Types.Lookup(vm.valueType(typeID))
	if !ok || tt.Kind != types.KindReference {
		return types.NoTypeID
	}
	return tt.Elem
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
