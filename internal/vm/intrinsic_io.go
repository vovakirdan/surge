package vm

import (
	"fmt"
	"os"

	"fortio.org/safecast"

	"surge/internal/mir"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// handleSizeOfAlignOf handles the size_of and align_of intrinsics.
func (vm *VM) handleSizeOfAlignOf(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite, name string) *VMError {
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
	return nil
}

// handleRtArgv handles the rt_argv intrinsic.
func (vm *VM) handleRtArgv(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
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
		vm.Heap.Release(arrH)
	}
	return nil
}

// handleRtStdinReadAll handles the rt_stdin_read_all intrinsic.
func (vm *VM) handleRtStdinReadAll(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
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
		vm.Heap.Release(h)
	}
	return nil
}

// handleWriteStdout handles the rt_write_stdout intrinsic.
func (vm *VM) handleWriteStdout(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
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
	s := vm.stringBytes(obj)
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
	return nil
}

// handleRtExit handles the rt_exit intrinsic.
func (vm *VM) handleRtExit(frame *Frame, call *mir.CallInstr) *VMError {
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
		vm.dropValue(val)
	}
	vm.ExitCode = code
	vm.RT.Exit(code)

	vm.dropAllFrames()
	vm.checkLeaksOrPanic()

	vm.Halted = true
	vm.Stack = nil
	return nil
}

// handleParseArg handles the rt_parse_arg intrinsic.
func (vm *VM) handleParseArg(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
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
		s := vm.stringBytes(vm.Heap.Get(strVal.H))
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
		s := vm.stringBytes(vm.Heap.Get(strVal.H))
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
		s := vm.stringBytes(vm.Heap.Get(strVal.H))
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
}
