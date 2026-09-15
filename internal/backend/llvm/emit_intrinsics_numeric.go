package llvm

import (
	"fmt"

	"surge/internal/types"
)

func (fe *funcEmitter) emitNumericCast(srcVal, srcLLVM string, srcTypeID, dstTypeID types.TypeID) (valOut, tyOut string, err error) {
	dstLLVM, err := fe.emitter.llvmValueType(dstTypeID)
	if err != nil {
		return "", "", err
	}
	if isBigIntType(fe.emitter.types, srcTypeID) || isBigUintType(fe.emitter.types, srcTypeID) || isBigFloatType(fe.emitter.types, srcTypeID) ||
		isBigIntType(fe.emitter.types, dstTypeID) || isBigUintType(fe.emitter.types, dstTypeID) || isBigFloatType(fe.emitter.types, dstTypeID) {
		return fe.emitBigNumericCast(srcVal, srcLLVM, srcTypeID, dstTypeID, dstLLVM)
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
		srcKind := "unknown"
		dstKind := "unknown"
		if fe.emitter != nil && fe.emitter.types != nil {
			if tt, ok := fe.emitter.types.Lookup(resolveAliasAndOwn(fe.emitter.types, srcTypeID)); ok {
				srcKind = tt.Kind.String()
			}
			if tt, ok := fe.emitter.types.Lookup(resolveAliasAndOwn(fe.emitter.types, dstTypeID)); ok {
				dstKind = tt.Kind.String()
			}
		}
		fnName := ""
		if fe != nil && fe.f != nil {
			fnName = fe.f.Name
		}
		if fnName != "" {
			return "", "", fmt.Errorf("unsupported numeric cast src=%d(%s) dst=%d(%s) in %s", srcTypeID, srcKind, dstTypeID, dstKind, fnName)
		}
		return "", "", fmt.Errorf("unsupported numeric cast src=%d(%s) dst=%d(%s)", srcTypeID, srcKind, dstTypeID, dstKind)
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
	if isBigIntType(fe.emitter.types, srcType) {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_from_bigint(ptr %s)\n", tmp, srcVal)
		return tmp, "ptr", nil
	}
	if isBigUintType(fe.emitter.types, srcType) {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_from_biguint(ptr %s)\n", tmp, srcVal)
		return tmp, "ptr", nil
	}
	if isBigFloatType(fe.emitter.types, srcType) {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_from_bigfloat(ptr %s)\n", tmp, srcVal)
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
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i8, align %d\n", outPtr, 1)
		fmt.Fprintf(&fe.emitter.buf, "  store i8 0, ptr %s\n", outPtr)
		ok := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_parse_bool(ptr %s, ptr %s)\n", ok, strAddr, outPtr)
		val8 := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", val8, outPtr)
		val1 := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = trunc i8 %s to i1\n", val1, val8)
		return val1, "i1", ok, nil
	}
	if isBigIntType(fe.emitter.types, dstType) {
		outPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr, align %d\n", outPtr, alignPtr)
		fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", outPtr)
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_parse_bigint(ptr %s, ptr %s)\n", okVal, strAddr, outPtr)
		val := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", val, outPtr)
		return val, "ptr", okVal, nil
	}
	if isBigUintType(fe.emitter.types, dstType) {
		outPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr, align %d\n", outPtr, alignPtr)
		fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", outPtr)
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_parse_biguint(ptr %s, ptr %s)\n", okVal, strAddr, outPtr)
		val := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", val, outPtr)
		return val, "ptr", okVal, nil
	}
	if isBigFloatType(fe.emitter.types, dstType) {
		outPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr, align %d\n", outPtr, alignPtr)
		fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", outPtr)
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_parse_bigfloat(ptr %s, ptr %s)\n", okVal, strAddr, outPtr)
		val := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", val, outPtr)
		return val, "ptr", okVal, nil
	}
	if info, ok := intInfo(fe.emitter.types, dstType); ok {
		outPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64, align %d\n", outPtr, alignWord)
		fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s\n", outPtr)
		okVal := fe.nextTemp()
		if info.signed {
			fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_parse_int(ptr %s, ptr %s)\n", okVal, strAddr, outPtr)
		} else {
			fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_parse_uint(ptr %s, ptr %s)\n", okVal, strAddr, outPtr)
		}
		val := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", val, outPtr)
		if info.bits < 64 {
			rangeOK := fe.nextTemp()
			if info.signed {
				minVal := -int64(1) << (info.bits - 1)
				maxVal := int64(1)<<(info.bits-1) - 1
				minOK := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sge i64 %s, %d\n", minOK, val, minVal)
				maxOK := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sle i64 %s, %d\n", maxOK, val, maxVal)
				fmt.Fprintf(&fe.emitter.buf, "  %s = and i1 %s, %s\n", rangeOK, minOK, maxOK)
			} else {
				maxVal := (uint64(1) << info.bits) - 1
				fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ule i64 %s, %d\n", rangeOK, val, maxVal)
			}
			parsedAndInRange := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = and i1 %s, %s\n", parsedAndInRange, okVal, rangeOK)
			okVal = parsedAndInRange
		}
		srcType := builtins.Int64
		if !info.signed {
			srcType = builtins.Uint64
		}
		casted, castTy, err := fe.emitNumericCast(val, "i64", srcType, dstType)
		if err != nil {
			return "", "", "", err
		}
		return casted, castTy, okVal, nil
	}
	if _, ok := floatInfo(fe.emitter.types, dstType); ok {
		outPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca double, align %d\n", outPtr, 8)
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
