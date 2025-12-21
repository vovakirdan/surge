package vm

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"fortio.org/safecast"
	"golang.org/x/text/unicode/norm"

	"surge/internal/mir"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// callIntrinsic handles runtime intrinsic calls (and selected extern calls not lowered into MIR).
func (vm *VM) callIntrinsic(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	fullName := call.Callee.Name
	name := fullName
	if idx := strings.Index(fullName, "::<"); idx >= 0 {
		name = fullName[:idx]
	}

	if handled, vmErr := vm.callTagConstructor(frame, call, writes); handled {
		return vmErr
	}

	switch name {
	case "size_of", "align_of":
		if !call.HasDst {
			return nil
		}
		if vm.Layout == nil {
			return vm.eb.makeError(PanicUnimplemented, "no layout engine for size_of/align_of")
		}
		if vm.M == nil || vm.M.Meta == nil || len(vm.M.Meta.FuncTypeArgs) == 0 || !call.Callee.Sym.IsValid() {
			return vm.eb.makeError(PanicUnimplemented, "missing type arguments for size_of/align_of")
		}
		typeArgs, ok := vm.M.Meta.FuncTypeArgs[call.Callee.Sym]
		if !ok || len(typeArgs) != 1 || typeArgs[0] == types.NoTypeID {
			return vm.eb.makeError(PanicUnimplemented, "invalid type arguments for size_of/align_of")
		}
		tArg := typeArgs[0]

		n := 0
		var layoutErr error
		if name == "size_of" {
			n, layoutErr = vm.Layout.SizeOf(tArg)
		} else {
			n, layoutErr = vm.Layout.AlignOf(tArg)
		}
		if layoutErr != nil {
			return vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("layout error for type#%d: %v", tArg, layoutErr))
		}

		dstLocal := call.Dst.Local
		dstType := frame.Locals[dstLocal].TypeID
		u64, err := safecast.Conv[uint64](n)
		if err != nil {
			return vm.eb.invalidNumericConversion("size/align out of range")
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

	case "rt_argv":
		argv := vm.RT.Argv()
		strTy := types.NoTypeID
		if vm.Types != nil {
			strTy = vm.Types.Builtins().String
		}
		elems := make([]Value, 0, len(argv))
		for _, s := range argv {
			h := vm.Heap.AllocString(strTy, s)
			elems = append(elems, MakeHandleString(h, strTy))
		}
		arrTy := types.NoTypeID
		if call.HasDst {
			arrTy = frame.Locals[call.Dst.Local].TypeID
		}
		arrH := vm.Heap.AllocArray(arrTy, elems)
		val := MakeHandleArray(arrH, arrTy)
		if call.HasDst {
			localID := call.Dst.Local
			vmErr := vm.writeLocal(frame, localID, val)
			if vmErr != nil {
				return vmErr
			}
			*writes = append(*writes, LocalWrite{
				LocalID: localID,
				Name:    frame.Locals[localID].Name,
				Value:   val,
			})
		} else {
			// No destination; free the array and its string elements.
			vm.Heap.Release(arrH)
		}

	case "rt_stdin_read_all":
		stdin := vm.RT.StdinReadAll()
		strTy := types.NoTypeID
		if vm.Types != nil {
			strTy = vm.Types.Builtins().String
		}
		h := vm.Heap.AllocString(strTy, stdin)
		val := MakeHandleString(h, strTy)
		if call.HasDst {
			localID := call.Dst.Local
			vmErr := vm.writeLocal(frame, localID, val)
			if vmErr != nil {
				return vmErr
			}
			*writes = append(*writes, LocalWrite{
				LocalID: localID,
				Name:    frame.Locals[localID].Name,
				Value:   val,
			})
		} else {
			// No destination; free the string.
			vm.Heap.Release(h)
		}

	case "rt_string_ptr":
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
		var strVal Value
		switch arg.Kind {
		case VKHandleString:
			strVal = arg
		case VKRef, VKRefMut:
			v, loadErr := vm.loadLocationRaw(arg.Loc)
			if loadErr != nil {
				return loadErr
			}
			strVal = v
		default:
			return vm.eb.typeMismatch("&string", arg.Kind.String())
		}
		if strVal.Kind != VKHandleString {
			return vm.eb.typeMismatch("string", strVal.Kind.String())
		}
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

	case "rt_string_len":
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
		var strVal Value
		switch arg.Kind {
		case VKHandleString:
			strVal = arg
		case VKRef, VKRefMut:
			v, loadErr := vm.loadLocationRaw(arg.Loc)
			if loadErr != nil {
				return loadErr
			}
			strVal = v
		default:
			return vm.eb.typeMismatch("&string", arg.Kind.String())
		}
		if strVal.Kind != VKHandleString {
			return vm.eb.typeMismatch("string", strVal.Kind.String())
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

	case "rt_string_len_bytes":
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
		var strVal Value
		switch arg.Kind {
		case VKHandleString:
			strVal = arg
		case VKRef, VKRefMut:
			v, loadErr := vm.loadLocationRaw(arg.Loc)
			if loadErr != nil {
				return loadErr
			}
			strVal = v
		default:
			return vm.eb.typeMismatch("&string", arg.Kind.String())
		}
		if strVal.Kind != VKHandleString {
			return vm.eb.typeMismatch("string", strVal.Kind.String())
		}
		s := vm.Heap.Get(strVal.H).Str
		dstLocal := call.Dst.Local
		dstType := frame.Locals[dstLocal].TypeID
		u64, err := safecast.Conv[uint64](len(s))
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

	case "rt_string_from_bytes":
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

	case "rt_string_from_utf16":
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

	case "rt_string_bytes_view":
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
		var strVal Value
		switch arg.Kind {
		case VKHandleString:
			strVal = arg
		case VKRef, VKRefMut:
			v, loadErr := vm.loadLocationRaw(arg.Loc)
			if loadErr != nil {
				return loadErr
			}
			strVal = v
		default:
			return vm.eb.typeMismatch("&string", arg.Kind.String())
		}
		if strVal.Kind != VKHandleString {
			return vm.eb.typeMismatch("string", strVal.Kind.String())
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
		length := len(vm.Heap.Get(strVal.H).Str)
		u64, err := safecast.Conv[uint64](length)
		if err != nil {
			vm.dropValue(ownerVal)
			return vm.eb.invalidNumericConversion("bytes view length out of range")
		}
		fields[info.lenIdx] = vm.makeBigUint(info.layout.FieldTypes[info.lenIdx], bignum.UintFromUint64(u64))
		h := vm.Heap.AllocStruct(info.layout.TypeID, fields)
		val := MakeHandleStruct(h, dstType)
		if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})

	case "rt_range_int_new":
		if !call.HasDst {
			return nil
		}
		if len(call.Args) != 3 {
			return vm.eb.makeError(PanicTypeMismatch, "rt_range_int_new requires 3 arguments")
		}
		startVal, vmErr := vm.evalOperand(frame, &call.Args[0])
		if vmErr != nil {
			return vmErr
		}
		defer vm.dropValue(startVal)
		endVal, vmErr := vm.evalOperand(frame, &call.Args[1])
		if vmErr != nil {
			return vmErr
		}
		defer vm.dropValue(endVal)
		incVal, vmErr := vm.evalOperand(frame, &call.Args[2])
		if vmErr != nil {
			return vmErr
		}
		defer vm.dropValue(incVal)
		if incVal.Kind != VKBool {
			return vm.eb.typeMismatch("bool", incVal.Kind.String())
		}
		startStored, vmErr := vm.cloneForShare(startVal)
		if vmErr != nil {
			return vmErr
		}
		endStored, vmErr := vm.cloneForShare(endVal)
		if vmErr != nil {
			vm.dropValue(startStored)
			return vmErr
		}
		dstLocal := call.Dst.Local
		dstType := frame.Locals[dstLocal].TypeID
		h := vm.Heap.AllocRange(dstType, startStored, endStored, true, true, incVal.Bool)
		val := MakeHandleRange(h, dstType)
		if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})

	case "rt_range_int_from_start":
		if !call.HasDst {
			return nil
		}
		if len(call.Args) != 2 {
			return vm.eb.makeError(PanicTypeMismatch, "rt_range_int_from_start requires 2 arguments")
		}
		startVal, vmErr := vm.evalOperand(frame, &call.Args[0])
		if vmErr != nil {
			return vmErr
		}
		defer vm.dropValue(startVal)
		incVal, vmErr := vm.evalOperand(frame, &call.Args[1])
		if vmErr != nil {
			return vmErr
		}
		defer vm.dropValue(incVal)
		if incVal.Kind != VKBool {
			return vm.eb.typeMismatch("bool", incVal.Kind.String())
		}
		startStored, vmErr := vm.cloneForShare(startVal)
		if vmErr != nil {
			return vmErr
		}
		dstLocal := call.Dst.Local
		dstType := frame.Locals[dstLocal].TypeID
		h := vm.Heap.AllocRange(dstType, startStored, Value{}, true, false, incVal.Bool)
		val := MakeHandleRange(h, dstType)
		if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})

	case "rt_range_int_to_end":
		if !call.HasDst {
			return nil
		}
		if len(call.Args) != 2 {
			return vm.eb.makeError(PanicTypeMismatch, "rt_range_int_to_end requires 2 arguments")
		}
		endVal, vmErr := vm.evalOperand(frame, &call.Args[0])
		if vmErr != nil {
			return vmErr
		}
		defer vm.dropValue(endVal)
		incVal, vmErr := vm.evalOperand(frame, &call.Args[1])
		if vmErr != nil {
			return vmErr
		}
		defer vm.dropValue(incVal)
		if incVal.Kind != VKBool {
			return vm.eb.typeMismatch("bool", incVal.Kind.String())
		}
		endStored, vmErr := vm.cloneForShare(endVal)
		if vmErr != nil {
			return vmErr
		}
		dstLocal := call.Dst.Local
		dstType := frame.Locals[dstLocal].TypeID
		h := vm.Heap.AllocRange(dstType, Value{}, endStored, false, true, incVal.Bool)
		val := MakeHandleRange(h, dstType)
		if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})

	case "rt_range_int_full":
		if !call.HasDst {
			return nil
		}
		if len(call.Args) != 1 {
			return vm.eb.makeError(PanicTypeMismatch, "rt_range_int_full requires 1 argument")
		}
		incVal, vmErr := vm.evalOperand(frame, &call.Args[0])
		if vmErr != nil {
			return vmErr
		}
		defer vm.dropValue(incVal)
		if incVal.Kind != VKBool {
			return vm.eb.typeMismatch("bool", incVal.Kind.String())
		}
		dstLocal := call.Dst.Local
		dstType := frame.Locals[dstLocal].TypeID
		h := vm.Heap.AllocRange(dstType, Value{}, Value{}, false, false, incVal.Bool)
		val := MakeHandleRange(h, dstType)
		if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})

	case "rt_write_stdout":
		if len(call.Args) != 2 {
			return vm.eb.makeError(PanicTypeMismatch, "rt_write_stdout requires 2 arguments")
		}
		ptrVal, vmErr := vm.evalOperand(frame, &call.Args[0])
		if vmErr != nil {
			return vmErr
		}
		defer vm.dropValue(ptrVal)
		if ptrVal.Kind != VKPtr {
			return vm.eb.typeMismatch("*byte", ptrVal.Kind.String())
		}
		lenVal, vmErr := vm.evalOperand(frame, &call.Args[1])
		if vmErr != nil {
			return vmErr
		}
		defer vm.dropValue(lenVal)

		maxInt := int64(int(^uint(0) >> 1))
		maxUint := uint64(^uint(0) >> 1)
		n := 0
		switch lenVal.Kind {
		case VKInt:
			if lenVal.Int < 0 || lenVal.Int > maxInt {
				return vm.eb.invalidNumericConversion("stdout write length out of range")
			}
			ni, err := safecast.Conv[int](lenVal.Int)
			if err != nil {
				return vm.eb.invalidNumericConversion("stdout write length out of range")
			}
			n = ni
		case VKBigUint:
			u, vmErr := vm.mustBigUint(lenVal)
			if vmErr != nil {
				return vmErr
			}
			uv, ok := u.Uint64()
			if !ok || uv > maxUint {
				return vm.eb.invalidNumericConversion("stdout write length out of range")
			}
			ni, err := safecast.Conv[int](uv)
			if err != nil {
				return vm.eb.invalidNumericConversion("stdout write length out of range")
			}
			n = ni
		default:
			return vm.eb.typeMismatch("uint", lenVal.Kind.String())
		}

		if ptrVal.Loc.Kind != LKStringBytes {
			return vm.eb.invalidLocation("rt_write_stdout: unsupported pointer kind")
		}
		obj := vm.Heap.Get(ptrVal.Loc.Handle)
		if obj.Kind != OKString {
			return vm.eb.typeMismatch("string bytes pointer", fmt.Sprintf("%v", obj.Kind))
		}
		s := obj.Str
		off := int(ptrVal.Loc.ByteOffset)
		end64 := int64(off) + int64(n)
		if off < 0 || off > len(s) || end64 < 0 || end64 > int64(len(s)) {
			return vm.eb.outOfBounds(int(end64), len(s))
		}
		end := int(end64)
		written, err := os.Stdout.WriteString(s[off:end])
		if err != nil {
			written = 0
		}

		if call.HasDst {
			dstLocal := call.Dst.Local
			dstType := frame.Locals[dstLocal].TypeID
			u64, err := safecast.Conv[uint64](written)
			if err != nil {
				return vm.eb.invalidNumericConversion("stdout written count out of range")
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
		}

	case "rt_exit":
		code := 0
		if len(call.Args) > 0 {
			val, vmErr := vm.evalOperand(frame, &call.Args[0])
			if vmErr != nil {
				return vmErr
			}
			switch val.Kind {
			case VKInt:
				code = int(val.Int)
			case VKBigInt:
				i, vmErr := vm.mustBigInt(val)
				if vmErr != nil {
					return vmErr
				}
				n, ok := i.Int64()
				if !ok {
					return vm.eb.invalidNumericConversion("exit code out of range")
				}
				code = int(n)
			default:
				vm.dropValue(val)
				return vm.eb.typeMismatch("int", val.Kind.String())
			}
			// Ensure the exit code value doesn't keep heap objects alive across leak checking.
			vm.dropValue(val)
		}
		vm.ExitCode = code
		vm.RT.Exit(code)

		// Drop all frames to ensure owned values are freed before leak check.
		vm.dropAllFrames()
		vm.checkLeaksOrPanic()

		vm.Halted = true
		vm.Stack = nil

	case "rt_parse_arg":
		if len(call.Args) == 0 {
			return vm.eb.makeError(PanicTypeMismatch, "rt_parse_arg requires 1 argument")
		}
		strVal, vmErr := vm.evalOperand(frame, &call.Args[0])
		if vmErr != nil {
			return vmErr
		}
		if strVal.Kind != VKHandleString {
			vm.dropValue(strVal)
			return vm.eb.typeMismatch("string", strVal.Kind.String())
		}

		if !call.HasDst {
			vm.dropValue(strVal)
			return nil
		}

		dstLocal := call.Dst.Local
		dstType := frame.Locals[dstLocal].TypeID
		dstValueType := vm.valueType(dstType)
		if vm.Types == nil {
			return vm.eb.makeError(PanicUnimplemented, "rt_parse_arg requires type information")
		}
		tt, ok := vm.Types.Lookup(dstValueType)
		if !ok {
			return vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("rt_parse_arg: unknown destination type type#%d", dstValueType))
		}

		switch tt.Kind {
		case types.KindString:
			strVal.TypeID = dstType
			vmErr = vm.writeLocal(frame, dstLocal, strVal)
			if vmErr != nil {
				vm.dropValue(strVal)
				return vmErr
			}
			*writes = append(*writes, LocalWrite{
				LocalID: dstLocal,
				Name:    frame.Locals[dstLocal].Name,
				Value:   strVal,
			})
			return nil

		case types.KindInt:
			if tt.Width != types.WidthAny {
				vm.dropValue(strVal)
				return vm.eb.unsupportedParseType("fixed-width int")
			}
			s := vm.Heap.Get(strVal.H).Str
			vm.dropValue(strVal)
			i, err := bignum.ParseInt(s)
			if err != nil {
				return vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("failed to parse %q as int: %v", s, err))
			}
			val := vm.makeBigInt(dstType, i)
			vmErr = vm.writeLocal(frame, dstLocal, val)
			if vmErr != nil {
				return vmErr
			}
			*writes = append(*writes, LocalWrite{
				LocalID: dstLocal,
				Name:    frame.Locals[dstLocal].Name,
				Value:   val,
			})
			return nil

		case types.KindUint:
			if tt.Width != types.WidthAny {
				vm.dropValue(strVal)
				return vm.eb.unsupportedParseType("fixed-width uint")
			}
			s := vm.Heap.Get(strVal.H).Str
			vm.dropValue(strVal)
			u, err := bignum.ParseUint(s)
			if err != nil {
				return vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("failed to parse %q as uint: %v", s, err))
			}
			val := vm.makeBigUint(dstType, u)
			vmErr = vm.writeLocal(frame, dstLocal, val)
			if vmErr != nil {
				return vmErr
			}
			*writes = append(*writes, LocalWrite{
				LocalID: dstLocal,
				Name:    frame.Locals[dstLocal].Name,
				Value:   val,
			})
			return nil

		case types.KindFloat:
			if tt.Width != types.WidthAny {
				vm.dropValue(strVal)
				return vm.eb.unsupportedParseType("fixed-width float")
			}
			s := vm.Heap.Get(strVal.H).Str
			vm.dropValue(strVal)
			f, err := bignum.ParseFloat(s)
			if err != nil {
				return vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("failed to parse %q as float: %v", s, err))
			}
			val := vm.makeBigFloat(dstType, f)
			vmErr = vm.writeLocal(frame, dstLocal, val)
			if vmErr != nil {
				return vmErr
			}
			*writes = append(*writes, LocalWrite{
				LocalID: dstLocal,
				Name:    frame.Locals[dstLocal].Name,
				Value:   val,
			})
			return nil

		default:
			vm.dropValue(strVal)
			return vm.eb.unsupportedParseType(tt.Kind.String())
		}

	case "__len":
		if !call.HasDst {
			return vm.eb.makeError(PanicTypeMismatch, "__len requires a destination")
		}
		if len(call.Args) != 1 {
			return vm.eb.makeError(PanicTypeMismatch, "__len requires 1 argument")
		}
		arg, vmErr := vm.evalOperand(frame, &call.Args[0])
		if vmErr != nil {
			return vmErr
		}
		defer vm.dropValue(arg)
		if arg.Kind == VKRef || arg.Kind == VKRefMut {
			v, loadErr := vm.loadLocationRaw(arg.Loc)
			if loadErr != nil {
				return loadErr
			}
			arg = v
		}
		dstLocal := call.Dst.Local
		dstType := frame.Locals[dstLocal].TypeID
		switch arg.Kind {
		case VKHandleString:
			obj := vm.Heap.Get(arg.H)
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
		case VKHandleArray:
			obj := vm.Heap.Get(arg.H)
			if obj.Kind != OKArray {
				return vm.eb.typeMismatch("array", fmt.Sprintf("%v", obj.Kind))
			}
			u64, err := safecast.Conv[uint64](len(obj.Arr))
			if err != nil {
				return vm.eb.invalidNumericConversion("array length out of range")
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
		case VKHandleStruct:
			info, vmErr := vm.bytesViewLayout(arg.TypeID)
			if vmErr != nil {
				return vmErr
			}
			if !info.ok {
				return vm.eb.typeMismatch("len-compatible", arg.Kind.String())
			}
			obj := vm.Heap.Get(arg.H)
			if obj.Kind != OKStruct {
				return vm.eb.typeMismatch("struct", fmt.Sprintf("%v", obj.Kind))
			}
			if info.ownerIdx < 0 || info.ownerIdx >= len(obj.Fields) || info.lenIdx < 0 || info.lenIdx >= len(obj.Fields) {
				return vm.eb.makeError(PanicOutOfBounds, "bytes view layout mismatch")
			}
			lenVal, vmErr := vm.cloneForShare(obj.Fields[info.lenIdx])
			if vmErr != nil {
				return vmErr
			}
			lenVal.TypeID = dstType
			if vmErr := vm.writeLocal(frame, dstLocal, lenVal); vmErr != nil {
				vm.dropValue(lenVal)
				return vmErr
			}
			*writes = append(*writes, LocalWrite{
				LocalID: dstLocal,
				Name:    frame.Locals[dstLocal].Name,
				Value:   lenVal,
			})
		default:
			return vm.eb.typeMismatch("len-compatible", arg.Kind.String())
		}

	case "__index":
		if !call.HasDst {
			return vm.eb.makeError(PanicTypeMismatch, "__index requires a destination")
		}
		if len(call.Args) != 2 {
			return vm.eb.makeError(PanicTypeMismatch, "__index requires 2 arguments")
		}
		objVal, vmErr := vm.evalOperand(frame, &call.Args[0])
		if vmErr != nil {
			return vmErr
		}
		defer vm.dropValue(objVal)
		idxVal, vmErr := vm.evalOperand(frame, &call.Args[1])
		if vmErr != nil {
			return vmErr
		}
		defer vm.dropValue(idxVal)
		if objVal.Kind == VKRef || objVal.Kind == VKRefMut {
			v, loadErr := vm.loadLocationRaw(objVal.Loc)
			if loadErr != nil {
				return loadErr
			}
			objVal = v
		}
		res, vmErr := vm.evalIndex(objVal, idxVal)
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

	case "__to":
		if !call.HasDst {
			return vm.eb.makeError(PanicTypeMismatch, "__to requires a destination")
		}
		if len(call.Args) != 1 {
			return vm.eb.makeError(PanicTypeMismatch, "__to requires 1 argument")
		}
		srcVal, vmErr := vm.evalOperand(frame, &call.Args[0])
		if vmErr != nil {
			return vmErr
		}
		dstLocal := call.Dst.Local
		dstTy := frame.Locals[dstLocal].TypeID

		converted, vmErr := vm.evalIntrinsicTo(srcVal, dstTy)
		if vmErr != nil {
			vm.dropValue(srcVal)
			return vmErr
		}
		vmErr = vm.writeLocal(frame, dstLocal, converted)
		if vmErr != nil {
			if !(srcVal.IsHeap() && converted.IsHeap() && srcVal.Kind == converted.Kind && srcVal.H == converted.H) {
				vm.dropValue(srcVal)
			}
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   converted,
		})
		if !(srcVal.IsHeap() && converted.IsHeap() && srcVal.Kind == converted.Kind && srcVal.H == converted.H) {
			vm.dropValue(srcVal)
		}

	default:
		return vm.eb.unsupportedIntrinsic(name)
	}

	return nil
}

