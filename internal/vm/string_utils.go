package vm

import (
	"math"
	"unicode/utf8"

	"fortio.org/safecast"
)

func (vm *VM) stringCPLen(obj *Object) int {
	if obj == nil {
		return 0
	}
	if obj.StrCPLenKnown {
		return obj.StrCPLen
	}
	obj.StrCPLen = utf8.RuneCountInString(obj.Str)
	obj.StrCPLenKnown = true
	return obj.StrCPLen
}

func codePointAt(s string, idx int) (rune, bool) {
	if idx < 0 {
		return 0, false
	}
	count := 0
	for _, r := range s {
		if count == idx {
			return r, true
		}
		count++
	}
	return 0, false
}

func byteOffsetsForCodePoints(s string, start, end int) (startByte, endByte int) {
	if start <= 0 && end <= 0 {
		return 0, 0
	}
	startByte = 0
	endByte = len(s)
	count := 0
	for i := range s {
		if count == start {
			startByte = i
			if start == end {
				return startByte, i
			}
		}
		if count == end {
			return startByte, i
		}
		count++
	}
	if start >= count {
		startByte = len(s)
	}
	return startByte, endByte
}

func (vm *VM) rangeBounds(r *RangeObject, length int) (start, end int, err *VMError) {
	if r == nil {
		return 0, 0, vm.eb.typeMismatch("range", "nil")
	}
	if length < 0 {
		length = 0
	}
	length64 := int64(length)
	start64 := int64(0)
	if r.HasStart {
		val, rangeErr := vm.rangeIndexFromValue(r.Start, length)
		if rangeErr != nil {
			return 0, 0, rangeErr
		}
		start64 = val
	}
	end64 := length64
	if r.HasEnd {
		val, rangeErr := vm.rangeIndexFromValue(r.End, length)
		if rangeErr != nil {
			return 0, 0, rangeErr
		}
		end64 = val
	}
	if r.Inclusive && r.HasEnd && end64 < math.MaxInt64 {
		end64++
	}
	if start64 < 0 {
		start64 = 0
	} else if start64 > length64 {
		start64 = length64
	}
	if end64 < 0 {
		end64 = 0
	} else if end64 > length64 {
		end64 = length64
	}
	startInt, convErr := safecast.Conv[int](start64)
	if convErr != nil {
		startInt = length
	}
	endInt, convErr := safecast.Conv[int](end64)
	if convErr != nil {
		endInt = length
	}
	return startInt, endInt, nil
}

func (vm *VM) rangeIndexFromValue(v Value, length int) (int64, *VMError) {
	length64 := int64(length)
	switch v.Kind {
	case VKInt:
		return normalizeRangeIndex(v.Int, length64), nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(v)
		if vmErr != nil {
			return 0, vmErr
		}
		if n, ok := i.Int64(); ok {
			return normalizeRangeIndex(n, length64), nil
		}
		if i.Neg && !i.IsZero() {
			return -1, nil
		}
		if length64 == math.MaxInt64 {
			return length64, nil
		}
		return length64 + 1, nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(v)
		if vmErr != nil {
			return 0, vmErr
		}
		if n, ok := u.Uint64(); ok {
			if n > math.MaxInt64 {
				if length64 == math.MaxInt64 {
					return length64, nil
				}
				return length64 + 1, nil
			}
			return normalizeRangeIndex(int64(n), length64), nil
		}
		if length64 == math.MaxInt64 {
			return length64, nil
		}
		return length64 + 1, nil
	default:
		return 0, vm.eb.typeMismatch("int", v.Kind.String())
	}
}

func normalizeRangeIndex(n, length int64) int64 {
	if n < 0 {
		if n < -length {
			return -1
		}
		return n + length
	}
	return n
}

func (vm *VM) uintValueToInt(v Value, context string) (int, *VMError) {
	maxInt := int64(int(^uint(0) >> 1))
	maxUint := uint64(^uint(0) >> 1)
	switch v.Kind {
	case VKInt:
		if v.Int < 0 || v.Int > maxInt {
			return 0, vm.eb.invalidNumericConversion(context)
		}
		ni, err := safecast.Conv[int](v.Int)
		if err != nil {
			return 0, vm.eb.invalidNumericConversion(context)
		}
		return ni, nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(v)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok || n > maxUint {
			return 0, vm.eb.invalidNumericConversion(context)
		}
		ni, err := safecast.Conv[int](n)
		if err != nil {
			return 0, vm.eb.invalidNumericConversion(context)
		}
		return ni, nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(v)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok || n < 0 || n > maxInt {
			return 0, vm.eb.invalidNumericConversion(context)
		}
		ni, err := safecast.Conv[int](n)
		if err != nil {
			return 0, vm.eb.invalidNumericConversion(context)
		}
		return ni, nil
	default:
		return 0, vm.eb.typeMismatch("uint", v.Kind.String())
	}
}

func (vm *VM) nonNegativeIndexValue(idx Value) (int, *VMError) {
	maxIndex := int(^uint(0) >> 1)
	maxInt := int64(maxIndex)
	maxUint := uint64(^uint(0) >> 1)
	switch idx.Kind {
	case VKInt:
		if idx.Int < 0 || idx.Int > maxInt {
			return 0, vm.eb.outOfBounds(maxIndex, 0)
		}
		n, err := safecast.Conv[int](idx.Int)
		if err != nil {
			return 0, vm.eb.outOfBounds(maxIndex, 0)
		}
		return n, nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(idx)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok || n < 0 || n > maxInt {
			return 0, vm.eb.outOfBounds(maxIndex, 0)
		}
		ni, err := safecast.Conv[int](n)
		if err != nil {
			return 0, vm.eb.outOfBounds(maxIndex, 0)
		}
		return ni, nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(idx)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok || n > maxUint {
			return 0, vm.eb.outOfBounds(maxIndex, 0)
		}
		ni, err := safecast.Conv[int](n)
		if err != nil {
			return 0, vm.eb.outOfBounds(maxIndex, 0)
		}
		return ni, nil
	default:
		return 0, vm.eb.typeMismatch("int", idx.Kind.String())
	}
}
