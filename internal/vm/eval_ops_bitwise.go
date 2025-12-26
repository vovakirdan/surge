package vm

import (
	"fmt"
	"math"

	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// evalBitAnd evaluates the bitwise AND operation.
func (vm *VM) evalBitAnd(left, right Value) (Value, *VMError) {
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
		res := (asUint64(left.Int) & asUint64(right.Int)) & mask
		if kind == types.KindUint {
			return MakeInt(asInt64(res), left.TypeID), nil
		}
		return MakeInt(signExtendUnsigned(res, width), left.TypeID), nil
	default:
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
}

// evalBitOr evaluates the bitwise OR operation.
func (vm *VM) evalBitOr(left, right Value) (Value, *VMError) {
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
		res := (asUint64(left.Int) | asUint64(right.Int)) & mask
		if kind == types.KindUint {
			return MakeInt(asInt64(res), left.TypeID), nil
		}
		return MakeInt(signExtendUnsigned(res, width), left.TypeID), nil
	default:
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
}

// evalBitXor evaluates the bitwise XOR operation.
func (vm *VM) evalBitXor(left, right Value) (Value, *VMError) {
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
		res := (asUint64(left.Int) ^ asUint64(right.Int)) & mask
		if kind == types.KindUint {
			return MakeInt(asInt64(res), left.TypeID), nil
		}
		return MakeInt(signExtendUnsigned(res, width), left.TypeID), nil
	default:
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
}

// evalShiftLeft evaluates the left shift operation.
func (vm *VM) evalShiftLeft(left, right Value) (Value, *VMError) {
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
		return vm.evalIntShiftLeft(left, right)
	default:
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
}

func (vm *VM) evalIntShiftLeft(left, right Value) (Value, *VMError) {
	kind, width, ok := vm.numericKind(left.TypeID)
	if !ok {
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
	bitWidth, _ := widthBits(width)
	if right.Int < 0 || right.Int >= int64(bitWidth) {
		return Value{}, vm.eb.intOverflow()
	}
	shift := uint(right.Int)
	if kind == types.KindUint {
		val := asUint64(left.Int)
		if maxVal, ok := uintMaxForWidth(width); ok {
			if shift > 0 && val > maxVal>>shift {
				return Value{}, vm.eb.intOverflow()
			}
		} else if shift > 0 && val > math.MaxUint64>>shift {
			return Value{}, vm.eb.intOverflow()
		}
		return MakeInt(asInt64(val<<shift), left.TypeID), nil
	}
	minVal := int64(math.MinInt64)
	maxVal := int64(math.MaxInt64)
	if width != types.WidthAny {
		if minValRange, maxValRange, ok := intRangeForWidth(width); ok {
			minVal = minValRange
			maxVal = maxValRange
		}
	}
	if shift > 0 {
		if left.Int > maxVal>>shift || left.Int < minVal>>shift {
			return Value{}, vm.eb.intOverflow()
		}
	}
	res := left.Int << shift
	if !checkSignedWidth(res, width) {
		return Value{}, vm.eb.intOverflow()
	}
	return MakeInt(res, left.TypeID), nil
}

// evalShiftRight evaluates the right shift operation.
func (vm *VM) evalShiftRight(left, right Value) (Value, *VMError) {
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
		return vm.evalIntShiftRight(left, right)
	default:
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
}

func (vm *VM) evalIntShiftRight(left, right Value) (Value, *VMError) {
	kind, width, ok := vm.numericKind(left.TypeID)
	if !ok {
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
	bitWidth, _ := widthBits(width)
	if right.Int < 0 || right.Int >= int64(bitWidth) {
		return Value{}, vm.eb.intOverflow()
	}
	shift := uint(right.Int)
	mask := maskForWidth(width)
	if kind == types.KindUint {
		val := asUint64(left.Int) & mask
		res := val >> shift
		return MakeInt(asInt64(res), left.TypeID), nil
	}
	val := signExtendUnsigned(asUint64(left.Int)&mask, width)
	return MakeInt(val>>shift, left.TypeID), nil
}
