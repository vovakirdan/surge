package llvm

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/mir"
)

func (fe *funcEmitter) emitMagicIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	switch name {
	case "__add", "__sub", "__mul", "__div", "__mod",
		"__bit_and", "__bit_or", "__bit_xor", "__shl", "__shr",
		"__eq", "__ne", "__lt", "__le", "__gt", "__ge":
		if !fe.canEmitMagicBinary(call) {
			return false, nil
		}
		return true, fe.emitMagicBinaryIntrinsic(call, name)
	case "__pos", "__neg", "__not":
		if !fe.canEmitMagicUnary(call) {
			return false, nil
		}
		return true, fe.emitMagicUnaryIntrinsic(call, name)
	default:
		return false, nil
	}
}

func (fe *funcEmitter) canEmitMagicBinary(call *mir.CallInstr) bool {
	if call == nil || len(call.Args) != 2 {
		return false
	}
	leftType := operandValueType(fe.emitter.types, &call.Args[0])
	rightType := operandValueType(fe.emitter.types, &call.Args[1])
	if isStringLike(fe.emitter.types, leftType) || isStringLike(fe.emitter.types, rightType) {
		if isStringLike(fe.emitter.types, leftType) && isStringLike(fe.emitter.types, rightType) {
			return true
		}
		if isStringLike(fe.emitter.types, leftType) && (isBigIntType(fe.emitter.types, rightType) || isBigUintType(fe.emitter.types, rightType)) {
			return true
		}
		if isStringLike(fe.emitter.types, leftType) {
			if _, ok := intInfo(fe.emitter.types, rightType); ok {
				return true
			}
		}
		return false
	}
	leftKind := numericKindOf(fe.emitter.types, leftType)
	rightKind := numericKindOf(fe.emitter.types, rightType)
	return leftKind != numericNone && leftKind == rightKind
}

func (fe *funcEmitter) canEmitMagicUnary(call *mir.CallInstr) bool {
	if call == nil || len(call.Args) != 1 {
		return false
	}
	operandType := operandValueType(fe.emitter.types, &call.Args[0])
	if isBigIntType(fe.emitter.types, operandType) || isBigUintType(fe.emitter.types, operandType) || isBigFloatType(fe.emitter.types, operandType) {
		return true
	}
	if _, ok := intInfo(fe.emitter.types, operandType); ok {
		return true
	}
	_, ok := floatInfo(fe.emitter.types, operandType)
	return ok
}

