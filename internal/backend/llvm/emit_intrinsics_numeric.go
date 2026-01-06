package llvm

import (
	"fmt"

	"surge/internal/types"
)

func (fe *funcEmitter) emitNumericCast(srcVal, srcLLVM string, srcTypeID, dstTypeID types.TypeID) (valOut, tyOut string, err error) {
	dstLLVM, err := llvmValueType(fe.emitter.types, dstTypeID)
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
		return "", "", fmt.Errorf("unsupported numeric cast")
	}
}

func (fe *funcEmitter) emitBigNumericCast(srcVal, srcLLVM string, srcTypeID, dstTypeID types.TypeID, dstLLVM string) (valOut, tyOut string, err error) {
	srcBigInt := isBigIntType(fe.emitter.types, srcTypeID)
	srcBigUint := isBigUintType(fe.emitter.types, srcTypeID)
	srcBigFloat := isBigFloatType(fe.emitter.types, srcTypeID)
	dstBigInt := isBigIntType(fe.emitter.types, dstTypeID)
	dstBigUint := isBigUintType(fe.emitter.types, dstTypeID)
	dstBigFloat := isBigFloatType(fe.emitter.types, dstTypeID)
	srcInt, srcIntOK := intInfo(fe.emitter.types, srcTypeID)
	dstInt, dstIntOK := intInfo(fe.emitter.types, dstTypeID)
	_, srcFloatOK := floatInfo(fe.emitter.types, srcTypeID)
	dstFloat, dstFloatOK := floatInfo(fe.emitter.types, dstTypeID)

	signedBounds := func(bits int) (minVal, maxVal int64, ok bool) {
		if bits <= 0 {
			return 0, 0, false
		}
		if bits >= 64 {
			limit := int64(^uint64(0) >> 1)
			return -limit - 1, limit, true
		}
		limit := int64(1)<<(bits-1) - 1
		minVal = -int64(1) << (bits - 1)
		return minVal, limit, true
	}
	unsignedMax := func(bits int) (uint64, bool) {
		if bits <= 0 {
			return 0, false
		}
		if bits >= 64 {
			return ^uint64(0), true
		}
		return (uint64(1) << bits) - 1, true
	}
	floatMaxConst := func(bits int) (string, bool) {
		switch bits {
		case 16:
			return "65504.0", true
		case 32:
			return "3.4028234663852886e+38", true
		case 64:
			return "1.7976931348623157e+308", true
		default:
			return "", false
		}
	}
	checkSignedRange := func(val string, bits int) error {
		minVal, maxVal, ok := signedBounds(bits)
		if !ok || bits >= 64 {
			return nil
		}
		tooLow := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i64 %s, %d\n", tooLow, val, minVal)
		tooHigh := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sgt i64 %s, %d\n", tooHigh, val, maxVal)
		oob := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = or i1 %s, %s\n", oob, tooLow, tooHigh)
		fail := fe.nextInlineBlock()
		cont := fe.nextInlineBlock()
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", oob, fail, cont)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
		if panicErr := fe.emitPanicNumeric("integer overflow"); panicErr != nil {
			return panicErr
		}
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
		return nil
	}
	checkUnsignedRange := func(val string, bits int) error {
		maxVal, ok := unsignedMax(bits)
		if !ok || bits >= 64 {
			return nil
		}
		tooHigh := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ugt i64 %s, %d\n", tooHigh, val, maxVal)
		fail := fe.nextInlineBlock()
		cont := fe.nextInlineBlock()
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", tooHigh, fail, cont)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
		if panicErr := fe.emitPanicNumeric("unsigned overflow"); panicErr != nil {
			return panicErr
		}
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
		return nil
	}
	checkFloatRange := func(val string, bits int) error {
		maxVal, ok := floatMaxConst(bits)
		if !ok {
			return nil
		}
		negMax := "-" + maxVal
		tooLow := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = fcmp olt double %s, %s\n", tooLow, val, negMax)
		tooHigh := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = fcmp ogt double %s, %s\n", tooHigh, val, maxVal)
		oob := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = or i1 %s, %s\n", oob, tooLow, tooHigh)
		fail := fe.nextInlineBlock()
		cont := fe.nextInlineBlock()
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", oob, fail, cont)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
		if panicErr := fe.emitPanicNumeric("float overflow"); panicErr != nil {
			return panicErr
		}
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
		return nil
	}

	switch {
	case dstBigInt:
		switch {
		case srcBigInt:
			return srcVal, "ptr", nil
		case srcBigUint:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_to_bigint(ptr %s)\n", tmp, srcVal)
			return tmp, "ptr", nil
		case srcBigFloat:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_to_bigint(ptr %s)\n", tmp, srcVal)
			return tmp, "ptr", nil
		case srcIntOK:
			val64, convErr := fe.coerceIntToI64(srcVal, srcLLVM, srcTypeID)
			if convErr != nil {
				return "", "", convErr
			}
			tmp := fe.nextTemp()
			if srcInt.signed {
				fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_from_i64(i64 %s)\n", tmp, val64)
			} else {
				fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_from_u64(i64 %s)\n", tmp, val64)
			}
			return tmp, "ptr", nil
		case srcFloatOK:
			val := srcVal
			if srcLLVM != "double" {
				tmp := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = fpext %s %s to double\n", tmp, srcLLVM, srcVal)
				val = tmp
			}
			tmpF := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_from_f64(double %s)\n", tmpF, val)
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_to_bigint(ptr %s)\n", tmp, tmpF)
			return tmp, "ptr", nil
		default:
			return "", "", fmt.Errorf("unsupported numeric cast to big int")
		}
	case dstBigUint:
		switch {
		case srcBigUint:
			return srcVal, "ptr", nil
		case srcBigInt:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_to_biguint(ptr %s)\n", tmp, srcVal)
			return tmp, "ptr", nil
		case srcBigFloat:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_to_biguint(ptr %s)\n", tmp, srcVal)
			return tmp, "ptr", nil
		case srcIntOK:
			val64, convErr := fe.coerceIntToI64(srcVal, srcLLVM, srcTypeID)
			if convErr != nil {
				return "", "", convErr
			}
			if srcInt.signed {
				neg := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i64 %s, 0\n", neg, val64)
				fail := fe.nextInlineBlock()
				cont := fe.nextInlineBlock()
				fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", neg, fail, cont)
				fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
				if panicErr := fe.emitPanicNumeric("cannot convert negative int to uint"); panicErr != nil {
					return "", "", panicErr
				}
				fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_from_u64(i64 %s)\n", tmp, val64)
			return tmp, "ptr", nil
		case srcFloatOK:
			val := srcVal
			if srcLLVM != "double" {
				tmp := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = fpext %s %s to double\n", tmp, srcLLVM, srcVal)
				val = tmp
			}
			tmpF := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_from_f64(double %s)\n", tmpF, val)
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_to_biguint(ptr %s)\n", tmp, tmpF)
			return tmp, "ptr", nil
		default:
			return "", "", fmt.Errorf("unsupported numeric cast to big uint")
		}
	case dstBigFloat:
		switch {
		case srcBigFloat:
			return srcVal, "ptr", nil
		case srcBigInt:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_to_bigfloat(ptr %s)\n", tmp, srcVal)
			return tmp, "ptr", nil
		case srcBigUint:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_to_bigfloat(ptr %s)\n", tmp, srcVal)
			return tmp, "ptr", nil
		case srcIntOK:
			val64, convErr := fe.coerceIntToI64(srcVal, srcLLVM, srcTypeID)
			if convErr != nil {
				return "", "", convErr
			}
			tmp := fe.nextTemp()
			if srcInt.signed {
				fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_from_i64(i64 %s)\n", tmp, val64)
			} else {
				fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_from_u64(i64 %s)\n", tmp, val64)
			}
			return tmp, "ptr", nil
		case srcFloatOK:
			val := srcVal
			if srcLLVM != "double" {
				tmp := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = fpext %s %s to double\n", tmp, srcLLVM, srcVal)
				val = tmp
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_from_f64(double %s)\n", tmp, val)
			return tmp, "ptr", nil
		default:
			return "", "", fmt.Errorf("unsupported numeric cast to big float")
		}
	}

	if dstIntOK {
		if !srcBigInt && !srcBigUint && !srcBigFloat {
			return "", "", fmt.Errorf("unsupported numeric cast")
		}
		val64 := ""
		if dstInt.signed {
			switch {
			case srcBigInt:
				val64, err = fe.emitCheckedBigIntToI64(srcVal, "integer overflow")
			case srcBigUint:
				val64, err = fe.emitCheckedBigUintToU64(srcVal, "integer overflow")
				if err == nil {
					maxInt := int64(^uint64(0) >> 1)
					tooHigh := fe.nextTemp()
					fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ugt i64 %s, %d\n", tooHigh, val64, maxInt)
					fail := fe.nextInlineBlock()
					cont := fe.nextInlineBlock()
					fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", tooHigh, fail, cont)
					fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
					if panicErr := fe.emitPanicNumeric("integer overflow"); panicErr != nil {
						return "", "", panicErr
					}
					fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
				}
			case srcBigFloat:
				tmp := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_to_bigint(ptr %s)\n", tmp, srcVal)
				val64, err = fe.emitCheckedBigIntToI64(tmp, "integer overflow")
			}
			if err != nil {
				return "", "", err
			}
			if rangeErr := checkSignedRange(val64, dstInt.bits); rangeErr != nil {
				return "", "", rangeErr
			}
			if dstLLVM != "i64" {
				tmp := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = trunc i64 %s to %s\n", tmp, val64, dstLLVM)
				return tmp, dstLLVM, nil
			}
			return val64, dstLLVM, nil
		}
		switch {
		case srcBigUint:
			val64, err = fe.emitCheckedBigUintToU64(srcVal, "unsigned overflow")
		case srcBigInt:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_to_biguint(ptr %s)\n", tmp, srcVal)
			val64, err = fe.emitCheckedBigUintToU64(tmp, "unsigned overflow")
		case srcBigFloat:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_to_biguint(ptr %s)\n", tmp, srcVal)
			val64, err = fe.emitCheckedBigUintToU64(tmp, "unsigned overflow")
		}
		if err != nil {
			return "", "", err
		}
		if rangeErr := checkUnsignedRange(val64, dstInt.bits); rangeErr != nil {
			return "", "", rangeErr
		}
		if dstLLVM != "i64" {
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = trunc i64 %s to %s\n", tmp, val64, dstLLVM)
			return tmp, dstLLVM, nil
		}
		return val64, dstLLVM, nil
	}

	if dstFloatOK {
		if !srcBigInt && !srcBigUint && !srcBigFloat {
			return "", "", fmt.Errorf("unsupported numeric cast")
		}
		val64 := ""
		switch {
		case srcBigFloat:
			val64, err = fe.emitCheckedBigFloatToF64(srcVal)
		case srcBigInt:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_to_bigfloat(ptr %s)\n", tmp, srcVal)
			val64, err = fe.emitCheckedBigFloatToF64(tmp)
		case srcBigUint:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_to_bigfloat(ptr %s)\n", tmp, srcVal)
			val64, err = fe.emitCheckedBigFloatToF64(tmp)
		}
		if err != nil {
			return "", "", err
		}
		if err := checkFloatRange(val64, dstFloat.bits); err != nil {
			return "", "", err
		}
		if dstLLVM != "double" {
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = fptrunc double %s to %s\n", tmp, val64, dstLLVM)
			return tmp, dstLLVM, nil
		}
		return val64, dstLLVM, nil
	}

	return "", "", fmt.Errorf("unsupported numeric cast")
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
	if isBigIntType(fe.emitter.types, dstType) {
		outPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr\n", outPtr)
		fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", outPtr)
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_parse_bigint(ptr %s, ptr %s)\n", okVal, strAddr, outPtr)
		val := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", val, outPtr)
		return val, "ptr", okVal, nil
	}
	if isBigUintType(fe.emitter.types, dstType) {
		outPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr\n", outPtr)
		fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", outPtr)
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_parse_biguint(ptr %s, ptr %s)\n", okVal, strAddr, outPtr)
		val := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", val, outPtr)
		return val, "ptr", okVal, nil
	}
	if isBigFloatType(fe.emitter.types, dstType) {
		outPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr\n", outPtr)
		fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", outPtr)
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_parse_bigfloat(ptr %s, ptr %s)\n", okVal, strAddr, outPtr)
		val := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", val, outPtr)
		return val, "ptr", okVal, nil
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

func (fe *funcEmitter) emitTagValueSinglePayload(typeID types.TypeID, tagIndex int, payloadType types.TypeID, val, valTy string, valType types.TypeID) (string, error) {
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
		casted, castTy, err := fe.coerceNumericValue(val, valTy, valType, payloadType)
		if err != nil {
			return "", err
		}
		val = casted
		valTy = castTy
	}
	if valTy != payloadLLVM {
		return "", fmt.Errorf("tag payload type mismatch for type#%d tag %d: expected %s, got %s", typeID, tagIndex, payloadLLVM, valTy)
	}
	off := layoutInfo.PayloadOffset + offsets[0]
	bytePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, mem, off)
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", valTy, val, bytePtr)
	return mem, nil
}
