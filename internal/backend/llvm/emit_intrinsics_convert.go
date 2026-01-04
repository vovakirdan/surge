package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

type floatMeta struct {
	bits int
}

func floatInfo(typesIn *types.Interner, id types.TypeID) (floatMeta, bool) {
	if typesIn == nil {
		return floatMeta{}, false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	if !ok || tt.Kind != types.KindFloat {
		return floatMeta{}, false
	}
	return floatMeta{bits: widthBits(tt.Width)}, true
}

func isBoolType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil {
		return false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindBool
}

func (fe *funcEmitter) emitToIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "__to" {
		return false, nil
	}
	if call.Callee.Sym.IsValid() && fe.emitter != nil && fe.emitter.mod != nil {
		if _, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
			return false, nil
		}
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("__to requires 1 argument")
	}
	if !call.HasDst {
		return true, nil
	}
	dstType := fe.f.Locals[call.Dst.Local].Type
	srcVal, srcLLVM, srcType, err := fe.emitToSource(&call.Args[0])
	if err != nil {
		return true, err
	}

	var outVal, outTy string
	switch {
	case isStringLike(fe.emitter.types, dstType):
		outVal, outTy, err = fe.emitToString(srcVal, srcLLVM, srcType)
	case isStringLike(fe.emitter.types, srcType):
		outVal, outTy, _, err = fe.emitParseStringValue(srcVal, dstType)
	default:
		outVal, outTy, err = fe.emitNumericCast(srcVal, srcLLVM, srcType, dstType)
	}
	if err != nil {
		return true, err
	}

	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return true, err
	}
	if dstTy != outTy {
		outTy = dstTy
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", outTy, outVal, ptr)
	return true, nil
}

