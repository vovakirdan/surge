package vm

import (
	"errors"
	"fmt"
	"strings"

	"surge/internal/types"
	"surge/internal/vm/bignum"
)

func (vm *VM) numericKind(id types.TypeID) (types.Kind, types.Width, bool) {
	if vm == nil || vm.Types == nil || id == types.NoTypeID {
		return 0, 0, false
	}
	id = vm.valueType(id)
	tt, ok := vm.Types.Lookup(id)
	if !ok {
		return 0, 0, false
	}
	switch tt.Kind {
	case types.KindInt, types.KindUint, types.KindFloat:
		return tt.Kind, tt.Width, true
	default:
		return 0, 0, false
	}
}

func (vm *VM) makeBigInt(typeID types.TypeID, v bignum.BigInt) Value {
	h := vm.Heap.AllocBigInt(typeID, v)
	return MakeBigInt(h, typeID)
}

func (vm *VM) makeBigUint(typeID types.TypeID, v bignum.BigUint) Value {
	h := vm.Heap.AllocBigUint(typeID, v)
	return MakeBigUint(h, typeID)
}

func (vm *VM) makeBigFloat(typeID types.TypeID, v bignum.BigFloat) Value {
	h := vm.Heap.AllocBigFloat(typeID, v)
	return MakeBigFloat(h, typeID)
}

func (vm *VM) mustBigInt(v Value) (bignum.BigInt, *VMError) {
	if v.Kind != VKBigInt {
		return bignum.BigInt{}, vm.eb.typeMismatch("bigint", v.Kind.String())
	}
	obj := vm.Heap.Get(v.H)
	if obj.Kind != OKBigInt {
		return bignum.BigInt{}, vm.eb.numericOpTypeMismatch(fmt.Sprintf("expected bigint object, got %v", obj.Kind))
	}
	return obj.BigInt, nil
}

func (vm *VM) mustBigUint(v Value) (bignum.BigUint, *VMError) {
	if v.Kind != VKBigUint {
		return bignum.BigUint{}, vm.eb.typeMismatch("biguint", v.Kind.String())
	}
	obj := vm.Heap.Get(v.H)
	if obj.Kind != OKBigUint {
		return bignum.BigUint{}, vm.eb.numericOpTypeMismatch(fmt.Sprintf("expected biguint object, got %v", obj.Kind))
	}
	return obj.BigUint, nil
}

func (vm *VM) mustBigFloat(v Value) (bignum.BigFloat, *VMError) {
	if v.Kind != VKBigFloat {
		return bignum.BigFloat{}, vm.eb.typeMismatch("bigfloat", v.Kind.String())
	}
	obj := vm.Heap.Get(v.H)
	if obj.Kind != OKBigFloat {
		return bignum.BigFloat{}, vm.eb.numericOpTypeMismatch(fmt.Sprintf("expected bigfloat object, got %v", obj.Kind))
	}
	return obj.BigFloat, nil
}

func (vm *VM) bignumErr(err error) *VMError {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, bignum.ErrMaxLimbs):
		return vm.eb.numericSizeLimitExceeded()
	case errors.Is(err, bignum.ErrDivByZero):
		return vm.eb.divisionByZero()
	case errors.Is(err, bignum.ErrUnderflow):
		return vm.eb.invalidNumericConversion("unsigned underflow")
	default:
		msg := err.Error()
		if strings.Contains(msg, "negative") {
			return vm.eb.invalidNumericConversion(msg)
		}
		return vm.eb.invalidNumericConversion(msg)
	}
}
