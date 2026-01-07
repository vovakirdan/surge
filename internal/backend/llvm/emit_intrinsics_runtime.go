package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

func (fe *funcEmitter) emitRuntimeIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if call.Callee.Sym.IsValid() && fe.emitter != nil && fe.emitter.mod != nil {
		if _, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
			return false, nil
		}
	}
	if handled, err := fe.emitFsIntrinsic(call); handled {
		return true, err
	}
	switch name {
	case "rt_alloc":
		return true, fe.emitRtAlloc(call)
	case "rt_free":
		return true, fe.emitRtFree(call)
	case "rt_realloc":
		return true, fe.emitRtRealloc(call)
	case "rt_memcpy":
		return true, fe.emitRtMemcpy(call)
	case "rt_memmove":
		return true, fe.emitRtMemmove(call)
	case "rt_write_stdout":
		return true, fe.emitRtWrite(call, "rt_write_stdout", "stdout write length out of range")
	case "rt_write_stderr":
		return true, fe.emitRtWrite(call, "rt_write_stderr", "stderr write length out of range")
	case "rt_string_len":
		return true, fe.emitRtStringLen(call, "rt_string_len")
	case "rt_string_len_bytes":
		return true, fe.emitRtStringLen(call, "rt_string_len_bytes")
	case "rt_string_from_bytes":
		return true, fe.emitRtStringFromBytes(call, "rt_string_from_bytes")
	case "rt_string_from_utf16":
		return true, fe.emitRtStringFromBytes(call, "rt_string_from_utf16")
	case "rt_panic":
		return true, fe.emitRtPanic(call)
	case "rt_panic_bounds":
		return true, fe.emitRtPanicBounds(call)
	case "rt_exit":
		return true, fe.emitRtExit(call)
	case "rt_string_index":
		return true, fe.emitRtStringIndex(call)
	case "sleep":
		return true, fe.emitRtSleep(call)
	default:
		return false, nil
	}
}

