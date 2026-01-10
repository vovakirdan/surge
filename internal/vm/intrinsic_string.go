package vm

import (
	"unicode/utf8"

	"fortio.org/safecast"
	"golang.org/x/text/unicode/norm"

	"surge/internal/mir"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// handleStringPtr handles the rt_string_ptr intrinsic.
func (vm *VM) handleStringPtr(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_string_ptr requires 1 argument")
	}
	arg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arg)
	strVal, vmErr := vm.extractStringValue(arg)
	if vmErr != nil {
		return vmErr
	}
	vm.stringBytes(vm.Heap.Get(strVal.H))
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	ptr := MakePtr(Location{Kind: LKStringBytes, Handle: strVal.H}, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, ptr); vmErr != nil {
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   ptr,
	})
	return nil
}

// handleStringLen handles the rt_string_len intrinsic.
func (vm *VM) handleStringLen(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_string_len requires 1 argument")
	}
	arg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arg)
	strVal, vmErr := vm.extractStringValue(arg)
	if vmErr != nil {
		return vmErr
	}
	obj := vm.Heap.Get(strVal.H)
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	u64, err := safecast.Conv[uint64](vm.stringCPLen(obj))
	if err != nil {
		return vm.eb.invalidNumericConversion("string length out of range")
	}
	val := vm.makeBigUint(dstType, bignum.UintFromUint64(u64))
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   val,
	})
	return nil
}

// handleStringLenBytes handles the rt_string_len_bytes intrinsic.
func (vm *VM) handleStringLenBytes(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_string_len_bytes requires 1 argument")
	}
	arg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arg)
	strVal, vmErr := vm.extractStringValue(arg)
	if vmErr != nil {
		return vmErr
	}
	sz := vm.stringByteLen(vm.Heap.Get(strVal.H))
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	u64, err := safecast.Conv[uint64](sz)
	if err != nil {
		return vm.eb.invalidNumericConversion("string length out of range")
	}
	val := vm.makeBigUint(dstType, bignum.UintFromUint64(u64))
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   val,
	})
	return nil
}

// handleStringFromBytes handles the rt_string_from_bytes intrinsic.
func (vm *VM) handleStringFromBytes(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_string_from_bytes requires 2 arguments")
	}
	ptrVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(ptrVal)
	lenVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(lenVal)
	if ptrVal.Kind != VKPtr {
		return vm.eb.typeMismatch("*byte", ptrVal.Kind.String())
	}
	n, vmErr := vm.uintValueToInt(lenVal, "string length out of range")
	if vmErr != nil {
		return vmErr
	}
	raw, vmErr := vm.readBytesFromPointer(ptrVal, n)
	if vmErr != nil {
		return vmErr
	}
	if !utf8.Valid(raw) {
		return vm.eb.makeError(PanicTypeMismatch, "invalid UTF-8")
	}
	str := norm.NFC.String(string(raw))
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	h := vm.Heap.AllocString(dstType, str)
	val := MakeHandleString(h, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   val,
	})
	return nil
}

// handleFromBytes handles built-in from_bytes(&byte[]) -> Erring<string, Error>.
func (vm *VM) handleFromBytes(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "from_bytes requires 1 argument")
	}
	if !call.HasDst {
		return nil
	}
	arrVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	borrowed := arrVal.Kind == VKRef || arrVal.Kind == VKRefMut
	if arrVal.Kind == VKRef || arrVal.Kind == VKRefMut {
		loaded, loadErr := vm.loadLocationRaw(arrVal.Loc)
		if loadErr != nil {
			return loadErr
		}
		arrVal = loaded
	}
	if arrVal.Kind != VKHandleArray {
		if !borrowed {
			vm.dropValue(arrVal)
		}
		return vm.eb.typeMismatch("byte[]", arrVal.Kind.String())
	}
	if borrowed {
		arrVal, vmErr = vm.cloneForShare(arrVal)
		if vmErr != nil {
			return vmErr
		}
	}
	defer vm.dropValue(arrVal)

	view, vmErr := vm.arrayViewFromHandle(arrVal.H)
	if vmErr != nil {
		return vmErr
	}
	raw := make([]byte, view.length)
	for i := range raw {
		b, convErr := vm.valueToUint8(view.baseObj.Arr[view.start+i])
		if convErr != nil {
			return convErr
		}
		raw[i] = b
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		return vmErr
	}
	successCase, ok := layout.CaseByName("Success")
	if !ok || len(successCase.PayloadTypes) != 1 || !vm.isStringType(successCase.PayloadTypes[0]) {
		return vm.eb.makeError(PanicTypeMismatch, "from_bytes requires Erring<string, Error> destination")
	}

	if !utf8.Valid(raw) {
		errType, vmErr := vm.erringErrorType(dstType)
		if vmErr != nil {
			return vmErr
		}
		errVal, vmErr := vm.makeErrorLikeValue(errType, "invalid UTF-8", 1)
		if vmErr != nil {
			return vmErr
		}
		if vmErr := vm.writeLocal(frame, dstLocal, errVal); vmErr != nil {
			vm.dropValue(errVal)
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   errVal,
		})
		return nil
	}

	str := norm.NFC.String(string(raw))
	payloadType := successCase.PayloadTypes[0]
	h := vm.Heap.AllocString(payloadType, str)
	payload := MakeHandleString(h, payloadType)
	tag := vm.Heap.AllocTag(dstType, successCase.TagSym, []Value{payload})
	tagVal := MakeHandleTag(tag, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, tagVal); vmErr != nil {
		vm.dropValue(tagVal)
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   tagVal,
	})
	return nil
}

