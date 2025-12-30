package vm

import (
	"fmt"
	"math"

	"surge/internal/ast"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// evalBinaryOp evaluates a binary operation.
func (vm *VM) evalBinaryOp(op ast.ExprBinaryOp, left, right Value) (Value, *VMError) {
	switch op {
	case ast.ExprBinaryRange, ast.ExprBinaryRangeInclusive:
		return vm.evalRange(op, left, right)

	case ast.ExprBinaryAdd:
		return vm.evalAdd(left, right)

	case ast.ExprBinarySub:
		return vm.evalSub(left, right)

	case ast.ExprBinaryLogicalAnd:
		return vm.evalLogicalAnd(left, right)

	case ast.ExprBinaryLogicalOr:
		return vm.evalLogicalOr(left, right)

	case ast.ExprBinaryMul:
		return vm.evalMul(left, right)

	case ast.ExprBinaryDiv:
		return vm.evalDiv(left, right)

	case ast.ExprBinaryMod:
		return vm.evalMod(left, right)

	case ast.ExprBinaryBitAnd:
		return vm.evalBitAnd(left, right)

	case ast.ExprBinaryBitOr:
		return vm.evalBitOr(left, right)

	case ast.ExprBinaryBitXor:
		return vm.evalBitXor(left, right)

	case ast.ExprBinaryShiftLeft:
		return vm.evalShiftLeft(left, right)

	case ast.ExprBinaryShiftRight:
		return vm.evalShiftRight(left, right)

	case ast.ExprBinaryEq:
		return vm.evalEqual(left, right)

	case ast.ExprBinaryNotEq:
		return vm.evalNotEqual(left, right)

	case ast.ExprBinaryLess:
		return vm.evalLess(left, right)

	case ast.ExprBinaryLessEq:
		return vm.evalLessEq(left, right)

	case ast.ExprBinaryGreater:
		return vm.evalGreater(left, right)

	case ast.ExprBinaryGreaterEq:
		return vm.evalGreaterEq(left, right)

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("binary op %s", op))
	}
}

// evalRange evaluates a range operation.
func (vm *VM) evalRange(op ast.ExprBinaryOp, left, right Value) (Value, *VMError) {
	inclusive := op == ast.ExprBinaryRangeInclusive
	startVal, vmErr := vm.cloneForShare(left)
	if vmErr != nil {
		return Value{}, vmErr
	}
	endVal, vmErr := vm.cloneForShare(right)
	if vmErr != nil {
		vm.dropValue(startVal)
		return Value{}, vmErr
	}
	h := vm.Heap.AllocRange(types.NoTypeID, startVal, endVal, true, true, inclusive)
	return MakeHandleRange(h, types.NoTypeID), nil
}

// evalLogicalAnd evaluates the logical AND operation.
func (vm *VM) evalLogicalAnd(left, right Value) (Value, *VMError) {
	if left.Kind != VKBool || right.Kind != VKBool {
		return Value{}, vm.eb.typeMismatch("bool", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
	return MakeBool(left.Bool && right.Bool, left.TypeID), nil
}

// evalLogicalOr evaluates the logical OR operation.
func (vm *VM) evalLogicalOr(left, right Value) (Value, *VMError) {
	if left.Kind != VKBool || right.Kind != VKBool {
		return Value{}, vm.eb.typeMismatch("bool", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
	return MakeBool(left.Bool || right.Bool, left.TypeID), nil
}

// evalUnaryOp evaluates a unary operation.
func (vm *VM) evalUnaryOp(op ast.ExprUnaryOp, operand Value) (Value, *VMError) {
	switch op {
	case ast.ExprUnaryMinus:
		return vm.evalUnaryMinus(operand)

	case ast.ExprUnaryNot:
		if operand.Kind != VKBool {
			return Value{}, vm.eb.typeMismatch("bool", operand.Kind.String())
		}
		return MakeBool(!operand.Bool, operand.TypeID), nil

	case ast.ExprUnaryPlus:
		switch operand.Kind {
		case VKBigInt, VKBigUint, VKBigFloat, VKInt:
			return operand, nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", operand.Kind.String())
		}

	case ast.ExprUnaryDeref:
		return vm.evalUnaryDeref(operand)

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("unary op %s", op))
	}
}

// evalUnaryMinus evaluates the unary minus operation.
func (vm *VM) evalUnaryMinus(operand Value) (Value, *VMError) {
	switch operand.Kind {
	case VKBigInt:
		i, vmErr := vm.mustBigInt(operand)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return vm.makeBigInt(operand.TypeID, i.Negated()), nil
	case VKBigFloat:
		f, vmErr := vm.mustBigFloat(operand)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return vm.makeBigFloat(operand.TypeID, bignum.FloatNeg(f)), nil
	case VKInt:
		kind, width, ok := vm.numericKind(operand.TypeID)
		if !ok {
			return Value{}, vm.eb.typeMismatch("signed int", operand.Kind.String())
		}
		if kind == types.KindUint {
			return Value{}, vm.eb.typeMismatch("signed int", "uint")
		}
		minVal := int64(math.MinInt64)
		if width != types.WidthAny {
			if minValRange, _, ok := intRangeForWidth(width); ok {
				minVal = minValRange
			}
		}
		if operand.Int == minVal {
			return Value{}, vm.eb.intOverflow()
		}
		res := -operand.Int
		if !checkSignedWidth(res, width) {
			return Value{}, vm.eb.intOverflow()
		}
		return MakeInt(res, operand.TypeID), nil
	default:
		return Value{}, vm.eb.typeMismatch("numeric", operand.Kind.String())
	}
}

// evalUnaryDeref evaluates the dereference operation.
func (vm *VM) evalUnaryDeref(operand Value) (Value, *VMError) {
	switch operand.Kind {
	case VKRef, VKRefMut:
		v, vmErr := vm.loadLocationRaw(operand.Loc)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if vm.Types != nil && operand.TypeID != types.NoTypeID {
			expected := types.NoTypeID
			id := operand.TypeID
			for i := 0; i < 32 && id != types.NoTypeID; i++ {
				tt, ok := vm.Types.Lookup(id)
				if !ok {
					break
				}
				switch tt.Kind {
				case types.KindAlias:
					target, ok := vm.Types.AliasTarget(id)
					if !ok || target == types.NoTypeID || target == id {
						id = types.NoTypeID
						continue
					}
					id = target
					continue
				case types.KindOwn:
					id = tt.Elem
					continue
				case types.KindReference, types.KindPointer:
					expected = tt.Elem
				}
				break
			}
			if expected != types.NoTypeID {
				if retagged, ok := vm.retagUnionValue(v, expected); ok {
					v = retagged
				}
			}
		}
		if v.IsHeap() && v.H != 0 {
			vm.Heap.Retain(v.H)
		}
		return v, nil
	default:
		return Value{}, vm.eb.derefOnNonRef(operand.Kind.String())
	}
}

// shiftCountFromBigUint converts a BigUint to an int shift count.
func shiftCountFromBigUint(u bignum.BigUint) (int, bool) {
	val, ok := u.Uint64()
	if !ok {
		return 0, false
	}
	maxInt := uint64(int(^uint(0) >> 1))
	if val > maxInt {
		return 0, false
	}
	return int(val), true
}

// shiftCountFromBigInt converts a BigInt to an int shift count.
func shiftCountFromBigInt(i bignum.BigInt) (int, bool) {
	if i.Neg && !i.IsZero() {
		return 0, false
	}
	return shiftCountFromBigUint(i.Abs())
}
