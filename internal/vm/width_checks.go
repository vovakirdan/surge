package vm

import (
	"math"

	"surge/internal/types"
	"surge/internal/vm/bignum"
)

func checkSignedWidth(value int64, width types.Width) bool {
	if width == types.WidthAny {
		return true
	}
	minVal, maxVal, ok := intRangeForWidth(width)
	if !ok {
		return true
	}
	return value >= minVal && value <= maxVal
}

func checkUnsignedWidth(value uint64, width types.Width) bool {
	if width == types.WidthAny {
		return true
	}
	maxVal, ok := uintMaxForWidth(width)
	if !ok {
		return true
	}
	return value <= maxVal
}

func widthBits(width types.Width) (int, bool) {
	switch width {
	case types.Width8:
		return 8, true
	case types.Width16:
		return 16, true
	case types.Width32:
		return 32, true
	case types.Width64:
		return 64, true
	case types.WidthAny:
		return 64, true
	default:
		return 0, false
	}
}

func maskForWidth(width types.Width) uint64 {
	bits, ok := widthBits(width)
	if !ok || bits >= 64 {
		return math.MaxUint64
	}
	return (uint64(1) << bits) - 1
}

func signExtendUnsigned(value uint64, width types.Width) int64 {
	bits, ok := widthBits(width)
	if !ok || bits >= 64 {
		return asInt64(value)
	}
	mask := (uint64(1) << bits) - 1
	value &= mask
	signBit := uint64(1) << (bits - 1)
	if value&signBit == 0 {
		return asInt64(value)
	}
	return asInt64(value | ^mask)
}

func (vm *VM) checkFloatWidth(value bignum.BigFloat, typeID types.TypeID) *VMError {
	kind, width, ok := vm.numericKind(typeID)
	if !ok || kind != types.KindFloat {
		return nil
	}
	if !floatFitsWidth(value, width) {
		return vm.eb.invalidNumericConversion("float overflow")
	}
	return nil
}
