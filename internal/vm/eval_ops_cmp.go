package vm

import (
	"fmt"

	"surge/internal/types"
)

// evalEqual evaluates the equality operation.
func (vm *VM) evalEqual(left, right Value) (Value, *VMError) {
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
}

// evalNotEqual evaluates the inequality operation.
func (vm *VM) evalNotEqual(left, right Value) (Value, *VMError) {
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
}

// evalLess evaluates the less-than operation.
func (vm *VM) evalLess(left, right Value) (Value, *VMError) {
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
			return MakeBool(asUint64(left.Int) < asUint64(right.Int), types.NoTypeID), nil
		}
		return MakeBool(left.Int < right.Int, types.NoTypeID), nil
	default:
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
}

// evalLessEq evaluates the less-than-or-equal operation.
func (vm *VM) evalLessEq(left, right Value) (Value, *VMError) {
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
			return MakeBool(asUint64(left.Int) <= asUint64(right.Int), types.NoTypeID), nil
		}
		return MakeBool(left.Int <= right.Int, types.NoTypeID), nil
	default:
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
}

// evalGreater evaluates the greater-than operation.
func (vm *VM) evalGreater(left, right Value) (Value, *VMError) {
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
			return MakeBool(asUint64(left.Int) > asUint64(right.Int), types.NoTypeID), nil
		}
		return MakeBool(left.Int > right.Int, types.NoTypeID), nil
	default:
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
}

// evalGreaterEq evaluates the greater-than-or-equal operation.
func (vm *VM) evalGreaterEq(left, right Value) (Value, *VMError) {
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
			return MakeBool(asUint64(left.Int) >= asUint64(right.Int), types.NoTypeID), nil
		}
		return MakeBool(left.Int >= right.Int, types.NoTypeID), nil
	default:
		return Value{}, vm.eb.typeMismatch("numeric", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
}