func (vm *VM) readBytesFromPointer(ptrVal Value, n int) ([]byte, *VMError) {
	if n < 0 {
		return nil, vm.eb.invalidNumericConversion("byte length out of range")
	}
	switch ptrVal.Loc.Kind {
	case LKStringBytes:
		obj := vm.Heap.Get(ptrVal.Loc.Handle)
		if obj.Kind != OKString {
			return nil, vm.eb.typeMismatch("string bytes pointer", fmt.Sprintf("%v", obj.Kind))
		}
		off := int(ptrVal.Loc.ByteOffset)
		end := off + n
		if off < 0 || end < off || end > len(obj.Str) {
			return nil, vm.eb.outOfBounds(end, len(obj.Str))
		}
		return []byte(obj.Str[off:end]), nil
	case LKArrayElem:
		obj := vm.Heap.Get(ptrVal.Loc.Handle)
		if obj.Kind != OKArray {
			return nil, vm.eb.typeMismatch("array bytes pointer", fmt.Sprintf("%v", obj.Kind))
		}
		start := int(ptrVal.Loc.Index)
		end := start + n
		if start < 0 || end < start || end > len(obj.Arr) {
			return nil, vm.eb.outOfBounds(end, len(obj.Arr))
		}
		out := make([]byte, n)
		for i := range n {
			b, vmErr := vm.valueToUint8(obj.Arr[start+i])
			if vmErr != nil {
				return nil, vmErr
			}
			out[i] = b
		}
		return out, nil
	default:
		return nil, vm.eb.invalidLocation("unsupported pointer kind")
	}
}

