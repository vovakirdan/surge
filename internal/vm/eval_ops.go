package vm

import (
	"fmt"
	"math"
	"math/bits"

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
		case left.Kind == VKHandleString && right.Kind == VKHandleString:
			return vm.concatStringValues(left, right)
		case left.Kind == VKHandleArray && right.Kind == VKHandleArray:
			leftView, vmErr := vm.arrayViewFromHandle(left.H)
			if vmErr != nil {
				return Value{}, vmErr
			}
			rightView, vmErr := vm.arrayViewFromHandle(right.H)
			if vmErr != nil {
				return Value{}, vmErr
			}
			elems := make([]Value, 0, leftView.length+rightView.length)
			for i := range leftView.length {
				v, vmErr := vm.cloneForShare(leftView.baseObj.Arr[leftView.start+i])
				if vmErr != nil {
					for _, el := range elems {
						vm.dropValue(el)
					}
					return Value{}, vmErr
				}
				elems = append(elems, v)
			}
			for i := range rightView.length {
				v, vmErr := vm.cloneForShare(rightView.baseObj.Arr[rightView.start+i])
				if vmErr != nil {
					for _, el := range elems {
						vm.dropValue(el)
					}
					return Value{}, vmErr
				}
				elems = append(elems, v)
			}
			arrType := left.TypeID
			if arrType == types.NoTypeID {
				arrType = right.TypeID
			}
			h := vm.Heap.AllocArray(arrType, elems)
			return MakeHandleArray(h, arrType), nil
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
			if vmErr := vm.checkFloatWidth(res, left.TypeID); vmErr != nil {
				return Value{}, vmErr
			}
			return vm.makeBigFloat(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			kind, width, ok := vm.numericKind(left.TypeID)
			if !ok {
				return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
			}
			if kind == types.KindUint {
				sum, carry := bits.Add64(uint64(left.Int), uint64(right.Int), 0)
				if carry != 0 || !checkUnsignedWidth(sum, width) {
					return Value{}, vm.eb.intOverflow()
				}
				return MakeInt(int64(sum), left.TypeID), nil
			}
			res, ok := AddInt64Checked(left.Int, right.Int)
			if !ok || !checkSignedWidth(res, width) {
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
			if vmErr := vm.checkFloatWidth(res, left.TypeID); vmErr != nil {
				return Value{}, vmErr
			}
			return vm.makeBigFloat(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			kind, width, ok := vm.numericKind(left.TypeID)
			if !ok {
				return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
			}
			if kind == types.KindUint {
				ua := uint64(left.Int)
				ub := uint64(right.Int)
				if ua < ub {
					return Value{}, vm.eb.intOverflow()
				}
				res := ua - ub
				if !checkUnsignedWidth(res, width) {
					return Value{}, vm.eb.intOverflow()
				}
				return MakeInt(int64(res), left.TypeID), nil
			}
			res, ok := SubInt64Checked(left.Int, right.Int)
			if !ok || !checkSignedWidth(res, width) {
				return Value{}, vm.eb.intOverflow()
			}
			return MakeInt(res, left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryLogicalAnd:
		if left.Kind != VKBool || right.Kind != VKBool {
			return Value{}, vm.eb.typeMismatch("bool", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		return MakeBool(left.Bool && right.Bool, left.TypeID), nil

	case ast.ExprBinaryLogicalOr:
		if left.Kind != VKBool || right.Kind != VKBool {
			return Value{}, vm.eb.typeMismatch("bool", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		return MakeBool(left.Bool || right.Bool, left.TypeID), nil

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
			if vmErr := vm.checkFloatWidth(res, left.TypeID); vmErr != nil {
				return Value{}, vmErr
			}
			return vm.makeBigFloat(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			kind, width, ok := vm.numericKind(left.TypeID)
			if !ok {
				return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
			}
			if kind == types.KindUint {
				hi, lo := bits.Mul64(uint64(left.Int), uint64(right.Int))
				if hi != 0 || !checkUnsignedWidth(lo, width) {
					return Value{}, vm.eb.intOverflow()
				}
				return MakeInt(int64(lo), left.TypeID), nil
			}
			res, ok := MulInt64Checked(left.Int, right.Int)
			if !ok || !checkSignedWidth(res, width) {
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
			if vmErr := vm.checkFloatWidth(res, left.TypeID); vmErr != nil {
				return Value{}, vmErr
			}
			return vm.makeBigFloat(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			kind, width, ok := vm.numericKind(left.TypeID)
			if !ok {
				return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
			}
			if kind == types.KindUint {
				ua := uint64(left.Int)
				ub := uint64(right.Int)
				if ub == 0 {
					return Value{}, vm.eb.divisionByZero()
				}
				res := ua / ub
				if !checkUnsignedWidth(res, width) {
					return Value{}, vm.eb.intOverflow()
				}
				return MakeInt(int64(res), left.TypeID), nil
			}
			if right.Int == 0 {
				return Value{}, vm.eb.divisionByZero()
			}
			minVal := int64(math.MinInt64)
			if width != types.WidthAny {
				if min, _, ok := intRangeForWidth(width); ok {
					minVal = min
				}
			}
			if right.Int == -1 && left.Int == minVal {
				return Value{}, vm.eb.intOverflow()
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
		case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
			a, vmErr := vm.mustBigFloat(left)
			if vmErr != nil {
				return Value{}, vmErr
			}
			b, vmErr := vm.mustBigFloat(right)
			if vmErr != nil {
				return Value{}, vmErr
			}
			q, err := bignum.FloatDiv(a, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			qi, err := bignum.FloatToIntTrunc(q)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			qf, err := bignum.FloatFromInt(qi)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			prod, err := bignum.FloatMul(qf, b)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			res, err := bignum.FloatSub(a, prod)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			if vmErr := vm.checkFloatWidth(res, left.TypeID); vmErr != nil {
				return Value{}, vmErr
			}
			return vm.makeBigFloat(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			kind, width, ok := vm.numericKind(left.TypeID)
			if !ok {
				return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
			}
			if kind == types.KindUint {
				ua := uint64(left.Int)
				ub := uint64(right.Int)
				if ub == 0 {
					return Value{}, vm.eb.divisionByZero()
				}
				res := ua % ub
				if !checkUnsignedWidth(res, width) {
					return Value{}, vm.eb.intOverflow()
				}
				return MakeInt(int64(res), left.TypeID), nil
			}
			if right.Int == 0 {
				return Value{}, vm.eb.divisionByZero()
			}
			return MakeInt(left.Int%right.Int, left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryBitAnd:
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
			res, err := bignum.IntAnd(a, b)
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
			res := bignum.UintAnd(a, b)
			return vm.makeBigUint(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			kind, width, ok := vm.numericKind(left.TypeID)
			if !ok {
				return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
			}
			mask := maskForWidth(width)
			res := (uint64(left.Int) & uint64(right.Int)) & mask
			if kind == types.KindUint {
				return MakeInt(int64(res), left.TypeID), nil
			}
			return MakeInt(signExtendUnsigned(res, width), left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryBitOr:
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
			res, err := bignum.IntOr(a, b)
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
			res := bignum.UintOr(a, b)
			return vm.makeBigUint(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			kind, width, ok := vm.numericKind(left.TypeID)
			if !ok {
				return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
			}
			mask := maskForWidth(width)
			res := (uint64(left.Int) | uint64(right.Int)) & mask
			if kind == types.KindUint {
				return MakeInt(int64(res), left.TypeID), nil
			}
			return MakeInt(signExtendUnsigned(res, width), left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryBitXor:
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
			res, err := bignum.IntXor(a, b)
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
			res := bignum.UintXor(a, b)
			return vm.makeBigUint(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			kind, width, ok := vm.numericKind(left.TypeID)
			if !ok {
				return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
			}
			mask := maskForWidth(width)
			res := (uint64(left.Int) ^ uint64(right.Int)) & mask
			if kind == types.KindUint {
				return MakeInt(int64(res), left.TypeID), nil
			}
			return MakeInt(signExtendUnsigned(res, width), left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryShiftLeft:
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
			shift, ok := shiftCountFromBigInt(b)
			if !ok {
				return Value{}, vm.eb.intOverflow()
			}
			res, err := bignum.IntShl(a, shift)
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
			shift, ok := shiftCountFromBigUint(b)
			if !ok {
				return Value{}, vm.eb.intOverflow()
			}
			res, err := bignum.UintShl(a, shift)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigUint(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			kind, width, ok := vm.numericKind(left.TypeID)
			if !ok {
				return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
			}
			bits, _ := widthBits(width)
			if right.Int < 0 || right.Int >= int64(bits) {
				return Value{}, vm.eb.intOverflow()
			}
			shift := uint(right.Int)
			mask := maskForWidth(width)
			val := uint64(left.Int) & mask
			res := val << shift
			if kind == types.KindUint {
				if !checkUnsignedWidth(res, width) {
					return Value{}, vm.eb.intOverflow()
				}
				return MakeInt(int64(res), left.TypeID), nil
			}
			if res&^mask != 0 {
				return Value{}, vm.eb.intOverflow()
			}
			return MakeInt(signExtendUnsigned(res, width), left.TypeID), nil
		default:
			return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}

	case ast.ExprBinaryShiftRight:
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
			shift, ok := shiftCountFromBigInt(b)
			if !ok {
				return Value{}, vm.eb.intOverflow()
			}
			res, err := bignum.IntShr(a, shift)
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
			shift, ok := shiftCountFromBigUint(b)
			if !ok {
				return Value{}, vm.eb.intOverflow()
			}
			res, err := bignum.UintShr(a, shift)
			if err != nil {
				return Value{}, vm.bignumErr(err)
			}
			return vm.makeBigUint(left.TypeID, res), nil
		case left.Kind == VKInt && right.Kind == VKInt:
			kind, width, ok := vm.numericKind(left.TypeID)
			if !ok {
				return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
			}
			bits, _ := widthBits(width)
			if right.Int < 0 || right.Int >= int64(bits) {
				return Value{}, vm.eb.intOverflow()
			}
			shift := uint(right.Int)
			mask := maskForWidth(width)
			if kind == types.KindUint {
				val := uint64(left.Int) & mask
				res := val >> shift
				return MakeInt(int64(res), left.TypeID), nil
			}
			val := signExtendUnsigned(uint64(left.Int)&mask, width)
			return MakeInt(val>>shift, left.TypeID), nil
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
			result = vm.stringBytes(lObj) == vm.stringBytes(rObj)
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
			result = vm.stringBytes(lObj) != vm.stringBytes(rObj)
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
			kind, _, ok := vm.numericKind(left.TypeID)
			if !ok {
				return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
			}
			if kind == types.KindUint {
				return MakeBool(uint64(left.Int) < uint64(right.Int), types.NoTypeID), nil
			}
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
			kind, _, ok := vm.numericKind(left.TypeID)
			if !ok {
				return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
			}
			if kind == types.KindUint {
				return MakeBool(uint64(left.Int) <= uint64(right.Int), types.NoTypeID), nil
			}
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
			kind, _, ok := vm.numericKind(left.TypeID)
			if !ok {
				return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
			}
			if kind == types.KindUint {
				return MakeBool(uint64(left.Int) > uint64(right.Int), types.NoTypeID), nil
			}
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
			kind, _, ok := vm.numericKind(left.TypeID)
			if !ok {
				return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
			}
			if kind == types.KindUint {
				return MakeBool(uint64(left.Int) >= uint64(right.Int), types.NoTypeID), nil
			}
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
			kind, width, ok := vm.numericKind(operand.TypeID)
			if !ok {
				return Value{}, vm.eb.typeMismatch("signed int", operand.Kind.String())
			}
			if kind == types.KindUint {
				return Value{}, vm.eb.typeMismatch("signed int", "uint")
			}
			minVal := int64(math.MinInt64)
			if width != types.WidthAny {
				if min, _, ok := intRangeForWidth(width); ok {
					minVal = min
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

func shiftCountFromBigInt(i bignum.BigInt) (int, bool) {
	if i.Neg && !i.IsZero() {
		return 0, false
	}
	return shiftCountFromBigUint(i.Abs())
}
