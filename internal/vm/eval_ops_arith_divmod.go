package vm

import (
	"fmt"
	"math"

	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// Division and modulo, split from eval_ops_arith.go: the same shape as the
// other four operations (a dispatch on the operand kinds, one arm per
// representation, the mixed-width arms before the mismatch), kept in their
// own file only so that neither half crosses the size gate.

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
	case vm.mixedBigUint(left, right):
		return vm.evalBigUintDiv(left, right)
	case vm.mixedBigInt(left, right):
		return vm.evalBigIntDiv(left, right)
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
	case vm.mixedBigUint(left, right):
		return vm.evalBigUintMod(left, right)
	case vm.mixedBigInt(left, right):
		return vm.evalBigIntMod(left, right)
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
