package vm

import (
	"fmt"
	"math"
	"math/bits"

	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// evalAdd evaluates the addition operation.
func (vm *VM) evalAdd(left, right Value) (Value, *VMError) {
	switch {
	case left.Kind == VKHandleString && right.Kind == VKHandleString:
		return vm.concatStringValues(left, right)
	case left.Kind == VKHandleArray && right.Kind == VKHandleArray:
		return vm.evalArrayConcat(left, right)
	case left.Kind == VKBigInt && right.Kind == VKBigInt:
		return vm.evalBigIntAdd(left, right)
	case left.Kind == VKBigUint && right.Kind == VKBigUint:
		return vm.evalBigUintAdd(left, right)
	case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
		return vm.evalBigFloatAdd(left, right)
	case left.Kind == VKInt && right.Kind == VKInt:
		return vm.evalIntAdd(left, right)
	default:
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
}

func (vm *VM) evalArrayConcat(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalBigIntAdd(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalBigUintAdd(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalBigFloatAdd(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalIntAdd(left, right Value) (Value, *VMError) {
	kind, width, ok := vm.numericKind(left.TypeID)
	if !ok {
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
	if kind == types.KindUint {
		sum, carry := bits.Add64(asUint64(left.Int), asUint64(right.Int), 0)
		if carry != 0 || !checkUnsignedWidth(sum, width) {
			return Value{}, vm.eb.intOverflow()
		}
		return MakeInt(asInt64(sum), left.TypeID), nil
	}
	res, ok := AddInt64Checked(left.Int, right.Int)
	if !ok || !checkSignedWidth(res, width) {
		return Value{}, vm.eb.intOverflow()
	}
	return MakeInt(res, left.TypeID), nil
}

// evalSub evaluates the subtraction operation.
func (vm *VM) evalSub(left, right Value) (Value, *VMError) {
	switch {
	case left.Kind == VKBigInt && right.Kind == VKBigInt:
		return vm.evalBigIntSub(left, right)
	case left.Kind == VKBigUint && right.Kind == VKBigUint:
		return vm.evalBigUintSub(left, right)
	case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
		return vm.evalBigFloatSub(left, right)
	case left.Kind == VKInt && right.Kind == VKInt:
		return vm.evalIntSub(left, right)
	default:
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
}

func (vm *VM) evalBigIntSub(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalBigUintSub(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalBigFloatSub(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalIntSub(left, right Value) (Value, *VMError) {
	kind, width, ok := vm.numericKind(left.TypeID)
	if !ok {
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
	if kind == types.KindUint {
		ua := asUint64(left.Int)
		ub := asUint64(right.Int)
		if ua < ub {
			return Value{}, vm.eb.intOverflow()
		}
		res := ua - ub
		if !checkUnsignedWidth(res, width) {
			return Value{}, vm.eb.intOverflow()
		}
		return MakeInt(asInt64(res), left.TypeID), nil
	}
	res, ok := SubInt64Checked(left.Int, right.Int)
	if !ok || !checkSignedWidth(res, width) {
		return Value{}, vm.eb.intOverflow()
	}
	return MakeInt(res, left.TypeID), nil
}

// evalMul evaluates the multiplication operation.
func (vm *VM) evalMul(left, right Value) (Value, *VMError) {
	switch {
	case left.Kind == VKHandleString:
		count, vmErr := vm.uintValueToInt(right, "string repeat count out of range")
		if vmErr != nil {
			return Value{}, vmErr
		}
		return vm.repeatStringValue(left, count)
	case left.Kind == VKBigInt && right.Kind == VKBigInt:
		return vm.evalBigIntMul(left, right)
	case left.Kind == VKBigUint && right.Kind == VKBigUint:
		return vm.evalBigUintMul(left, right)
	case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
		return vm.evalBigFloatMul(left, right)
	case left.Kind == VKInt && right.Kind == VKInt:
		return vm.evalIntMul(left, right)
	default:
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
}

func (vm *VM) evalBigIntMul(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalBigUintMul(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalBigFloatMul(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalIntMul(left, right Value) (Value, *VMError) {
	kind, width, ok := vm.numericKind(left.TypeID)
	if !ok {
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
	if kind == types.KindUint {
		hi, lo := bits.Mul64(asUint64(left.Int), asUint64(right.Int))
		if hi != 0 || !checkUnsignedWidth(lo, width) {
			return Value{}, vm.eb.intOverflow()
		}
		return MakeInt(asInt64(lo), left.TypeID), nil
	}
	res, ok := MulInt64Checked(left.Int, right.Int)
	if !ok || !checkSignedWidth(res, width) {
		return Value{}, vm.eb.intOverflow()
	}
	return MakeInt(res, left.TypeID), nil
}

// evalDiv evaluates the division operation.
func (vm *VM) evalDiv(left, right Value) (Value, *VMError) {
	switch {
	case left.Kind == VKBigInt && right.Kind == VKBigInt:
		return vm.evalBigIntDiv(left, right)
	case left.Kind == VKBigUint && right.Kind == VKBigUint:
		return vm.evalBigUintDiv(left, right)
	case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
		return vm.evalBigFloatDiv(left, right)
	case left.Kind == VKInt && right.Kind == VKInt:
		return vm.evalIntDiv(left, right)
	default:
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
}

func (vm *VM) evalBigIntDiv(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalBigUintDiv(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalBigFloatDiv(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalIntDiv(left, right Value) (Value, *VMError) {
	kind, width, ok := vm.numericKind(left.TypeID)
	if !ok {
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
	if kind == types.KindUint {
		ua := asUint64(left.Int)
		ub := asUint64(right.Int)
		if ub == 0 {
			return Value{}, vm.eb.divisionByZero()
		}
		res := ua / ub
		if !checkUnsignedWidth(res, width) {
			return Value{}, vm.eb.intOverflow()
		}
		return MakeInt(asInt64(res), left.TypeID), nil
	}
	if right.Int == 0 {
		return Value{}, vm.eb.divisionByZero()
	}
	minVal := int64(math.MinInt64)
	if width != types.WidthAny {
		if minValRange, _, ok := intRangeForWidth(width); ok {
			minVal = minValRange
		}
	}
	if right.Int == -1 && left.Int == minVal {
		return Value{}, vm.eb.intOverflow()
	}
	return MakeInt(left.Int/right.Int, left.TypeID), nil
}

// evalMod evaluates the modulo operation.
func (vm *VM) evalMod(left, right Value) (Value, *VMError) {
	switch {
	case left.Kind == VKBigInt && right.Kind == VKBigInt:
		return vm.evalBigIntMod(left, right)
	case left.Kind == VKBigUint && right.Kind == VKBigUint:
		return vm.evalBigUintMod(left, right)
	case left.Kind == VKBigFloat && right.Kind == VKBigFloat:
		return vm.evalBigFloatMod(left, right)
	case left.Kind == VKInt && right.Kind == VKInt:
		return vm.evalIntMod(left, right)
	default:
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
}

func (vm *VM) evalBigIntMod(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalBigUintMod(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalBigFloatMod(left, right Value) (Value, *VMError) {
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
}

func (vm *VM) evalIntMod(left, right Value) (Value, *VMError) {
	kind, width, ok := vm.numericKind(left.TypeID)
	if !ok {
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
	if kind == types.KindUint {
		ua := asUint64(left.Int)
		ub := asUint64(right.Int)
		if ub == 0 {
			return Value{}, vm.eb.divisionByZero()
		}
		res := ua % ub
		if !checkUnsignedWidth(res, width) {
			return Value{}, vm.eb.intOverflow()
		}
		return MakeInt(asInt64(res), left.TypeID), nil
	}
	if right.Int == 0 {
		return Value{}, vm.eb.divisionByZero()
	}
	return MakeInt(left.Int%right.Int, left.TypeID), nil
}