func (vm *VM) readUint16sFromPointer(ptrVal Value, n int) ([]uint16, *VMError) {
	if n < 0 {
		return nil, vm.eb.invalidNumericConversion("uint16 length out of range")
	}
	switch ptrVal.Loc.Kind {
	case LKArrayElem:
		obj := vm.Heap.Get(ptrVal.Loc.Handle)
		if obj.Kind != OKArray {
			return nil, vm.eb.typeMismatch("array uint16 pointer", fmt.Sprintf("%v", obj.Kind))
		}
		start := int(ptrVal.Loc.Index)
		end := start + n
		if start < 0 || end < start || end > len(obj.Arr) {
			return nil, vm.eb.outOfBounds(end, len(obj.Arr))
		}
		out := make([]uint16, n)
		for i := range n {
			u, vmErr := vm.valueToUint16(obj.Arr[start+i])
			if vmErr != nil {
				return nil, vmErr
			}
			out[i] = u
		}
		return out, nil
	default:
		return nil, vm.eb.invalidLocation("unsupported pointer kind")
	}
}

func (vm *VM) valueToUint8(v Value) (byte, *VMError) {
	switch v.Kind {
	case VKInt:
		if v.Int < 0 || v.Int > 255 {
			return 0, vm.eb.invalidNumericConversion("byte out of range")
		}
		return byte(v.Int), nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(v)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok || n > 255 {
			return 0, vm.eb.invalidNumericConversion("byte out of range")
		}
		return byte(n), nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(v)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok || n < 0 || n > 255 {
			return 0, vm.eb.invalidNumericConversion("byte out of range")
		}
		return byte(n), nil
	default:
		return 0, vm.eb.typeMismatch("uint8", v.Kind.String())
	}
}

func (vm *VM) valueToUint16(v Value) (uint16, *VMError) {
	switch v.Kind {
	case VKInt:
		if v.Int < 0 || v.Int > 65535 {
			return 0, vm.eb.invalidNumericConversion("uint16 out of range")
		}
		return uint16(v.Int), nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(v)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok || n > 65535 {
			return 0, vm.eb.invalidNumericConversion("uint16 out of range")
		}
		return uint16(n), nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(v)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok || n < 0 || n > 65535 {
			return 0, vm.eb.invalidNumericConversion("uint16 out of range")
		}
		return uint16(n), nil
	default:
		return 0, vm.eb.typeMismatch("uint16", v.Kind.String())
	}
}

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