// handleStringFromUTF16 handles the rt_string_from_utf16 intrinsic.
func (vm *VM) handleStringFromUTF16(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_string_from_utf16 requires 2 arguments")
	}
	ptrVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(ptrVal)
	lenVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(lenVal)
	if ptrVal.Kind != VKPtr {
		return vm.eb.typeMismatch("*uint16", ptrVal.Kind.String())
	}
	n, vmErr := vm.uintValueToInt(lenVal, "string length out of range")
	if vmErr != nil {
		return vmErr
	}
	units, vmErr := vm.readUint16sFromPointer(ptrVal, n)
	if vmErr != nil {
		return vmErr
	}
	decoded, ok := decodeUTF16Strict(units)
	if !ok {
		return vm.eb.makeError(PanicTypeMismatch, "invalid UTF-16")
	}
	str := norm.NFC.String(decoded)
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	h := vm.Heap.AllocString(dstType, str)
	val := MakeHandleString(h, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   val,
	})
	return nil
}

// handleStringIndex handles the rt_string_index intrinsic.
func (vm *VM) handleStringIndex(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_string_index requires 2 arguments")
	}
	arg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arg)
	strVal, vmErr := vm.extractStringValue(arg)
	if vmErr != nil {
		return vmErr
	}
	idxVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(idxVal)
	res, vmErr := vm.evalStringIndex(strVal, idxVal)
	if vmErr != nil {
		return vmErr
	}
	dstLocal := call.Dst.Local
	if res.TypeID == types.NoTypeID {
		res.TypeID = frame.Locals[dstLocal].TypeID
	}
	if vmErr := vm.writeLocal(frame, dstLocal, res); vmErr != nil {
		if res.IsHeap() {
			vm.dropValue(res)
		}
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   res,
	})
	return nil
}

// handleStringSlice handles the rt_string_slice intrinsic.
func (vm *VM) handleStringSlice(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_string_slice requires 2 arguments")
	}
	arg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arg)
	strVal, vmErr := vm.extractStringValue(arg)
	if vmErr != nil {
		return vmErr
	}
	idxVal, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(idxVal)
	res, vmErr := vm.evalStringIndex(strVal, idxVal)
	if vmErr != nil {
		return vmErr
	}
	dstLocal := call.Dst.Local
	if res.TypeID == types.NoTypeID {
		res.TypeID = frame.Locals[dstLocal].TypeID
	}
	if vmErr := vm.writeLocal(frame, dstLocal, res); vmErr != nil {
		if res.IsHeap() {
			vm.dropValue(res)
		}
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   res,
	})
	return nil
}

// handleStringForceFlatten handles the rt_string_force_flatten intrinsic.
func (vm *VM) handleStringForceFlatten(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_string_force_flatten requires 1 argument")
	}
	arg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arg)
	strVal, vmErr := vm.extractStringValue(arg)
	if vmErr != nil {
		return vmErr
	}
	_ = vm.stringBytes(vm.Heap.Get(strVal.H))
	if call.HasDst {
		dstLocal := call.Dst.Local
		val := MakeNothing()
		if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})
	}
	return nil
}

