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

// mixedBigUint reports whether the pair is one heap big uint beside one inline
// word of an unsigned type -- the representation split mustBigUint widens.
// The big side's kind is what decides the arm, because the inline side is a
// VKInt whichever signedness its type has.
func (vm *VM) mixedBigUint(left, right Value) bool {
	small, big := left, right
	if left.Kind == VKBigUint {
		small, big = right, left
	}
	if big.Kind != VKBigUint || small.Kind != VKInt {
		return false
	}
	kind, _, ok := vm.numericKind(small.TypeID)
	return ok && kind == types.KindUint
}

// mixedBigInt is the signed twin of mixedBigUint.
func (vm *VM) mixedBigInt(left, right Value) bool {
	small, big := left, right
	if left.Kind == VKBigInt {
		small, big = right, left
	}
	if big.Kind != VKBigInt || small.Kind != VKInt {
		return false
	}
	kind, _, ok := vm.numericKind(small.TypeID)
	return ok && kind == types.KindInt
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

// A value of an arbitrary-precision type is stored INLINE while it fits a
// machine word and as a heap bignum once it does not, so two operands of one
// declared type can arrive in two representations: `wide_value % U64_MOD`,
// where both are `uint`, is a VKInt beside a VKBigUint the moment U64_MOD is
// 2^64. The declared type is what the language reasons about and sema has
// already made the two sides agree on it; the split is this backend's own
// storage choice, so it is widened here rather than refused as a mismatch --
// which is what it read as on the parity row before, while the native lane
// simply computed the answer.
func (vm *VM) mustBigInt(v Value) (bignum.BigInt, *VMError) {
	if v.Kind == VKInt {
		if kind, _, ok := vm.numericKind(v.TypeID); ok && kind == types.KindInt {
			return bignum.IntFromInt64(v.Int), nil
		}
	}
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
	// Same widening as mustBigInt, for the unsigned representation split.
	if v.Kind == VKInt {
		if kind, _, ok := vm.numericKind(v.TypeID); ok && kind == types.KindUint {
			// An inline uint is its bit pattern kept in the int64 slot, so a
			// value at or above 2^63 reads negative there and the conversion
			// restores it; the reinterpretation is the point, not an overflow.
			return bignum.UintFromUint64(uint64(v.Int)), nil //nolint:gosec // reinterpretation of an unsigned bit pattern
		}
	}
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
