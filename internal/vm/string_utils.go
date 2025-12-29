package vm

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"fortio.org/safecast"
)

const stringRopeThresholdBytes = 128

func (vm *VM) stringCPLen(obj *Object) int {
	if obj == nil {
		return 0
	}
	if obj.StrKind == StringSlice {
		obj.StrCPLen = obj.StrSliceLen
		obj.StrCPLenKnown = true
		return obj.StrSliceLen
	}
	if obj.StrCPLenKnown {
		return obj.StrCPLen
	}
	switch obj.StrKind {
	case StringFlat:
		obj.StrCPLen = utf8.RuneCountInString(obj.Str)
		obj.StrCPLenKnown = true
		return obj.StrCPLen
	case StringConcat:
		left := vm.Heap.Get(obj.StrLeft)
		right := vm.Heap.Get(obj.StrRight)
		leftLen := vm.stringCPLen(left)
		rightLen := vm.stringCPLen(right)
		obj.StrCPLen = leftLen + rightLen
		obj.StrCPLenKnown = true
		return obj.StrCPLen
	default:
		obj.StrCPLen = utf8.RuneCountInString(obj.Str)
		obj.StrCPLenKnown = true
		return obj.StrCPLen
	}
}

func (vm *VM) concatStringValues(left, right Value) (Value, *VMError) {
	if left.Kind != VKHandleString || right.Kind != VKHandleString {
		return Value{}, vm.eb.typeMismatch("string", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
	}
	leftObj := vm.Heap.Get(left.H)
	rightObj := vm.Heap.Get(right.H)
	if leftObj == nil || rightObj == nil {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, "invalid string handle")
	}

	leftBytes := vm.stringByteLen(leftObj)
	rightBytes := vm.stringByteLen(rightObj)
	totalBytes := leftBytes + rightBytes

	leftCP := vm.stringCPLen(leftObj)
	rightCP := vm.stringCPLen(rightObj)
	totalCP := leftCP + rightCP

	typeID := left.TypeID
	if totalBytes <= stringRopeThresholdBytes {
		leftStr := vm.stringBytes(leftObj)
		rightStr := vm.stringBytes(rightObj)
		joined := leftStr + rightStr
		h := vm.Heap.AllocStringWithCPLen(typeID, joined, totalCP)
		return MakeHandleString(h, typeID), nil
	}

	h := vm.Heap.AllocStringConcat(typeID, left.H, right.H, totalBytes, totalCP, true)
	return MakeHandleString(h, typeID), nil
}

func (vm *VM) repeatStringValue(val Value, count int) (Value, *VMError) {
	if val.Kind != VKHandleString {
		return Value{}, vm.eb.typeMismatch("string", val.Kind.String())
	}
	obj := vm.Heap.Get(val.H)
	if obj == nil {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, "invalid string handle")
	}
	if count <= 0 {
		h := vm.Heap.AllocStringWithCPLen(val.TypeID, "", 0)
		return MakeHandleString(h, val.TypeID), nil
	}

	unitBytes := vm.stringByteLen(obj)
	unitCP := vm.stringCPLen(obj)
	if unitBytes == 0 || unitCP == 0 {
		h := vm.Heap.AllocStringWithCPLen(val.TypeID, "", 0)
		return MakeHandleString(h, val.TypeID), nil
	}

	maxInt := int(^uint(0) >> 1)
	if unitBytes > 0 && count > maxInt/unitBytes {
		return Value{}, vm.eb.invalidNumericConversion("string repeat length out of range")
	}
	if unitCP > 0 && count > maxInt/unitCP {
		return Value{}, vm.eb.invalidNumericConversion("string repeat length out of range")
	}

	base := vm.stringBytes(obj)
	totalBytes := unitBytes * count
	totalCP := unitCP * count

	var b strings.Builder
	b.Grow(totalBytes)
	for range count {
		b.WriteString(base)
	}
	h := vm.Heap.AllocStringWithCPLen(val.TypeID, b.String(), totalCP)
	return MakeHandleString(h, val.TypeID), nil
}

func (vm *VM) stringByteLen(obj *Object) int {
	if obj == nil {
		return 0
	}
	if obj.StrByteLen > 0 || (obj.StrKind == StringFlat && obj.StrFlatKnown) {
		if obj.StrByteLen == 0 {
			obj.StrByteLen = len(obj.Str)
		}
		return obj.StrByteLen
	}
	switch obj.StrKind {
	case StringConcat:
		left := vm.Heap.Get(obj.StrLeft)
		right := vm.Heap.Get(obj.StrRight)
		obj.StrByteLen = vm.stringByteLen(left) + vm.stringByteLen(right)
	case StringSlice:
		base := vm.Heap.Get(obj.StrSliceBase)
		start := obj.StrSliceStart
		end := start + obj.StrSliceLen
		obj.StrByteLen = vm.byteLenForRange(base, start, end)
	default:
		obj.StrByteLen = len(obj.Str)
	}
	return obj.StrByteLen
}

