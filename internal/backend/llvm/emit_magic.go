package llvm

import (
	"fmt"

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
		return isStringLike(fe.emitter.types, leftType) && isStringLike(fe.emitter.types, rightType)
	}
	_, okLeft := intInfo(fe.emitter.types, leftType)
	_, okRight := intInfo(fe.emitter.types, rightType)
	return okLeft && okRight
}

func (fe *funcEmitter) canEmitMagicUnary(call *mir.CallInstr) bool {
	if call == nil || len(call.Args) != 1 {
		return false
	}
	operandType := operandValueType(fe.emitter.types, &call.Args[0])
	_, ok := intInfo(fe.emitter.types, operandType)
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

	leftVal, leftTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return err
	}
	rightVal, rightTy, err := fe.emitValueOperand(&call.Args[1])
	if err != nil {
		return err
	}
	if leftTy != rightTy {
		return fmt.Errorf("binary operand type mismatch: %s vs %s", leftTy, rightTy)
	}
	info, ok := intInfo(fe.emitter.types, leftType)
	if !ok && leftTy != "ptr" {
		return fmt.Errorf("unsupported numeric op on type")
	}
	resultTy := leftTy
	tmp := fe.nextTemp()
	switch name {
	case "__add", "__sub", "__mul", "__div", "__mod", "__bit_and", "__bit_or", "__bit_xor", "__shl", "__shr":
		if !ok {
			return fmt.Errorf("unsupported numeric op on type")
		}
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
	case "__eq", "__ne", "__lt", "__le", "__gt", "__ge":
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
	switch name {
	case "__pos":
		tmp = val
	case "__neg":
		info, ok := intInfo(fe.emitter.types, operandValueType(fe.emitter.types, &call.Args[0]))
		if !ok || !info.signed {
			return fmt.Errorf("unsupported unary minus type")
		}
		fmt.Fprintf(&fe.emitter.buf, "  %s = sub %s 0, %s\n", tmp, ty, val)
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
