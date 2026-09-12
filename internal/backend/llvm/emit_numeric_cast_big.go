package llvm

import (
	"fmt"

	"surge/internal/types"
)

func (fe *funcEmitter) emitBigNumericCast(srcVal, srcLLVM string, srcTypeID, dstTypeID types.TypeID, dstLLVM string) (valOut, tyOut string, err error) {
	floatOps, _ := scalarLifecycleFor(fe.emitter.types, fe.emitter.types.Builtins().Float)
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
			fe.emitScalarRelease(tmpF, floatOps)
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
			fe.emitScalarRelease(tmpF, floatOps)
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
				val64, err = fe.emitCheckedNumericToFixed(tmp, numericInt, "integer overflow", true)
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
			val64, err = fe.emitCheckedNumericToFixed(tmp, numericUint, "unsigned overflow", true)
		case srcBigFloat:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_to_biguint(ptr %s)\n", tmp, srcVal)
			val64, err = fe.emitCheckedNumericToFixed(tmp, numericUint, "unsigned overflow", true)
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
			val64, err = fe.emitCheckedNumericToFixed(tmp, numericFloat, "float overflow", true)
		case srcBigUint:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_to_bigfloat(ptr %s)\n", tmp, srcVal)
			val64, err = fe.emitCheckedNumericToFixed(tmp, numericFloat, "float overflow", true)
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