// handleStringBytesView handles the rt_string_bytes_view intrinsic.
func (vm *VM) handleStringBytesView(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_string_bytes_view requires 1 argument")
	}
	arg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(arg)
	strVal, vmErr := vm.extractStringValue(arg)
	if vmErr != nil {
		return vmErr
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	info, vmErr := vm.bytesViewLayout(dstType)
	if vmErr != nil {
		return vmErr
	}
	if !info.ok {
		return vm.eb.makeError(PanicTypeMismatch, "invalid BytesView layout")
	}
	fields := make([]Value, len(info.layout.FieldNames))
	ownerVal, vmErr := vm.cloneForShare(strVal)
	if vmErr != nil {
		return vmErr
	}
	fields[info.ownerIdx] = ownerVal
	fields[info.ptrIdx] = MakePtr(Location{Kind: LKStringBytes, Handle: strVal.H}, info.layout.FieldTypes[info.ptrIdx])
	length := vm.stringByteLen(vm.Heap.Get(strVal.H))
	u64, err := safecast.Conv[uint64](length)
	if err != nil {
		vm.dropValue(ownerVal)
		return vm.eb.invalidNumericConversion("bytes view length out of range")
	}
	fields[info.lenIdx] = vm.makeBigUint(info.layout.FieldTypes[info.lenIdx], bignum.UintFromUint64(u64))
	h := vm.Heap.AllocStruct(info.layout.TypeID, fields)
	val := MakeHandleStruct(h, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		vm.Heap.Release(h)
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   val,
	})
	return nil
}

// handleStringConcat handles the rt_string_concat intrinsic.
func (vm *VM) handleStringConcat(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_string_concat requires 2 arguments")
	}
	leftArg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(leftArg)
	rightArg, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(rightArg)

	leftVal, vmErr := vm.extractStringValue(leftArg)
	if vmErr != nil {
		return vmErr
	}
	rightVal, vmErr := vm.extractStringValue(rightArg)
	if vmErr != nil {
		return vmErr
	}
	res, vmErr := vm.concatStringValues(leftVal, rightVal)
	if vmErr != nil {
		return vmErr
	}
	if !call.HasDst {
		vm.dropValue(res)
		return nil
	}
	dstLocal := call.Dst.Local
	if res.TypeID == types.NoTypeID {
		res.TypeID = frame.Locals[dstLocal].TypeID
	}
	if vmErr := vm.writeLocal(frame, dstLocal, res); vmErr != nil {
		vm.dropValue(res)
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   res,
	})
	return nil
}

// handleStringEq handles the rt_string_eq intrinsic.
func (vm *VM) handleStringEq(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return vm.eb.makeError(PanicTypeMismatch, "rt_string_eq requires a destination")
	}
	if len(call.Args) != 2 {
		return vm.eb.makeError(PanicTypeMismatch, "rt_string_eq requires 2 arguments")
	}
	leftArg, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(leftArg)
	rightArg, vmErr := vm.evalOperand(frame, &call.Args[1])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(rightArg)

	leftVal, vmErr := vm.extractStringValue(leftArg)
	if vmErr != nil {
		return vmErr
	}
	rightVal, vmErr := vm.extractStringValue(rightArg)
	if vmErr != nil {
		return vmErr
	}
	leftObj := vm.Heap.Get(leftVal.H)
	rightObj := vm.Heap.Get(rightVal.H)
	if leftObj == nil || rightObj == nil {
		return vm.eb.makeError(PanicOutOfBounds, "invalid string handle")
	}
	eq := vm.stringBytes(leftObj) == vm.stringBytes(rightObj)

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	val := MakeBool(eq, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   val,
	})
	return nil
}

// extractStringValue extracts a string value from an argument (handling refs).
func (vm *VM) extractStringValue(arg Value) (Value, *VMError) {
	var strVal Value
	switch arg.Kind {
	case VKHandleString:
		strVal = arg
	case VKRef, VKRefMut:
		v, loadErr := vm.loadLocationRaw(arg.Loc)
		if loadErr != nil {
			return Value{}, loadErr
		}
		strVal = v
	default:
		return Value{}, vm.eb.typeMismatch("&string", arg.Kind.String())
	}
	if strVal.Kind != VKHandleString {
		return Value{}, vm.eb.typeMismatch("string", strVal.Kind.String())
	}
	return strVal, nil
}

// decodeUTF16Strict decodes a UTF-16 sequence strictly, rejecting invalid sequences.
func decodeUTF16Strict(units []uint16) (string, bool) {
	if len(units) == 0 {
		return "", true
	}
	runes := make([]rune, 0, len(units))
	for i := 0; i < len(units); i++ {
		u := units[i]
		switch {
		case u >= 0xD800 && u <= 0xDBFF:
			if i+1 >= len(units) {
				return "", false
			}
			lo := units[i+1]
			if lo < 0xDC00 || lo > 0xDFFF {
				return "", false
			}
			code := 0x10000 + ((uint32(u) - 0xD800) << 10) + (uint32(lo) - 0xDC00)
			runes = append(runes, rune(code))
			i++
		case u >= 0xDC00 && u <= 0xDFFF:
			return "", false
		default:
			runes = append(runes, rune(u))
		}
	}
	return string(runes), true
}