func (fe *funcEmitter) emitMagicBinaryIntrinsic(call *mir.CallInstr, name string) error {
	if len(call.Args) != 2 {
		return fmt.Errorf("%s requires 2 arguments", name)
	}
	if !call.HasDst {
		return nil
	}
	leftType := operandValueType(fe.emitter.types, &call.Args[0])
	rightType := operandValueType(fe.emitter.types, &call.Args[1])
	if name == "__mul" && isStringLike(fe.emitter.types, leftType) && !isStringLike(fe.emitter.types, rightType) {
		strPtr, err := fe.emitHandleOperandPtr(&call.Args[0])
		if err != nil {
			return err
		}
		countVal, countLLVM, err := fe.emitValueOperand(&call.Args[1])
		if err != nil {
			return err
		}
		count64, err := fe.emitRepeatCountToI64(countVal, countLLVM, call.Args[1].Type)
		if err != nil {
			return err
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_repeat(ptr %s, i64 %s)\n", tmp, strPtr, count64)
		ptr, dstTy, placeErr := fe.emitPlacePtr(call.Dst)
		if placeErr != nil {
			return placeErr
		}
		if dstTy != "ptr" {
			dstTy = "ptr"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
		return nil
	}
	if isStringLike(fe.emitter.types, leftType) || isStringLike(fe.emitter.types, rightType) {
		if !isStringLike(fe.emitter.types, leftType) || !isStringLike(fe.emitter.types, rightType) {
			return fmt.Errorf("mixed string and non-string operands")
		}
		leftPtr, err := fe.emitHandleOperandPtr(&call.Args[0])
		if err != nil {
			return err
		}
		rightPtr, err := fe.emitHandleOperandPtr(&call.Args[1])
		if err != nil {
			return err
		}
		tmp := fe.nextTemp()
		resultTy := ""
		switch name {
		case "__add":
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_concat(ptr %s, ptr %s)\n", tmp, leftPtr, rightPtr)
			resultTy = "ptr"
		case "__eq":
			fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_string_eq(ptr %s, ptr %s)\n", tmp, leftPtr, rightPtr)
			resultTy = "i1"
		case "__ne":
			eqTmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_string_eq(ptr %s, ptr %s)\n", eqTmp, leftPtr, rightPtr)
			fmt.Fprintf(&fe.emitter.buf, "  %s = xor i1 %s, 1\n", tmp, eqTmp)
			resultTy = "i1"
		default:
			return fmt.Errorf("unsupported string op %s", name)
		}
		ptr, dstTy, placeErr := fe.emitPlacePtr(call.Dst)
		if placeErr != nil {
			return placeErr
		}
		if dstTy != resultTy {
			dstTy = resultTy
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
		return nil
	}

	leftVal, leftTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	rightVal, rightTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	leftType = resolveValueType(fe.emitter.types, leftType)
	rightType = resolveValueType(fe.emitter.types, rightType)
	pair, err := fe.coerceNumericPair(leftVal, leftTy, leftType, rightVal, rightTy, rightType)
	if err != nil {
		return err
	}
	leftVal, leftTy, leftType = pair.leftVal, pair.leftTy, pair.leftType
	rightVal, rightTy, rightType = pair.rightVal, pair.rightTy, pair.rightType
	if leftTy != rightTy {
		return fmt.Errorf("binary operand type mismatch: %s vs %s", leftTy, rightTy)
	}
	if isBigIntType(fe.emitter.types, leftType) || isBigUintType(fe.emitter.types, leftType) || isBigFloatType(fe.emitter.types, leftType) {
		op, ok := magicBinaryOp(name)
		if !ok {
			return fmt.Errorf("unsupported magic binary op %s", name)
		}
		bin := mir.BinaryOp{Op: op, Left: call.Args[0], Right: call.Args[1]}
		val, resultTy, binErr := fe.emitBigBinary(&bin, leftVal, rightVal, leftType, rightType)
		if binErr != nil {
			return binErr
		}
		ptr, dstTy, placeErr := fe.emitPlacePtr(call.Dst)
		if placeErr != nil {
			return placeErr
		}
		if dstTy != resultTy {
			dstTy = resultTy
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, val, ptr)
		return nil
	}
	info, ok := intInfo(fe.emitter.types, leftType)
	_, floatOK := floatInfo(fe.emitter.types, leftType)
	resultTy := leftTy
	tmp := fe.nextTemp()
	switch name {
	case "__add", "__sub", "__mul", "__div", "__mod", "__bit_and", "__bit_or", "__bit_xor", "__shl", "__shr":
		if ok {
			opcode := ""
			switch name {
			case "__add":
				opcode = "add"
			case "__sub":
				opcode = "sub"
			case "__mul":
				opcode = "mul"
			case "__div":
				if info.signed {
					opcode = "sdiv"
				} else {
					opcode = "udiv"
				}
			case "__mod":
				if info.signed {
					opcode = "srem"
				} else {
					opcode = "urem"
				}
			case "__bit_and":
				opcode = "and"
			case "__bit_or":
				opcode = "or"
			case "__bit_xor":
				opcode = "xor"
			case "__shl":
				opcode = "shl"
			case "__shr":
				if info.signed {
					opcode = "ashr"
				} else {
					opcode = "lshr"
				}
			}
			fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s, %s\n", tmp, opcode, leftTy, leftVal, rightVal)
			break
		}
		if !floatOK {
			return fmt.Errorf("unsupported numeric op on type")
		}
		opcode := ""
		switch name {
		case "__add":
			opcode = "fadd"
		case "__sub":
			opcode = "fsub"
		case "__mul":
			opcode = "fmul"
		case "__div":
			opcode = "fdiv"
		case "__mod":
			return fmt.Errorf("unsupported float op %s", name)
		case "__bit_and":
			return fmt.Errorf("unsupported float op %s", name)
		case "__bit_or":
			return fmt.Errorf("unsupported float op %s", name)
		case "__bit_xor":
			return fmt.Errorf("unsupported float op %s", name)
		case "__shl":
			return fmt.Errorf("unsupported float op %s", name)
		case "__shr":
			return fmt.Errorf("unsupported float op %s", name)
		}
		fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s, %s\n", tmp, opcode, leftTy, leftVal, rightVal)
	case "__eq", "__ne", "__lt", "__le", "__gt", "__ge":
		if ok {
			pred := ""
			switch name {
			case "__eq":
				pred = "eq"
			case "__ne":
				pred = "ne"
			case "__lt":
				if info.signed {
					pred = "slt"
				} else {
					pred = "ult"
				}
			case "__le":
				if info.signed {
					pred = "sle"
				} else {
					pred = "ule"
				}
			case "__gt":
				if info.signed {
					pred = "sgt"
				} else {
					pred = "ugt"
				}
			case "__ge":
				if info.signed {
					pred = "sge"
				} else {
					pred = "uge"
				}
			}
			if leftTy == "ptr" {
				if name != "__eq" && name != "__ne" {
					return fmt.Errorf("unsupported pointer comparison %s", name)
				}
				fmt.Fprintf(&fe.emitter.buf, "  %s = icmp %s ptr %s, %s\n", tmp, pred, leftVal, rightVal)
			} else {
				fmt.Fprintf(&fe.emitter.buf, "  %s = icmp %s %s %s, %s\n", tmp, pred, leftTy, leftVal, rightVal)
			}
			resultTy = "i1"
			break
		}
		if !floatOK {
			return fmt.Errorf("unsupported numeric op on type")
		}
		pred := ""
		switch name {
		case "__eq":
			pred = "oeq"
		case "__ne":
			pred = "one"
		case "__lt":
			pred = "olt"
		case "__le":
			pred = "ole"
		case "__gt":
			pred = "ogt"
		case "__ge":
			pred = "oge"
		}
		fmt.Fprintf(&fe.emitter.buf, "  %s = fcmp %s %s %s, %s\n", tmp, pred, leftTy, leftVal, rightVal)
		resultTy = "i1"
	default:
		return fmt.Errorf("unsupported magic binary op %s", name)
	}
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return err
	}
	if dstTy != resultTy {
		dstTy = resultTy
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
	return nil
}

func (fe *funcEmitter) emitMagicUnaryIntrinsic(call *mir.CallInstr, name string) error {
	if len(call.Args) != 1 {
		return fmt.Errorf("%s requires 1 argument", name)
	}
	if !call.HasDst {
		return nil
	}
	val, ty, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	operandType := operandValueType(fe.emitter.types, &call.Args[0])
	switch name {
	case "__pos":
		tmp = val
	case "__neg":
		if isBigIntType(fe.emitter.types, operandType) {
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_neg(ptr %s)\n", tmp, val)
			ty = "ptr"
			break
		}
		if isBigFloatType(fe.emitter.types, operandType) {
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_neg(ptr %s)\n", tmp, val)
			ty = "ptr"
			break
		}
		if isBigUintType(fe.emitter.types, operandType) {
			return fmt.Errorf("unsupported unary minus type")
		}
		if info, ok := intInfo(fe.emitter.types, operandType); ok {
			if !info.signed {
				return fmt.Errorf("unsupported unary minus type")
			}
			fmt.Fprintf(&fe.emitter.buf, "  %s = sub %s 0, %s\n", tmp, ty, val)
			break
		}
		if _, ok := floatInfo(fe.emitter.types, operandType); ok {
			fmt.Fprintf(&fe.emitter.buf, "  %s = fneg %s %s\n", tmp, ty, val)
			break
		}
		return fmt.Errorf("unsupported unary minus type")
	case "__not":
		if ty != "i1" {
			return fmt.Errorf("unary not requires i1, got %s", ty)
		}
		fmt.Fprintf(&fe.emitter.buf, "  %s = xor i1 %s, 1\n", tmp, val)
	default:
		return fmt.Errorf("unsupported magic unary op %s", name)
	}
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return err
	}
	if dstTy != ty {
		dstTy = ty
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
	return nil
}

func magicBinaryOp(name string) (ast.ExprBinaryOp, bool) {
	switch name {
	case "__add":
		return ast.ExprBinaryAdd, true
	case "__sub":
		return ast.ExprBinarySub, true
	case "__mul":
		return ast.ExprBinaryMul, true
	case "__div":
		return ast.ExprBinaryDiv, true
	case "__mod":
		return ast.ExprBinaryMod, true
	case "__bit_and":
		return ast.ExprBinaryBitAnd, true
	case "__bit_or":
		return ast.ExprBinaryBitOr, true
	case "__bit_xor":
		return ast.ExprBinaryBitXor, true
	case "__shl":
		return ast.ExprBinaryShiftLeft, true
	case "__shr":
		return ast.ExprBinaryShiftRight, true
	case "__eq":
		return ast.ExprBinaryEq, true
	case "__ne":
		return ast.ExprBinaryNotEq, true
	case "__lt":
		return ast.ExprBinaryLess, true
	case "__le":
		return ast.ExprBinaryLessEq, true
	case "__gt":
		return ast.ExprBinaryGreater, true
	case "__ge":
		return ast.ExprBinaryGreaterEq, true
	default:
		return 0, false
	}
}
