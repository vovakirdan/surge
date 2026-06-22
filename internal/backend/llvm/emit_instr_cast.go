package llvm

import (
	"fmt"
	"strconv"
	"strings"

	"surge/internal/ast"
	"surge/internal/mir"
	"surge/internal/numlit"
	"surge/internal/types"
)

func (fe *funcEmitter) emitCast(c *mir.CastOp) (val, ty string, err error) {
	if c == nil {
		return "", "", fmt.Errorf("nil cast")
	}
	if constVal, constTy, ok, constErr := fe.emitConstIntegerCast(c); ok || constErr != nil {
		return constVal, constTy, constErr
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

type constIntegerValue struct {
	negative  bool
	magnitude uint64
}

func (fe *funcEmitter) emitConstIntegerCast(c *mir.CastOp) (val, ty string, ok bool, err error) {
	if c.Value.Kind != mir.OperandConst {
		return "", "", false, nil
	}
	if isBoolType(fe.emitter.types, c.TargetTy) {
		return "", "", false, nil
	}
	dstInfo, dstOK := intInfo(fe.emitter.types, c.TargetTy)
	if !dstOK {
		return "", "", false, nil
	}
	lit, litOK := constIntegerLiteralValue(&c.Value.Const)
	if !litOK {
		return "", "", false, nil
	}
	imm, fits := formatConstIntegerForTarget(lit, dstInfo)
	if !fits {
		return "", "", false, nil
	}
	dstTy, err := llvmValueType(fe.emitter.types, c.TargetTy)
	if err != nil {
		return "", "", false, err
	}
	return imm, dstTy, true, nil
}

func constIntegerLiteralValue(c *mir.Const) (constIntegerValue, bool) {
	if c == nil {
		return constIntegerValue{}, false
	}
	switch c.Kind {
	case mir.ConstInt:
		if c.Text != "" {
			return parseIntegerLiteralText(c.Text)
		}
		return integerValueFromInt64(c.IntValue)
	case mir.ConstUint:
		if c.Text != "" {
			return parseIntegerLiteralText(c.Text)
		}
		return constIntegerValue{magnitude: c.UintValue}, true
	default:
		return constIntegerValue{}, false
	}
}

func integerValueFromInt64(value int64) (constIntegerValue, bool) {
	text := strconv.FormatInt(value, 10)
	if strings.HasPrefix(text, "-") {
		magnitude, err := strconv.ParseUint(strings.TrimPrefix(text, "-"), 10, 64)
		if err != nil {
			return constIntegerValue{}, false
		}
		return constIntegerValue{negative: true, magnitude: magnitude}, true
	}
	magnitude, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return constIntegerValue{}, false
	}
	return constIntegerValue{magnitude: magnitude}, true
}

func parseIntegerLiteralText(text string) (constIntegerValue, bool) {
	clean := strings.ReplaceAll(strings.TrimSpace(text), "_", "")
	if clean == "" {
		return constIntegerValue{}, false
	}
	negative := false
	switch clean[0] {
	case '-':
		negative = true
		clean = clean[1:]
	case '+':
		clean = clean[1:]
	}
	if clean == "" {
		return constIntegerValue{}, false
	}
	magnitude, ok := numlit.ParseUint64(clean)
	if !ok {
		return constIntegerValue{}, false
	}
	return constIntegerValue{negative: negative, magnitude: magnitude}, true
}

func formatConstIntegerForTarget(v constIntegerValue, info intMeta) (string, bool) {
	if info.bits <= 0 || info.bits > 64 {
		return "", false
	}
	if info.signed {
		maxPositive := uint64(1)<<(info.bits-1) - 1
		maxNegativeMagnitude := uint64(1) << (info.bits - 1)
		if v.negative {
			if v.magnitude > maxNegativeMagnitude {
				return "", false
			}
			return "-" + strconv.FormatUint(v.magnitude, 10), true
		}
		if v.magnitude > maxPositive {
			return "", false
		}
		return strconv.FormatUint(v.magnitude, 10), true
	}
	if v.negative {
		return "", false
	}
	maxValue := ^uint64(0)
	if info.bits < 64 {
		maxValue = uint64(1)<<info.bits - 1
	}
	if v.magnitude > maxValue {
		return "", false
	}
	return strconv.FormatUint(v.magnitude, 10), true
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
			return "", "", fmt.Errorf("unsupported deref type %s (id=%d) for %s operand", types.Label(fe.emitter.types, op.Operand.Type), op.Operand.Type, op.Operand.Kind)
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
