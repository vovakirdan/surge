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

	case ast.ExprBinaryAdd:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.IntAdd(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigInt(left.TypeID, res), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.UintAdd(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigUint(left.TypeID, res), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.FloatAdd(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			res, ok := AddInt64Checked(left.Int, right.Int)
			if !ok {
				return Value{}, vm.eb.intOverflow()
			}
			return MakeInt(res, left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinarySub:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.IntSub(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigInt(left.TypeID, res), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.UintSub(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigUint(left.TypeID, res), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.FloatSub(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			res, ok := SubInt64Checked(left.Int, right.Int)
			if !ok {
				return Value{}, vm.eb.intOverflow()
			}
			return MakeInt(res, left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryMul:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.IntMul(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigInt(left.TypeID, res), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.UintMul(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigUint(left.TypeID, res), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.FloatMul(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			res, ok := MulInt64Checked(left.Int, right.Int)
			if !ok {
				return Value{}, vm.eb.intOverflow()
			}
			return MakeInt(res, left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryDiv:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			q, _, err := bignum.IntDivMod(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigInt(left.TypeID, q), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			q, _, err := bignum.UintDivMod(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigUint(left.TypeID, q), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			res, err := bignum.FloatDiv(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigFloat(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			if right.Int == 0 {
				return Value{}, vm.eb.divisionByZero()
			}
			return MakeInt(left.Int/right.Int, left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryMod:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			_, r, err := bignum.IntDivMod(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigInt(left.TypeID, r), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			_, r, err := bignum.UintDivMod(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigUint(left.TypeID, r), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			if right.Int == 0 {
				return Value{}, vm.eb.divisionByZero()
			}
			return MakeInt(left.Int%right.Int, left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryEq:
		if left.Kind != right.Kind {
			return Value{}, vm.eb.typeMismatch(left.Kind.String(), right.Kind.String())
		}
		var result bool
		switch left.Kind {
		case VKInt:
			result = left.Int == right.Int
		case VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			result = a.Cmp(b) == 0
		case VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			result = a.Cmp(b) == 0
		case VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			result = a.Cmp(b) == 0
		case VKBool:
			result = left.Bool == right.Bool
		case VKHandleString:
			lObj := vm.Heap.Get(left.H)
			rObj := vm.Heap.Get(right.H)
			if lObj == nil || rObj == nil {
				return Value{}, vm.eb.makeError(PanicOutOfBounds, "invalid string handle")
			}
			result = lObj.Str == rObj.Str
		default:
			result = left.H == right.H
		}
		return MakeBool(result, types.NoTypeID), nil

	case ast.ExprBinaryNotEq:
		if left.Kind != right.Kind {
			return Value{}, vm.eb.typeMismatch(left.Kind.String(), right.Kind.String())
		}
		var result bool
		switch left.Kind {
		case VKInt:
			result = left.Int != right.Int
		case VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			result = a.Cmp(b) != 0
		case VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			result = a.Cmp(b) != 0
		case VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			result = a.Cmp(b) != 0
		case VKBool:
			result = left.Bool != right.Bool
		case VKHandleString:
			lObj := vm.Heap.Get(left.H)
			rObj := vm.Heap.Get(right.H)
			if lObj == nil || rObj == nil {
				return Value{}, vm.eb.makeError(PanicOutOfBounds, "invalid string handle")
			}
			result = lObj.Str != rObj.Str
		default:
			result = left.H != right.H
		}
		return MakeBool(result, types.NoTypeID), nil

	case ast.ExprBinaryLess:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) < 0, types.NoTypeID), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) < 0, types.NoTypeID), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) < 0, types.NoTypeID), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			return MakeBool(left.Int < right.Int, types.NoTypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryLessEq:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) <= 0, types.NoTypeID), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) <= 0, types.NoTypeID), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) <= 0, types.NoTypeID), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			return MakeBool(left.Int <= right.Int, types.NoTypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryGreater:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) > 0, types.NoTypeID), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) > 0, types.NoTypeID), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) > 0, types.NoTypeID), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			return MakeBool(left.Int > right.Int, types.NoTypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryGreaterEq:
		switch {
		case left.Kind == VKBigInt && right.Kind == VKBigInt:
			a, vmErr := vm.mustBigInt(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigInt(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) >= 0, types.NoTypeID), nil
		case left.Kind == VKBigUint && right.Kind == VKBigUint:
			a, vmErr := vm.mustBigUint(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigUint(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) >= 0, types.NoTypeID), nil
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			return MakeBool(a.Cmp(b) >= 0, types.NoTypeID), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			return MakeBool(left.Int >= right.Int, types.NoTypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("binary op %s", op))
	}
}

// evalUnaryOp evaluates a unary operation.
func (vm *VM) evalUnaryOp(op ast.ExprUnaryOp, operand Value) (Value, *VMError) {
	switch op {
	case ast.ExprUnaryMinus:
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
			if operand.Int == math.MinInt64 {
				return Value{}, vm.eb.intOverflow()
			}
			return MakeInt(-operand.Int, operand.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", operand.Kind.String())
		}

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
		switch operand.Kind {
		case VKRef, VKRefMut:
			v, vmErr := vm.loadLocationRaw(operand.Loc)
			if vmErr != nil {
				return Value{}, vmErr
			}
			if v.IsHeap() && v.H != 0 {
				vm.Heap.Retain(v.H)
			}
			return v, nil
		default:
			return Value{}, vm.eb.derefOnNonRef(operand.Kind.String())
		}

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("unary op %s", op))
	}
}
