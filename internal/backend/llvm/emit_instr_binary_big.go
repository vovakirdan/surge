package llvm

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/mir"
	"surge/internal/types"
)

// Arithmetic and comparison on integers too wide to sit in a register.
//
// A bignum operand is a runtime object rather than an LLVM value, so every
// operation here is a call and every result is owned — which is what separates
// this from the machine-word arithmetic next door, not the operator spelling.
// WidthAny int addition, subtraction and comparison first try the inline
// fixnum path (emit_numeric_fixnum_fast.go) and call only for a heap operand
// or a result outside the inline range.

// bigComparePredicate maps a comparison onto the signed icmp predicate applied
// to a runtime three-way answer or to two decoded fixnums.
func bigComparePredicate(op ast.ExprBinaryOp) (string, error) {
	switch op {
	case ast.ExprBinaryEq:
		return "eq", nil
	case ast.ExprBinaryNotEq:
		return "ne", nil
	case ast.ExprBinaryLess:
		return "slt", nil
	case ast.ExprBinaryLessEq:
		return "sle", nil
	case ast.ExprBinaryGreater:
		return "sgt", nil
	case ast.ExprBinaryGreaterEq:
		return "sge", nil
	default:
		return "", fmt.Errorf("unsupported compare op %v", op)
	}
}

func (fe *funcEmitter) emitBigCompare(fn string, op ast.ExprBinaryOp, leftVal, rightVal string) (val, ty string, err error) {
	pred, err := bigComparePredicate(op)
	if err != nil {
		return "", "", err
	}
	cmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i32 @%s(ptr %s, ptr %s)\n", cmp, fn, leftVal, rightVal)
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp %s i32 %s, 0\n", tmp, pred, cmp)
	return tmp, "i1", nil
}

func (fe *funcEmitter) emitBigBinary(op *mir.BinaryOp, leftVal, rightVal string, leftTypeID, rightTypeID types.TypeID) (val, ty string, err error) {
	leftBigInt := isBigIntType(fe.emitter.types, leftTypeID)
	leftBigUint := isBigUintType(fe.emitter.types, leftTypeID)
	leftBigFloat := isBigFloatType(fe.emitter.types, leftTypeID)
	rightBigInt := isBigIntType(fe.emitter.types, rightTypeID)
	rightBigUint := isBigUintType(fe.emitter.types, rightTypeID)
	rightBigFloat := isBigFloatType(fe.emitter.types, rightTypeID)

	if leftBigInt != rightBigInt || leftBigUint != rightBigUint || leftBigFloat != rightBigFloat {
		formatType := func(id types.TypeID) string {
			if fe.emitter == nil || fe.emitter.types == nil || id == types.NoTypeID {
				return fmt.Sprintf("type#%d", id)
			}
			id = resolveAliasAndOwn(fe.emitter.types, id)
			tt, ok := fe.emitter.types.Lookup(id)
			if !ok {
				return fmt.Sprintf("type#%d", id)
			}
			switch tt.Kind {
			case types.KindInt, types.KindUint, types.KindFloat:
				return fmt.Sprintf("%s(%d)", tt.Kind.String(), tt.Width)
			default:
				return tt.Kind.String()
			}
		}
		return "", "", fmt.Errorf("mixed big numeric operands: left=%s right=%s", formatType(leftTypeID), formatType(rightTypeID))
	}

	switch {
	case leftBigInt:
		switch op.Op {
		case ast.ExprBinaryAdd:
			tmp, arithErr := fe.emitFixnumIntArith("add", "rt_bigint_add", leftVal, rightVal)
			return tmp, "ptr", arithErr
		case ast.ExprBinarySub:
			tmp, arithErr := fe.emitFixnumIntArith("sub", "rt_bigint_sub", leftVal, rightVal)
			return tmp, "ptr", arithErr
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
			tmp, cmpErr := fe.emitFixnumIntCompare(op.Op, leftVal, rightVal)
			return tmp, "i1", cmpErr
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