func (vm *VM) stringBytes(obj *Object) string {
	if obj == nil {
		return ""
	}
	if obj.StrFlatKnown {
		return obj.Str
	}
	switch obj.StrKind {
	case StringFlat:
		obj.StrFlatKnown = true
		obj.StrByteLen = len(obj.Str)
		return obj.Str
	default:
	}
	cpLen := vm.stringCPLen(obj)
	if cpLen <= 0 {
		obj.Str = ""
		obj.StrFlatKnown = true
		obj.StrByteLen = 0
		return obj.Str
	}
	var b strings.Builder
	b.Grow(vm.stringByteLen(obj))
	vm.appendStringBytesRange(&b, obj, 0, cpLen)
	obj.Str = b.String()
	obj.StrFlatKnown = true
	obj.StrByteLen = len(obj.Str)
	return obj.Str
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

func (vm *VM) codePointAtObj(obj *Object, idx int) (rune, bool) {
	if obj == nil || idx < 0 {
		return 0, false
	}
	switch obj.StrKind {
	case StringFlat:
		return codePointAt(obj.Str, idx)
	case StringConcat:
		left := vm.Heap.Get(obj.StrLeft)
		leftLen := vm.stringCPLen(left)
		if idx < leftLen {
			return vm.codePointAtObj(left, idx)
		}
		right := vm.Heap.Get(obj.StrRight)
		return vm.codePointAtObj(right, idx-leftLen)
	case StringSlice:
		base := vm.Heap.Get(obj.StrSliceBase)
		return vm.codePointAtObj(base, obj.StrSliceStart+idx)
	default:
		return codePointAt(vm.stringBytes(obj), idx)
	}
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

func (vm *VM) appendStringBytesRange(b *strings.Builder, obj *Object, start, end int) {
	if b == nil || obj == nil || start >= end {
		return
	}
	switch obj.StrKind {
	case StringFlat:
		cpLen := vm.stringCPLen(obj)
		if start <= 0 && end >= cpLen {
			b.WriteString(obj.Str)
			return
		}
		byteStart, byteEnd := byteOffsetsForCodePoints(obj.Str, start, end)
		if byteStart < 0 {
			byteStart = 0
		}
		if byteEnd > len(obj.Str) {
			byteEnd = len(obj.Str)
		}
		if byteStart < byteEnd {
			b.WriteString(obj.Str[byteStart:byteEnd])
		}
	case StringConcat:
		left := vm.Heap.Get(obj.StrLeft)
		right := vm.Heap.Get(obj.StrRight)
		leftLen := vm.stringCPLen(left)
		if start < leftLen {
			leftEnd := minInt(end, leftLen)
			vm.appendStringBytesRange(b, left, start, leftEnd)
		}
		if end > leftLen {
			rightStart := maxInt(0, start-leftLen)
			rightEnd := end - leftLen
			vm.appendStringBytesRange(b, right, rightStart, rightEnd)
		}
	case StringSlice:
		base := vm.Heap.Get(obj.StrSliceBase)
		sliceStart := obj.StrSliceStart
		vm.appendStringBytesRange(b, base, sliceStart+start, sliceStart+end)
	default:
		s := vm.stringBytes(obj)
		byteStart, byteEnd := byteOffsetsForCodePoints(s, start, end)
		if byteStart < 0 {
			byteStart = 0
		}
		if byteEnd > len(s) {
			byteEnd = len(s)
		}
		if byteStart < byteEnd {
			b.WriteString(s[byteStart:byteEnd])
		}
	}
}

func (vm *VM) byteLenForRange(obj *Object, start, end int) int {
	if obj == nil || start >= end {
		return 0
	}
	switch obj.StrKind {
	case StringFlat:
		cpLen := vm.stringCPLen(obj)
		if start <= 0 && end >= cpLen {
			return len(obj.Str)
		}
		byteStart, byteEnd := byteOffsetsForCodePoints(obj.Str, start, end)
		if byteEnd < byteStart {
			return 0
		}
		return byteEnd - byteStart
	case StringConcat:
		left := vm.Heap.Get(obj.StrLeft)
		right := vm.Heap.Get(obj.StrRight)
		leftLen := vm.stringCPLen(left)
		total := 0
		if start < leftLen {
			leftEnd := minInt(end, leftLen)
			total += vm.byteLenForRange(left, start, leftEnd)
		}
		if end > leftLen {
			rightStart := maxInt(0, start-leftLen)
			rightEnd := end - leftLen
			total += vm.byteLenForRange(right, rightStart, rightEnd)
		}
		return total
	case StringSlice:
		base := vm.Heap.Get(obj.StrSliceBase)
		sliceStart := obj.StrSliceStart
		return vm.byteLenForRange(base, sliceStart+start, sliceStart+end)
	default:
		s := vm.stringBytes(obj)
		byteStart, byteEnd := byteOffsetsForCodePoints(s, start, end)
		if byteEnd < byteStart {
			return 0
		}
		return byteEnd - byteStart
	}
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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