func (fe *funcEmitter) emitRtSleep(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 1 {
		return fmt.Errorf("sleep expects 1 argument")
	}
	ms64, err := fe.emitUintOperandToI64(&call.Args[0], "sleep duration out of range")
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_sleep(i64 %s)\n", tmp, ms64)
	if call.HasDst {
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != "ptr" {
			dstTy = "ptr"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
		return nil
	}
	return nil
}

func (fe *funcEmitter) emitRtAlloc(call *mir.CallInstr) error {
	if len(call.Args) != 2 {
		return fmt.Errorf("rt_alloc requires 2 arguments")
	}
	size64, err := fe.emitUintOperandToI64(&call.Args[0], "alloc size out of range")
	if err != nil {
		return err
	}
	align64, err := fe.emitUintOperandToI64(&call.Args[1], "alloc align out of range")
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %s, i64 %s)\n", tmp, size64, align64)
	if call.HasDst {
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != "ptr" {
			dstTy = "ptr"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
		return nil
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_free(ptr %s, i64 %s, i64 %s)\n", tmp, size64, align64)
	return nil
}

func (fe *funcEmitter) emitRtFree(call *mir.CallInstr) error {
	if len(call.Args) != 3 {
		return fmt.Errorf("rt_free requires 3 arguments")
	}
	ptrVal, ptrTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if ptrTy != "ptr" {
		return fmt.Errorf("rt_free expects *byte pointer")
	}
	size64, err := fe.emitUintOperandToI64(&call.Args[1], "free size out of range")
	if err != nil {
		return err
	}
	align64, err := fe.emitUintOperandToI64(&call.Args[2], "free align out of range")
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_free(ptr %s, i64 %s, i64 %s)\n", ptrVal, size64, align64)
	return nil
}

func (fe *funcEmitter) emitRtRealloc(call *mir.CallInstr) error {
	if len(call.Args) != 4 {
		return fmt.Errorf("rt_realloc requires 4 arguments")
	}
	ptrVal, ptrTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if ptrTy != "ptr" {
		return fmt.Errorf("rt_realloc expects *byte pointer")
	}
	oldSize64, err := fe.emitUintOperandToI64(&call.Args[1], "old size out of range")
	if err != nil {
		return err
	}
	newSize64, err := fe.emitUintOperandToI64(&call.Args[2], "new size out of range")
	if err != nil {
		return err
	}
	align64, err := fe.emitUintOperandToI64(&call.Args[3], "realloc align out of range")
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_realloc(ptr %s, i64 %s, i64 %s, i64 %s)\n", tmp, ptrVal, oldSize64, newSize64, align64)
	if call.HasDst {
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != "ptr" {
			dstTy = "ptr"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
		return nil
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_free(ptr %s, i64 %s, i64 %s)\n", tmp, newSize64, align64)
	return nil
}

func (fe *funcEmitter) emitRtMemcpy(call *mir.CallInstr) error {
	if len(call.Args) != 3 {
		return fmt.Errorf("rt_memcpy requires 3 arguments")
	}
	dstVal, dstTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		return fmt.Errorf("rt_memcpy expects *byte destination")
	}
	srcVal, srcTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	if srcTy != "ptr" {
		return fmt.Errorf("rt_memcpy expects *byte source")
	}
	n64, err := fe.emitUintOperandToI64(&call.Args[2], "memcpy length out of range")
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_memcpy(ptr %s, ptr %s, i64 %s)\n", dstVal, srcVal, n64)
	return nil
}

func (fe *funcEmitter) emitRtMemmove(call *mir.CallInstr) error {
	if len(call.Args) != 3 {
		return fmt.Errorf("rt_memmove requires 3 arguments")
	}
	dstVal, dstTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		return fmt.Errorf("rt_memmove expects *byte destination")
	}
	srcVal, srcTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	if srcTy != "ptr" {
		return fmt.Errorf("rt_memmove expects *byte source")
	}
	n64, err := fe.emitUintOperandToI64(&call.Args[2], "memmove length out of range")
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_memmove(ptr %s, ptr %s, i64 %s)\n", dstVal, srcVal, n64)
	return nil
}

func (fe *funcEmitter) emitRtWrite(call *mir.CallInstr, name, lenMsg string) error {
	if len(call.Args) != 2 {
		return fmt.Errorf("%s requires 2 arguments", name)
	}
	ptrVal, ptrTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if ptrTy != "ptr" {
		return fmt.Errorf("%s expects *byte pointer", name)
	}
	len64, err := fe.emitUintOperandToI64(&call.Args[1], lenMsg)
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i64 @%s(ptr %s, i64 %s)\n", tmp, name, ptrVal, len64)
	if call.HasDst {
		dstType := types.NoTypeID
		if call.Dst.Kind == mir.PlaceLocal && int(call.Dst.Local) < len(fe.f.Locals) {
			dstType = fe.f.Locals[call.Dst.Local].Type
		}
		if err := fe.emitLenStore(call.Dst, dstType, tmp); err != nil {
			return err
		}
	}
	return nil
}

func (fe *funcEmitter) emitRtStringLen(call *mir.CallInstr, name string) error {
	if len(call.Args) != 1 {
		return fmt.Errorf("%s requires 1 argument", name)
	}
	handlePtr, err := fe.emitHandleOperandPtr(&call.Args[0])
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i64 @%s(ptr %s)\n", tmp, name, handlePtr)
	if call.HasDst {
		dstType := types.NoTypeID
		if call.Dst.Kind == mir.PlaceLocal && int(call.Dst.Local) < len(fe.f.Locals) {
			dstType = fe.f.Locals[call.Dst.Local].Type
		}
		if err := fe.emitLenStore(call.Dst, dstType, tmp); err != nil {
			return err
		}
	}
	return nil
}

func (fe *funcEmitter) emitRtStringFromBytes(call *mir.CallInstr, name string) error {
	if len(call.Args) != 2 {
		return fmt.Errorf("%s requires 2 arguments", name)
	}
	ptrVal, ptrTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if ptrTy != "ptr" {
		return fmt.Errorf("%s expects pointer argument", name)
	}
	len64, err := fe.emitUintOperandToI64(&call.Args[1], "string length out of range")
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @%s(ptr %s, i64 %s)\n", tmp, name, ptrVal, len64)
	if call.HasDst {
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != "ptr" {
			dstTy = "ptr"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
	}
	return nil
}

func (fe *funcEmitter) emitRtPanic(call *mir.CallInstr) error {
	if len(call.Args) != 2 {
		return fmt.Errorf("rt_panic requires 2 arguments")
	}
	ptrVal, ptrTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	if ptrTy != "ptr" {
		return fmt.Errorf("rt_panic expects *byte pointer")
	}
	len64, err := fe.emitUintOperandToI64(&call.Args[1], "panic message length out of range")
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_panic(ptr %s, i64 %s)\n", ptrVal, len64)
	fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
	return nil
}

func (fe *funcEmitter) emitRtPanicBounds(call *mir.CallInstr) error {
	if len(call.Args) != 3 {
		return fmt.Errorf("rt_panic_bounds requires 3 arguments")
	}
	kind64, err := fe.emitUintOperandToI64(&call.Args[0], "panic bounds kind out of range")
	if err != nil {
		return err
	}
	idx64, err := fe.emitIntOperandToI64(&call.Args[1], "panic bounds index out of range")
	if err != nil {
		return err
	}
	len64, err := fe.emitIntOperandToI64(&call.Args[2], "panic bounds length out of range")
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_panic_bounds(i64 %s, i64 %s, i64 %s)\n", kind64, idx64, len64)
	fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
	return nil
}

func (fe *funcEmitter) emitRtExit(call *mir.CallInstr) error {
	if len(call.Args) != 1 {
		return fmt.Errorf("rt_exit requires 1 argument")
	}
	code64, err := fe.emitIntOperandToI64(&call.Args[0], "exit code out of range")
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_exit(i64 %s)\n", code64)
	return nil
}

func (fe *funcEmitter) emitRtStringIndex(call *mir.CallInstr) error {
	if len(call.Args) != 2 {
		return fmt.Errorf("rt_string_index requires 2 arguments")
	}
	if !call.HasDst {
		return nil
	}
	handlePtr, err := fe.emitHandleOperandPtr(&call.Args[0])
	if err != nil {
		return err
	}
	idxVal, idxTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	idxType := fe.operandTypeID(&call.Args[1])
	lenVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i64 @rt_string_len(ptr %s)\n", lenVal, handlePtr)
	idx64, err := fe.emitIndexToI64(0, idxVal, idxTy, idxType, lenVal)
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i32 @rt_string_index(ptr %s, i64 %s)\n", tmp, handlePtr, idx64)
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return err
	}
	if dstTy != "i32" {
		dstTy = "i32"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
	return nil
}

func (fe *funcEmitter) emitUintOperandToI64(op *mir.Operand, msg string) (string, error) {
	if op == nil {
		return "", fmt.Errorf("nil operand")
	}
	val, llvmTy, err := fe.emitValueOperand(op)
	if err != nil {
		return "", err
	}
	typeID := fe.operandTypeID(op)
	maxInt := int64(^uint64(0) >> 1)
	switch {
	case isBigUintType(fe.emitter.types, typeID):
		outVal, err := fe.emitCheckedBigUintToU64(val, msg)
		if err != nil {
			return "", err
		}
		tooHigh := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ugt i64 %s, %d\n", tooHigh, outVal, maxInt)
		if err := fe.emitPanicOnCond(tooHigh, msg); err != nil {
			return "", err
		}
		return outVal, nil
	case isBigIntType(fe.emitter.types, typeID):
		outVal, err := fe.emitCheckedBigIntToI64(val, msg)
		if err != nil {
			return "", err
		}
		neg := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i64 %s, 0\n", neg, outVal)
		tooHigh := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sgt i64 %s, %d\n", tooHigh, outVal, maxInt)
		oob := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = or i1 %s, %s\n", oob, neg, tooHigh)
		if err := fe.emitPanicOnCond(oob, msg); err != nil {
			return "", err
		}
		return outVal, nil
	default:
		outVal, err := fe.coerceIntToI64(val, llvmTy, typeID)
		if err != nil {
			return "", err
		}
		info, ok := intInfo(fe.emitter.types, typeID)
		if !ok {
			return outVal, nil
		}
		if info.signed {
			neg := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i64 %s, 0\n", neg, outVal)
			tooHigh := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sgt i64 %s, %d\n", tooHigh, outVal, maxInt)
			oob := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = or i1 %s, %s\n", oob, neg, tooHigh)
			if err := fe.emitPanicOnCond(oob, msg); err != nil {
				return "", err
			}
			return outVal, nil
		}
		tooHigh := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ugt i64 %s, %d\n", tooHigh, outVal, maxInt)
		if err := fe.emitPanicOnCond(tooHigh, msg); err != nil {
			return "", err
		}
		return outVal, nil
	}
}

func (fe *funcEmitter) emitIntOperandToI64(op *mir.Operand, msg string) (string, error) {
	if op == nil {
		return "", fmt.Errorf("nil operand")
	}
	val, llvmTy, err := fe.emitValueOperand(op)
	if err != nil {
		return "", err
	}
	typeID := fe.operandTypeID(op)
	maxInt := int64(^uint64(0) >> 1)
	switch {
	case isBigIntType(fe.emitter.types, typeID):
		return fe.emitCheckedBigIntToI64(val, msg)
	case isBigUintType(fe.emitter.types, typeID):
		outVal, err := fe.emitCheckedBigUintToU64(val, msg)
		if err != nil {
			return "", err
		}
		tooHigh := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ugt i64 %s, %d\n", tooHigh, outVal, maxInt)
		if err := fe.emitPanicOnCond(tooHigh, msg); err != nil {
			return "", err
		}
		return outVal, nil
	default:
		outVal, err := fe.coerceIntToI64(val, llvmTy, typeID)
		if err != nil {
			return "", err
		}
		info, ok := intInfo(fe.emitter.types, typeID)
		if !ok || info.signed {
			return outVal, nil
		}
		tooHigh := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ugt i64 %s, %d\n", tooHigh, outVal, maxInt)
		if err := fe.emitPanicOnCond(tooHigh, msg); err != nil {
			return "", err
		}
		return outVal, nil
	}
}

func (fe *funcEmitter) emitPanicOnCond(cond, msg string) error {
	bad := fe.nextInlineBlock()
	ok := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", cond, bad, ok)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", bad)
	if err := fe.emitPanicNumeric(msg); err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", ok)
	return nil
}

func (fe *funcEmitter) operandTypeID(op *mir.Operand) types.TypeID {
	if op == nil {
		return types.NoTypeID
	}
	typeID := operandValueType(fe.emitter.types, op)
	if typeID == types.NoTypeID {
		typeID = op.Type
	}
	if typeID == types.NoTypeID && op.Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(op.Place); err == nil {
			typeID = baseType
		}
	}
	return typeID
}