func (fe *funcEmitter) emitFromStrIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "from_str" {
		return false, nil
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("from_str requires 1 argument")
	}
	if !call.HasDst {
		return true, nil
	}
	dstType := fe.f.Locals[call.Dst.Local].Type
	successIdx, successMeta, err := fe.emitter.tagCaseMeta(dstType, "Success", symbols.NoSymbolID)
	if err != nil {
		return true, err
	}
	if len(successMeta.PayloadTypes) != 1 {
		return true, fmt.Errorf("from_str requires single payload Success tag")
	}
	targetType := successMeta.PayloadTypes[0]
	strVal, _, srcType, err := fe.emitToSource(&call.Args[0])
	if err != nil {
		return true, err
	}
	if !isStringLike(fe.emitter.types, srcType) {
		return true, fmt.Errorf("from_str requires string argument")
	}

	var parsedVal, parsedTy, okVal string
	if isStringLike(fe.emitter.types, targetType) {
		parsedVal = strVal
		parsedTy = "ptr"
		okVal = "1"
	} else {
		parsedVal, parsedTy, okVal, err = fe.emitParseStringValue(strVal, targetType)
		if err != nil {
			return true, err
		}
	}

	okBB := fe.nextInlineBlock()
	errBB := fe.nextInlineBlock()
	contBB := fe.nextInlineBlock()

	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", okVal, okBB, errBB)

	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return true, err
	}

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", okBB)
	tagVal, err := fe.emitTagValueSinglePayload(dstType, successIdx, targetType, parsedVal, parsedTy)
	if err != nil {
		return true, err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tagVal, ptr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", errBB)
	errType, err := fe.erringErrorType(dstType)
	if err != nil {
		return true, err
	}
	msgVal, _, err := fe.emitStringConst("parse error")
	if err != nil {
		return true, err
	}
	errVal, err := fe.emitErrorLikeValue(errType, msgVal, "ptr", "1", "i64")
	if err != nil {
		return true, err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, errVal, ptr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", contBB)
	return true, nil
}

func (fe *funcEmitter) emitToSource(op *mir.Operand) (val, llvmTy string, typeID types.TypeID, err error) {
	if op == nil {
		return "", "", types.NoTypeID, fmt.Errorf("nil operand")
	}
	val, llvmTy, err = fe.emitOperand(op)
	if err != nil {
		return "", "", types.NoTypeID, err
	}
	typeID = operandValueType(fe.emitter.types, op)
	if typeID == types.NoTypeID && op.Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(op.Place); err == nil {
			typeID = baseType
		}
	}
	if isRefType(fe.emitter.types, op.Type) {
		elemType, ok := derefType(fe.emitter.types, op.Type)
		if ok {
			llvmElem, err := llvmValueType(fe.emitter.types, elemType)
			if err != nil {
				return "", "", types.NoTypeID, err
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", tmp, llvmElem, val)
			val = tmp
			llvmTy = llvmElem
			typeID = elemType
		}
	}
	return val, llvmTy, typeID, nil
}

func (fe *funcEmitter) emitNumericCast(srcVal, srcLLVM string, srcTypeID, dstTypeID types.TypeID) (valOut, tyOut string, err error) {
	dstLLVM, err := llvmValueType(fe.emitter.types, dstTypeID)
	if err != nil {
		return "", "", err
	}
	srcInt, srcIntOK := intInfo(fe.emitter.types, srcTypeID)
	dstInt, dstIntOK := intInfo(fe.emitter.types, dstTypeID)
	srcFloat, srcFloatOK := floatInfo(fe.emitter.types, srcTypeID)
	dstFloat, dstFloatOK := floatInfo(fe.emitter.types, dstTypeID)

	switch {
	case srcIntOK && dstIntOK:
		if srcInt.bits < dstInt.bits {
			op := "zext"
			if srcInt.signed {
				op = "sext"
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s to %s\n", tmp, op, srcLLVM, srcVal, dstLLVM)
			return tmp, dstLLVM, nil
		}
		if srcInt.bits > dstInt.bits {
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = trunc %s %s to %s\n", tmp, srcLLVM, srcVal, dstLLVM)
			return tmp, dstLLVM, nil
		}
		return srcVal, dstLLVM, nil
	case srcIntOK && dstFloatOK:
		op := "uitofp"
		if srcInt.signed {
			op = "sitofp"
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s to %s\n", tmp, op, srcLLVM, srcVal, dstLLVM)
		return tmp, dstLLVM, nil
	case srcFloatOK && dstIntOK:
		op := "fptoui"
		if dstInt.signed {
			op = "fptosi"
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s to %s\n", tmp, op, srcLLVM, srcVal, dstLLVM)
		return tmp, dstLLVM, nil
	case srcFloatOK && dstFloatOK:
		if srcFloat.bits < dstFloat.bits {
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = fpext %s %s to %s\n", tmp, srcLLVM, srcVal, dstLLVM)
			return tmp, dstLLVM, nil
		}
		if srcFloat.bits > dstFloat.bits {
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = fptrunc %s %s to %s\n", tmp, srcLLVM, srcVal, dstLLVM)
			return tmp, dstLLVM, nil
		}
		return srcVal, dstLLVM, nil
	default:
		return "", "", fmt.Errorf("unsupported numeric cast")
	}
}

func (fe *funcEmitter) emitToString(srcVal, srcLLVM string, srcType types.TypeID) (valOut, tyOut string, err error) {
	if isStringLike(fe.emitter.types, srcType) {
		return srcVal, "ptr", nil
	}
	if isBoolType(fe.emitter.types, srcType) {
		trueVal, _, err := fe.emitStringConst("true")
		if err != nil {
			return "", "", err
		}
		falseVal, _, err := fe.emitStringConst("false")
		if err != nil {
			return "", "", err
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = select i1 %s, ptr %s, ptr %s\n", tmp, srcVal, trueVal, falseVal)
		return tmp, "ptr", nil
	}
	if info, ok := intInfo(fe.emitter.types, srcType); ok {
		val64, err := fe.coerceIntToI64(srcVal, srcLLVM, srcType)
		if err != nil {
			return "", "", err
		}
		tmp := fe.nextTemp()
		if info.signed {
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_from_int(i64 %s)\n", tmp, val64)
		} else {
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_from_uint(i64 %s)\n", tmp, val64)
		}
		return tmp, "ptr", nil
	}
	if _, ok := floatInfo(fe.emitter.types, srcType); ok {
		val := srcVal
		if srcLLVM != "double" {
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = fpext %s %s to double\n", tmp, srcLLVM, srcVal)
			val = tmp
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_from_float(double %s)\n", tmp, val)
		return tmp, "ptr", nil
	}
	return "", "", fmt.Errorf("__to to string unsupported")
}

func (fe *funcEmitter) emitParseStringValue(strVal string, dstType types.TypeID) (valOut, tyOut, okVal string, err error) {
	if fe.emitter == nil || fe.emitter.types == nil {
		return "", "", "", fmt.Errorf("missing type interner")
	}
	strAddr := fe.emitHandleAddr(strVal)
	builtins := fe.emitter.types.Builtins()

	if isBoolType(fe.emitter.types, dstType) {
		outPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i8\n", outPtr)
		fmt.Fprintf(&fe.emitter.buf, "  store i8 0, ptr %s\n", outPtr)
		ok := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_parse_bool(ptr %s, ptr %s)\n", ok, strAddr, outPtr)
		val8 := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", val8, outPtr)
		val1 := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = trunc i8 %s to i1\n", val1, val8)
		return val1, "i1", ok, nil
	}
	if info, ok := intInfo(fe.emitter.types, dstType); ok {
		outPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", outPtr)
		fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s\n", outPtr)
		okVal := fe.nextTemp()
		if info.signed {
			fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_parse_int(ptr %s, ptr %s)\n", okVal, strAddr, outPtr)
		} else {
			fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_parse_uint(ptr %s, ptr %s)\n", okVal, strAddr, outPtr)
		}
		val := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", val, outPtr)
		srcType := builtins.Int
		if !info.signed {
			srcType = builtins.Uint
		}
		casted, castTy, err := fe.emitNumericCast(val, "i64", srcType, dstType)
		if err != nil {
			return "", "", "", err
		}
		return casted, castTy, okVal, nil
	}
	if _, ok := floatInfo(fe.emitter.types, dstType); ok {
		outPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca double\n", outPtr)
		fmt.Fprintf(&fe.emitter.buf, "  store double 0.0, ptr %s\n", outPtr)
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_parse_float(ptr %s, ptr %s)\n", okVal, strAddr, outPtr)
		val := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load double, ptr %s\n", val, outPtr)
		casted, castTy, err := fe.emitNumericCast(val, "double", builtins.Float, dstType)
		if err != nil {
			return "", "", "", err
		}
		return casted, castTy, okVal, nil
	}
	return "", "", "", fmt.Errorf("unsupported from_str target")
}

func (fe *funcEmitter) emitTagValueSinglePayload(typeID types.TypeID, tagIndex int, payloadType types.TypeID, val, valTy string) (string, error) {
	layoutInfo, err := fe.emitter.layoutOf(typeID)
	if err != nil {
		return "", err
	}
	if layoutInfo.TagSize != 4 {
		return "", fmt.Errorf("unsupported tag size %d for type#%d", layoutInfo.TagSize, typeID)
	}
	size := layoutInfo.Size
	align := layoutInfo.Align
	if size <= 0 {
		size = 1
	}
	if align <= 0 {
		align = 1
	}
	mem := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", mem, size, align)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %d, ptr %s\n", tagIndex, mem)

	offsets, err := fe.emitter.payloadOffsets([]types.TypeID{payloadType})
	if err != nil {
		return "", err
	}
	payloadLLVM, err := llvmValueType(fe.emitter.types, payloadType)
	if err != nil {
		return "", err
	}
	if valTy != payloadLLVM {
		valTy = payloadLLVM
	}
	off := layoutInfo.PayloadOffset + offsets[0]
	bytePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, mem, off)
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", valTy, val, bytePtr)
	return mem, nil
}
