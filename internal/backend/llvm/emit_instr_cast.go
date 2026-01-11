package llvm

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/mir"
)

func (fe *funcEmitter) emitCast(c *mir.CastOp) (val, ty string, err error) {
	if c == nil {
		return "", "", fmt.Errorf("nil cast")
	}
	val, srcTy, err := fe.emitOperand(&c.Value)
	if err != nil {
		return "", "", err
	}
	dstTy, err := llvmValueType(fe.emitter.types, c.TargetTy)
	if err != nil {
		return "", "", err
	}
	if srcTy == dstTy {
		srcType := resolveAliasAndOwn(fe.emitter.types, c.Value.Type)
		dstType := resolveAliasAndOwn(fe.emitter.types, c.TargetTy)
		if isUnionType(fe.emitter.types, c.Value.Type) && isUnionType(fe.emitter.types, c.TargetTy) {
			return fe.emitUnionCast(val, c.Value.Type, c.TargetTy)
		}
		if (isBigIntType(fe.emitter.types, srcType) || isBigUintType(fe.emitter.types, srcType) || isBigFloatType(fe.emitter.types, srcType) ||
			isBigIntType(fe.emitter.types, dstType) || isBigUintType(fe.emitter.types, dstType) || isBigFloatType(fe.emitter.types, dstType)) && srcType != dstType {
			return fe.emitNumericCast(val, srcTy, c.Value.Type, c.TargetTy)
		}
		return val, dstTy, nil
	}
	if isUnionType(fe.emitter.types, c.Value.Type) && isUnionType(fe.emitter.types, c.TargetTy) {
		return fe.emitUnionCast(val, c.Value.Type, c.TargetTy)
	}
	if isBigIntType(fe.emitter.types, c.Value.Type) || isBigUintType(fe.emitter.types, c.Value.Type) || isBigFloatType(fe.emitter.types, c.Value.Type) ||
		isBigIntType(fe.emitter.types, c.TargetTy) || isBigUintType(fe.emitter.types, c.TargetTy) || isBigFloatType(fe.emitter.types, c.TargetTy) {
		return fe.emitNumericCast(val, srcTy, c.Value.Type, c.TargetTy)
	}
	srcInfo, srcOK := intInfo(fe.emitter.types, c.Value.Type)
	dstInfo, dstOK := intInfo(fe.emitter.types, c.TargetTy)
	_, srcFloatOK := floatInfo(fe.emitter.types, c.Value.Type)
	_, dstFloatOK := floatInfo(fe.emitter.types, c.TargetTy)
	if srcOK && dstOK {
		if srcInfo.bits < dstInfo.bits {
			op := "zext"
			if srcInfo.signed {
				op = "sext"
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s to %s\n", tmp, op, srcTy, val, dstTy)
			return tmp, dstTy, nil
		}
		if srcInfo.bits > dstInfo.bits {
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = trunc %s %s to %s\n", tmp, srcTy, val, dstTy)
			return tmp, dstTy, nil
		}
		return val, dstTy, nil
	}
	if (srcOK || srcFloatOK) && (dstOK || dstFloatOK) {
		return fe.emitNumericCast(val, srcTy, c.Value.Type, c.TargetTy)
	}
	return "", "", fmt.Errorf("unsupported cast to %s", dstTy)
}

func (fe *funcEmitter) emitUnary(op *mir.UnaryOp) (val, ty string, err error) {
	if op == nil {
		return "", "", fmt.Errorf("nil unary op")
	}
	switch op.Op {
	case ast.ExprUnaryPlus:
		return fe.emitValueOperand(&op.Operand)
	case ast.ExprUnaryOwn:
		return fe.emitValueOperand(&op.Operand)
	case ast.ExprUnaryMinus:
		val, ty, err := fe.emitValueOperand(&op.Operand)
		if err != nil {
			return "", "", err
		}
		if isBigIntType(fe.emitter.types, op.Operand.Type) {
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_neg(ptr %s)\n", tmp, val)
			return tmp, "ptr", nil
		}
		if isBigFloatType(fe.emitter.types, op.Operand.Type) {
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_neg(ptr %s)\n", tmp, val)
			return tmp, "ptr", nil
		}
		if isBigUintType(fe.emitter.types, op.Operand.Type) {
			return "", "", fmt.Errorf("unsupported unary minus type")
		}
		info, ok := intInfo(fe.emitter.types, op.Operand.Type)
		if !ok || !info.signed {
			return "", "", fmt.Errorf("unsupported unary minus type")
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = sub %s 0, %s\n", tmp, ty, val)
		return tmp, ty, nil
	case ast.ExprUnaryNot:
		val, ty, err := fe.emitValueOperand(&op.Operand)
		if err != nil {
			return "", "", err
		}
		if ty != "i1" {
			return "", "", fmt.Errorf("unary not requires i1, got %s", ty)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = xor i1 %s, 1\n", tmp, val)
		return tmp, "i1", nil
	case ast.ExprUnaryDeref:
		ptrVal, _, err := fe.emitOperand(&op.Operand)
		if err != nil {
			return "", "", err
		}
		elemType, ok := derefType(fe.emitter.types, op.Operand.Type)
		if !ok {
			return "", "", fmt.Errorf("unsupported deref type")
		}
		elemLLVM, err := llvmValueType(fe.emitter.types, elemType)
		if err != nil {
			return "", "", err
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", tmp, elemLLVM, ptrVal)
		return tmp, elemLLVM, nil
	default:
		return "", "", fmt.Errorf("unsupported unary op %v", op.Op)
	}
}
