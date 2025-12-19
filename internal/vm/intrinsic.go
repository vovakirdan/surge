package vm

import (
	"fmt"
	"os"
	"strings"

	"fortio.org/safecast"

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
		if name == "size_of" {
			n = vm.Layout.SizeOf(tArg)
		} else {
			n = vm.Layout.AlignOf(tArg)
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
			v, vmErr := vm.loadLocationRaw(arg.Loc)
			if vmErr != nil {
				return vmErr
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
			v, vmErr := vm.loadLocationRaw(arg.Loc)
			if vmErr != nil {
				return vmErr
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

func (vm *VM) evalIntrinsicTo(src Value, dstType types.TypeID) (Value, *VMError) {
	if dstType == types.NoTypeID {
		return src, nil
	}
	if vm.Types == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "__to requires type information")
	}

	dstValTy := vm.valueType(dstType)
	dstTT, ok := vm.Types.Lookup(dstValTy)
	if !ok {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("__to: unknown destination type type#%d", dstValTy))
	}

	// Legacy: allow custom exit code structs with `code: int`.
	if dstValTy == vm.Types.Builtins().Int && src.Kind == VKHandleStruct {
		obj := vm.Heap.Get(src.H)
		layout, vmErr := vm.layouts.Struct(obj.TypeID)
		if vmErr != nil {
			return Value{}, vmErr
		}
		idx, ok := layout.IndexByName["code"]
		if !ok {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("type#%d has no field \"code\" for __to(int)", obj.TypeID))
		}
		if idx < 0 || idx >= len(obj.Fields) {
			return Value{}, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("field index %d out of bounds for type#%d", idx, obj.TypeID))
		}
		field := obj.Fields[idx]
		switch field.Kind {
		case VKBigInt:
			// Convert by retaining the field, then letting the caller drop the struct.
			out, vmErr := vm.cloneForShare(field)
			if vmErr != nil {
				return Value{}, vmErr
			}
			out.TypeID = dstType
			return out, nil
		case VKInt:
			return vm.makeBigInt(dstType, bignum.IntFromInt64(field.Int)), nil
		default:
			return Value{}, vm.eb.typeMismatch("int", field.Kind.String())
		}
	}

	switch dstTT.Kind {
	case types.KindString:
		strTy := dstType
		switch src.Kind {
		case VKHandleString:
			src.TypeID = strTy
			return src, nil
		case VKBigInt:
			i, vmErr := vm.mustBigInt(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			h := vm.Heap.AllocString(strTy, bignum.FormatInt(i))
			return MakeHandleString(h, strTy), nil
		case VKBigUint:
			u, vmErr := vm.mustBigUint(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			h := vm.Heap.AllocString(strTy, bignum.FormatUint(u))
			return MakeHandleString(h, strTy), nil
		case VKBigFloat:
			f, vmErr := vm.mustBigFloat(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			s, err := bignum.FormatFloat(f)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			h := vm.Heap.AllocString(strTy, s)
			return MakeHandleString(h, strTy), nil
		case VKBool:
			s := "false"
			if src.Bool {
				s = "true"
			}
			h := vm.Heap.AllocString(strTy, s)
			return MakeHandleString(h, strTy), nil
		case VKInt:
			h := vm.Heap.AllocString(strTy, bignum.FormatInt(bignum.IntFromInt64(src.Int)))
			return MakeHandleString(h, strTy), nil
		default:
			return Value{}, vm.eb.unimplemented("__to to string")
		}

	case types.KindInt:
		if dstTT.Width != types.WidthAny {
			return Value{}, vm.eb.unimplemented("__to to fixed-width int")
		}
		switch src.Kind {
		case VKBigInt:
			src.TypeID = dstType
			return src, nil
		case VKBigUint:
			u, vmErr := vm.mustBigUint(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return vm.makeBigInt(dstType, bignum.BigInt{Limbs: u.Limbs}), nil
		case VKBigFloat:
			f, vmErr := vm.mustBigFloat(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			i, err := bignum.FloatToIntTrunc(f)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigInt(dstType, i), nil
		case VKHandleString:
			s := vm.Heap.Get(src.H).Str
			i, err := bignum.ParseInt(s)
			if err != nil {
				return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("failed to parse %q as int: %v", s, err))
			}
			return vm.makeBigInt(dstType, i), nil
		case VKInt:
			return vm.makeBigInt(dstType, bignum.IntFromInt64(src.Int)), nil
		case VKBool:
			n := int64(0)
			if src.Bool {
				n = 1
			}
			return vm.makeBigInt(dstType, bignum.IntFromInt64(n)), nil
		default:
			return Value{}, vm.eb.unimplemented("__to to int")
		}

	case types.KindUint:
		if dstTT.Width != types.WidthAny {
			return Value{}, vm.eb.unimplemented("__to to fixed-width uint")
		}
		switch src.Kind {
		case VKBigUint:
			src.TypeID = dstType
			return src, nil
		case VKBigInt:
			i, vmErr := vm.mustBigInt(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			if i.Neg && !i.IsZero() {
				return Value{}, vm.eb.invalidNumericConversion("cannot convert negative int to uint")
			}
			return vm.makeBigUint(dstType, i.Abs()), nil
		case VKBigFloat:
			f, vmErr := vm.mustBigFloat(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			u, err := bignum.FloatToUintTrunc(f)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigUint(dstType, u), nil
		case VKHandleString:
			s := vm.Heap.Get(src.H).Str
			u, err := bignum.ParseUint(s)
			if err != nil {
				return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("failed to parse %q as uint: %v", s, err))
			}
			return vm.makeBigUint(dstType, u), nil
		case VKInt:
			if src.Int < 0 {
				return Value{}, vm.eb.invalidNumericConversion("cannot convert negative int to uint")
			}
			return vm.makeBigUint(dstType, bignum.UintFromUint64(uint64(src.Int))), nil
		case VKBool:
			n := uint64(0)
			if src.Bool {
				n = 1
			}
			return vm.makeBigUint(dstType, bignum.UintFromUint64(n)), nil
		default:
			return Value{}, vm.eb.unimplemented("__to to uint")
		}

	case types.KindFloat:
		if dstTT.Width != types.WidthAny {
			return Value{}, vm.eb.unimplemented("__to to fixed-width float")
		}
		switch src.Kind {
		case VKBigFloat:
			src.TypeID = dstType
			return src, nil
		case VKBigInt:
			i, vmErr := vm.mustBigInt(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			f, err := bignum.FloatFromInt(i)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(dstType, f), nil
		case VKBigUint:
			u, vmErr := vm.mustBigUint(src)
			if vmErr != nil {
				return Value{}, vmErr
			}
			f, err := bignum.FloatFromUint(u)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(dstType, f), nil
		case VKHandleString:
			s := vm.Heap.Get(src.H).Str
			f, err := bignum.ParseFloat(s)
			if err != nil {
				return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("failed to parse %q as float: %v", s, err))
			}
			return vm.makeBigFloat(dstType, f), nil
		case VKInt:
			f, err := bignum.FloatFromInt(bignum.IntFromInt64(src.Int))
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(dstType, f), nil
		case VKBool:
			n := int64(0)
			if src.Bool {
				n = 1
			}
			f, err := bignum.FloatFromInt(bignum.IntFromInt64(n))
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(dstType, f), nil
		default:
			return Value{}, vm.eb.unimplemented("__to to float")
		}

	default:
		return Value{}, vm.eb.unimplemented("__to conversion")
	}
}
