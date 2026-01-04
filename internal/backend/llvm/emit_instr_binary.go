package llvm

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/mir"
	"surge/internal/types"
)

func (fe *funcEmitter) emitBinary(op *mir.BinaryOp) (val, ty string, err error) {
	if op == nil {
		return "", "", fmt.Errorf("nil binary op")
	}
	switch op.Op {
	case ast.ExprBinaryRange, ast.ExprBinaryRangeInclusive:
		leftVal, leftTy, leftErr := fe.emitValueOperand(&op.Left)
		if leftErr != nil {
			return "", "", leftErr
		}
		rightVal, rightTy, rightErr := fe.emitValueOperand(&op.Right)
		if rightErr != nil {
			return "", "", rightErr
		}
		if leftTy != "ptr" || rightTy != "ptr" {
			return "", "", fmt.Errorf("range operands must be int handles")
		}
		inclusive := "0"
		if op.Op == ast.ExprBinaryRangeInclusive {
			inclusive = "1"
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_range_int_new(ptr %s, ptr %s, i1 %s)\n", tmp, leftVal, rightVal, inclusive)
		return tmp, "ptr", nil
	}
	if op.Op == ast.ExprBinaryMul && isStringLike(fe.emitter.types, op.Left.Type) && !isStringLike(fe.emitter.types, op.Right.Type) {
		strPtr, strErr := fe.emitHandleOperandPtr(&op.Left)
		if strErr != nil {
			return "", "", strErr
		}
		countVal, countLLVM, countErr := fe.emitValueOperand(&op.Right)
		if countErr != nil {
			return "", "", countErr
		}
		count64, repeatErr := fe.emitRepeatCountToI64(countVal, countLLVM, op.Right.Type)
		if repeatErr != nil {
			return "", "", repeatErr
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_repeat(ptr %s, i64 %s)\n", tmp, strPtr, count64)
		return tmp, "ptr", nil
	}
	if isStringLike(fe.emitter.types, op.Left.Type) || isStringLike(fe.emitter.types, op.Right.Type) {
		if !isStringLike(fe.emitter.types, op.Left.Type) || !isStringLike(fe.emitter.types, op.Right.Type) {
			return "", "", fmt.Errorf("mixed string and non-string operands")
		}
		return fe.emitStringBinary(op)
	}
	leftVal, leftTy, err := fe.emitValueOperand(&op.Left)
	if err != nil {
		return "", "", err
	}
	rightVal, rightTy, err := fe.emitValueOperand(&op.Right)
	if err != nil {
		return "", "", err
	}
	if leftTy != rightTy {
		return "", "", fmt.Errorf("binary operand type mismatch: %s vs %s", leftTy, rightTy)
	}
	if isBigIntType(fe.emitter.types, op.Left.Type) || isBigUintType(fe.emitter.types, op.Left.Type) || isBigFloatType(fe.emitter.types, op.Left.Type) ||
		isBigIntType(fe.emitter.types, op.Right.Type) || isBigUintType(fe.emitter.types, op.Right.Type) || isBigFloatType(fe.emitter.types, op.Right.Type) {
		return fe.emitBigBinary(op, leftVal, rightVal)
	}

	switch op.Op {
	case ast.ExprBinaryLogicalAnd:
		if leftTy != "i1" {
			return "", "", fmt.Errorf("logical and requires i1, got %s", leftTy)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = and i1 %s, %s\n", tmp, leftVal, rightVal)
		return tmp, "i1", nil
	case ast.ExprBinaryLogicalOr:
		if leftTy != "i1" {
			return "", "", fmt.Errorf("logical or requires i1, got %s", leftTy)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = or i1 %s, %s\n", tmp, leftVal, rightVal)
		return tmp, "i1", nil
	case ast.ExprBinaryAdd, ast.ExprBinarySub, ast.ExprBinaryMul, ast.ExprBinaryDiv, ast.ExprBinaryMod,
		ast.ExprBinaryBitAnd, ast.ExprBinaryBitOr, ast.ExprBinaryBitXor, ast.ExprBinaryShiftLeft, ast.ExprBinaryShiftRight:
		info, ok := intInfo(fe.emitter.types, op.Left.Type)
		if ok {
			var opcode string
			switch op.Op {
			case ast.ExprBinaryAdd:
				opcode = "add"
			case ast.ExprBinarySub:
				opcode = "sub"
			case ast.ExprBinaryMul:
				opcode = "mul"
			case ast.ExprBinaryDiv:
				if info.signed {
					opcode = "sdiv"
				} else {
					opcode = "udiv"
				}
			case ast.ExprBinaryMod:
				if info.signed {
					opcode = "srem"
				} else {
					opcode = "urem"
				}
			case ast.ExprBinaryBitAnd:
				opcode = "and"
			case ast.ExprBinaryBitOr:
				opcode = "or"
			case ast.ExprBinaryBitXor:
				opcode = "xor"
			case ast.ExprBinaryShiftLeft:
				opcode = "shl"
			case ast.ExprBinaryShiftRight:
				if info.signed {
					opcode = "ashr"
				} else {
					opcode = "lshr"
				}
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s, %s\n", tmp, opcode, leftTy, leftVal, rightVal)
			return tmp, leftTy, nil
		}
		if _, floatOK := floatInfo(fe.emitter.types, op.Left.Type); floatOK {
			opcode := ""
			switch op.Op {
			case ast.ExprBinaryAdd:
				opcode = "fadd"
			case ast.ExprBinarySub:
				opcode = "fsub"
			case ast.ExprBinaryMul:
				opcode = "fmul"
			case ast.ExprBinaryDiv:
				opcode = "fdiv"
			default:
				return "", "", fmt.Errorf("unsupported float op %v", op.Op)
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s, %s\n", tmp, opcode, leftTy, leftVal, rightVal)
			return tmp, leftTy, nil
		}
		return "", "", fmt.Errorf("unsupported numeric op on type")
	case ast.ExprBinaryEq, ast.ExprBinaryNotEq, ast.ExprBinaryLess, ast.ExprBinaryLessEq, ast.ExprBinaryGreater, ast.ExprBinaryGreaterEq:
		return fe.emitCompare(op, leftVal, rightVal, leftTy)
	default:
		return "", "", fmt.Errorf("unsupported binary op %v", op.Op)
	}
}

func (fe *funcEmitter) emitCompare(op *mir.BinaryOp, leftVal, rightVal, leftTy string) (val, ty string, err error) {
	if isBigIntType(fe.emitter.types, op.Left.Type) {
		return fe.emitBigCompare("rt_bigint_cmp", op.Op, leftVal, rightVal)
	}
	if isBigUintType(fe.emitter.types, op.Left.Type) {
		return fe.emitBigCompare("rt_biguint_cmp", op.Op, leftVal, rightVal)
	}
	if isBigFloatType(fe.emitter.types, op.Left.Type) {
		return fe.emitBigCompare("rt_bigfloat_cmp", op.Op, leftVal, rightVal)
	}
	if floatInfo, ok := floatInfo(fe.emitter.types, op.Left.Type); ok {
		_ = floatInfo
		pred := ""
		switch op.Op {
		case ast.ExprBinaryEq:
			pred = "oeq"
		case ast.ExprBinaryNotEq:
			pred = "one"
		case ast.ExprBinaryLess:
			pred = "olt"
		case ast.ExprBinaryLessEq:
			pred = "ole"
		case ast.ExprBinaryGreater:
			pred = "ogt"
		case ast.ExprBinaryGreaterEq:
			pred = "oge"
		default:
			return "", "", fmt.Errorf("unsupported compare op %v", op.Op)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = fcmp %s %s %s, %s\n", tmp, pred, leftTy, leftVal, rightVal)
		return tmp, "i1", nil
	}
	info, ok := intInfo(fe.emitter.types, op.Left.Type)
	if !ok && leftTy != "ptr" {
		return "", "", fmt.Errorf("unsupported compare type")
	}
	if leftTy == "ptr" {
		pred := "eq"
		if op.Op == ast.ExprBinaryNotEq {
			pred = "ne"
		} else if op.Op != ast.ExprBinaryEq {
			return "", "", fmt.Errorf("unsupported pointer comparison %v", op.Op)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp %s ptr %s, %s\n", tmp, pred, leftVal, rightVal)
		return tmp, "i1", nil
	}
	pred := ""
	switch op.Op {
	case ast.ExprBinaryEq:
		pred = "eq"
	case ast.ExprBinaryNotEq:
		pred = "ne"
	case ast.ExprBinaryLess:
		if info.signed {
			pred = "slt"
		} else {
			pred = "ult"
		}
	case ast.ExprBinaryLessEq:
		if info.signed {
			pred = "sle"
		} else {
			pred = "ule"
		}
	case ast.ExprBinaryGreater:
		if info.signed {
			pred = "sgt"
		} else {
			pred = "ugt"
		}
	case ast.ExprBinaryGreaterEq:
		if info.signed {
			pred = "sge"
		} else {
			pred = "uge"
		}
	default:
		return "", "", fmt.Errorf("unsupported compare op %v", op.Op)
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp %s %s %s, %s\n", tmp, pred, leftTy, leftVal, rightVal)
	return tmp, "i1", nil
}

func (fe *funcEmitter) emitBigCompare(fn string, op ast.ExprBinaryOp, leftVal, rightVal string) (val, ty string, err error) {
	cmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i32 @%s(ptr %s, ptr %s)\n", cmp, fn, leftVal, rightVal)
	pred := ""
	switch op {
	case ast.ExprBinaryEq:
		pred = "eq"
	case ast.ExprBinaryNotEq:
		pred = "ne"
	case ast.ExprBinaryLess:
		pred = "slt"
	case ast.ExprBinaryLessEq:
		pred = "sle"
	case ast.ExprBinaryGreater:
		pred = "sgt"
	case ast.ExprBinaryGreaterEq:
		pred = "sge"
	default:
		return "", "", fmt.Errorf("unsupported compare op %v", op)
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp %s i32 %s, 0\n", tmp, pred, cmp)
	return tmp, "i1", nil
}

func (fe *funcEmitter) emitBigBinary(op *mir.BinaryOp, leftVal, rightVal string) (val, ty string, err error) {
	leftBigInt := isBigIntType(fe.emitter.types, op.Left.Type)
	leftBigUint := isBigUintType(fe.emitter.types, op.Left.Type)
	leftBigFloat := isBigFloatType(fe.emitter.types, op.Left.Type)
	rightBigInt := isBigIntType(fe.emitter.types, op.Right.Type)
	rightBigUint := isBigUintType(fe.emitter.types, op.Right.Type)
	rightBigFloat := isBigFloatType(fe.emitter.types, op.Right.Type)

	if leftBigInt != rightBigInt || leftBigUint != rightBigUint || leftBigFloat != rightBigFloat {
		return "", "", fmt.Errorf("mixed big numeric operands")
	}

	switch {
	case leftBigInt:
		switch op.Op {
		case ast.ExprBinaryAdd:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_add(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinarySub:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_sub(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryMul:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_mul(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryDiv:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_div(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryMod:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_mod(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryBitAnd:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_bit_and(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryBitOr:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_bit_or(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryBitXor:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_bit_xor(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryShiftLeft:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_shl(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryShiftRight:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_shr(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryEq, ast.ExprBinaryNotEq, ast.ExprBinaryLess, ast.ExprBinaryLessEq, ast.ExprBinaryGreater, ast.ExprBinaryGreaterEq:
			return fe.emitBigCompare("rt_bigint_cmp", op.Op, leftVal, rightVal)
		default:
			return "", "", fmt.Errorf("unsupported big int op %v", op.Op)
		}
	case leftBigUint:
		switch op.Op {
		case ast.ExprBinaryAdd:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_add(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinarySub:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_sub(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryMul:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_mul(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryDiv:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_div(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryMod:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_mod(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryBitAnd:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_bit_and(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryBitOr:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_bit_or(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryBitXor:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_bit_xor(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryShiftLeft:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_shl(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryShiftRight:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_shr(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryEq, ast.ExprBinaryNotEq, ast.ExprBinaryLess, ast.ExprBinaryLessEq, ast.ExprBinaryGreater, ast.ExprBinaryGreaterEq:
			return fe.emitBigCompare("rt_biguint_cmp", op.Op, leftVal, rightVal)
		default:
			return "", "", fmt.Errorf("unsupported big uint op %v", op.Op)
		}
	case leftBigFloat:
		switch op.Op {
		case ast.ExprBinaryAdd:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_add(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinarySub:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_sub(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryMul:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_mul(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryDiv:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_div(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryMod:
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_mod(ptr %s, ptr %s)\n", tmp, leftVal, rightVal)
			return tmp, "ptr", nil
		case ast.ExprBinaryEq, ast.ExprBinaryNotEq, ast.ExprBinaryLess, ast.ExprBinaryLessEq, ast.ExprBinaryGreater, ast.ExprBinaryGreaterEq:
			return fe.emitBigCompare("rt_bigfloat_cmp", op.Op, leftVal, rightVal)
		default:
			return "", "", fmt.Errorf("unsupported big float op %v", op.Op)
		}
	default:
		return "", "", fmt.Errorf("unsupported big numeric op")
	}
}

func (fe *funcEmitter) emitStringBinary(op *mir.BinaryOp) (val, ty string, err error) {
	leftPtr, err := fe.emitOperandAddr(&op.Left)
	if err != nil {
		return "", "", err
	}
	rightPtr, err := fe.emitOperandAddr(&op.Right)
	if err != nil {
		return "", "", err
	}
	switch op.Op {
	case ast.ExprBinaryAdd:
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_concat(ptr %s, ptr %s)\n", tmp, leftPtr, rightPtr)
		return tmp, "ptr", nil
	case ast.ExprBinaryEq:
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_string_eq(ptr %s, ptr %s)\n", tmp, leftPtr, rightPtr)
		return tmp, "i1", nil
	case ast.ExprBinaryNotEq:
		eqTmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_string_eq(ptr %s, ptr %s)\n", eqTmp, leftPtr, rightPtr)
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = xor i1 %s, 1\n", tmp, eqTmp)
		return tmp, "i1", nil
	default:
		return "", "", fmt.Errorf("unsupported string op %v", op.Op)
	}
}

func (fe *funcEmitter) emitRepeatCountToI64(countVal, countLLVM string, countType types.TypeID) (string, error) {
	maxIndex := int64(^uint64(0) >> 1)
	switch {
	case isBigUintType(fe.emitter.types, countType):
		val64, err := fe.emitCheckedBigUintToU64(countVal, "string repeat count out of range")
		if err != nil {
			return "", err
		}
		tooHigh := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ugt i64 %s, %d\n", tooHigh, val64, maxIndex)
		fail := fe.nextInlineBlock()
		cont := fe.nextInlineBlock()
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", tooHigh, fail, cont)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
		if err := fe.emitPanicNumeric("string repeat count out of range"); err != nil {
			return "", err
		}
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
		return val64, nil
	case isBigIntType(fe.emitter.types, countType):
		val64, err := fe.emitCheckedBigIntToI64(countVal, "string repeat count out of range")
		if err != nil {
			return "", err
		}
		neg := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i64 %s, 0\n", neg, val64)
		tooHigh := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sgt i64 %s, %d\n", tooHigh, val64, maxIndex)
		oob := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = or i1 %s, %s\n", oob, neg, tooHigh)
		fail := fe.nextInlineBlock()
		cont := fe.nextInlineBlock()
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", oob, fail, cont)
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
		if err := fe.emitPanicNumeric("string repeat count out of range"); err != nil {
			return "", err
		}
		fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
		return val64, nil
	default:
		info, ok := intInfo(fe.emitter.types, countType)
		if !ok {
			return "", fmt.Errorf("string repeat count must be integer")
		}
		val64, err := fe.coerceIntToI64(countVal, countLLVM, countType)
		if err != nil {
			return "", err
		}
		if info.signed {
			neg := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i64 %s, 0\n", neg, val64)
			tooHigh := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sgt i64 %s, %d\n", tooHigh, val64, maxIndex)
			oob := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = or i1 %s, %s\n", oob, neg, tooHigh)
			fail := fe.nextInlineBlock()
			cont := fe.nextInlineBlock()
			fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", oob, fail, cont)
			fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
			if err := fe.emitPanicNumeric("string repeat count out of range"); err != nil {
				return "", err
			}
			fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
		} else {
			tooHigh := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ugt i64 %s, %d\n", tooHigh, val64, maxIndex)
			fail := fe.nextInlineBlock()
			cont := fe.nextInlineBlock()
			fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", tooHigh, fail, cont)
			fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
			if err := fe.emitPanicNumeric("string repeat count out of range"); err != nil {
				return "", err
			}
			fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
		}
		return val64, nil
	}
}
