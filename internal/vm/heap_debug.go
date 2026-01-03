package vm

import (
	"fmt"

	"surge/internal/types"
	"surge/internal/vm/bignum"
)

type heapCounters struct {
	allocCount  uint64
	freeCount   uint64
	rcIncrCount uint64
	rcDecrCount uint64
}

type heapStatsSnapshot struct {
	allocCount  uint64
	freeCount   uint64
	liveBlocks  uint64
	liveBytes   uint64
	rcIncrCount uint64
	rcDecrCount uint64
}

func safeUint64FromInt(n int) uint64 {
	if n <= 0 {
		return 0
	}
	// #nosec G115 -- negative sizes are clamped above; sizes are non-negative by construction.
	return uint64(n)
}

func (vm *VM) heapStatsSnapshot() heapStatsSnapshot {
	if vm == nil {
		return heapStatsSnapshot{}
	}
	snap := heapStatsSnapshot{
		allocCount:  vm.heapCounters.allocCount,
		freeCount:   vm.heapCounters.freeCount,
		rcIncrCount: vm.heapCounters.rcIncrCount,
		rcDecrCount: vm.heapCounters.rcDecrCount,
	}

	if vm.Heap != nil {
		for h := Handle(1); h < vm.Heap.next; h++ {
			obj, ok := vm.Heap.lookup(h)
			if !ok || obj == nil || obj.Freed || obj.RefCount == 0 {
				continue
			}
			snap.liveBlocks++
			snap.liveBytes += vm.heapObjectBytes(obj)
		}
	}

	if vm.rawMem != nil {
		for _, alloc := range vm.rawMem.allocs {
			if alloc == nil || alloc.freed {
				continue
			}
			snap.liveBlocks++
			snap.liveBytes += uint64(len(alloc.data))
		}
	}

	return snap
}

func (vm *VM) heapObjectBytes(obj *Object) uint64 {
	if obj == nil {
		return 0
	}
	switch obj.Kind {
	case OKString:
		if vm != nil {
			return safeUint64FromInt(vm.stringByteLen(obj))
		}
		return safeUint64FromInt(len(obj.Str))
	case OKArray:
		elemSize := vm.arrayElemSize(obj)
		if elemSize == 0 {
			return 0
		}
		return uint64(len(obj.Arr)) * elemSize
	case OKArraySlice:
		return 0
	case OKMap:
		if vm != nil && vm.Layout != nil && vm.Types != nil && obj.TypeID != types.NoTypeID {
			keyType, valueType, ok := vm.Types.MapInfo(obj.TypeID)
			if ok && keyType != types.NoTypeID && valueType != types.NoTypeID {
				keySize, errKey := vm.Layout.SizeOf(keyType)
				valSize, errVal := vm.Layout.SizeOf(valueType)
				if errKey == nil && errVal == nil {
					elemSize := safeUint64FromInt(keySize) + safeUint64FromInt(valSize)
					return uint64(len(obj.MapEntries)) * elemSize
				}
			}
		}
		return 0
	case OKStruct, OKTag, OKRange:
		if vm != nil && vm.Layout != nil && obj.TypeID != types.NoTypeID {
			if size, err := vm.Layout.SizeOf(obj.TypeID); err == nil {
				return safeUint64FromInt(size)
			}
		}
		return 0
	case OKBigInt:
		return uint64(len(obj.BigInt.Limbs)) * 4
	case OKBigUint:
		return uint64(len(obj.BigUint.Limbs)) * 4
	case OKBigFloat:
		return uint64(len(obj.BigFloat.Mant.Limbs)) * 4
	default:
		return 0
	}
}

func (vm *VM) arrayElemSize(obj *Object) uint64 {
	if vm == nil || vm.Layout == nil || obj == nil {
		return 0
	}
	elemType := vm.arrayElemType(obj)
	if elemType != types.NoTypeID {
		if size, err := vm.Layout.SizeOf(elemType); err == nil {
			return safeUint64FromInt(size)
		}
	}
	for i := range obj.Arr {
		if obj.Arr[i].TypeID == types.NoTypeID {
			continue
		}
		if size, err := vm.Layout.SizeOf(vm.valueType(obj.Arr[i].TypeID)); err == nil {
			return safeUint64FromInt(size)
		}
	}
	return 0
}

func (vm *VM) arrayElemType(obj *Object) types.TypeID {
	if vm == nil || vm.Types == nil || obj == nil || obj.TypeID == types.NoTypeID {
		return types.NoTypeID
	}
	tt, ok := vm.Types.Lookup(obj.TypeID)
	if !ok {
		return types.NoTypeID
	}
	switch tt.Kind {
	case types.KindArray:
		return tt.Elem
	case types.KindStruct:
		info, ok := vm.Types.StructInfo(obj.TypeID)
		if !ok || info == nil {
			return types.NoTypeID
		}
		name, ok := lookupName(vm.Types.Strings, info.Name)
		if !ok || name != "Array" {
			return types.NoTypeID
		}
		args := vm.Types.StructArgs(obj.TypeID)
		if len(args) == 1 {
			return args[0]
		}
		return types.NoTypeID
	default:
		return types.NoTypeID
	}
}

func (vm *VM) objectRefCount(obj *Object) int {
	if obj == nil {
		return 0
	}
	count := 0
	switch obj.Kind {
	case OKString:
		if obj.StrLeft != 0 {
			count++
		}
		if obj.StrRight != 0 {
			count++
		}
		if obj.StrSliceBase != 0 {
			count++
		}
	case OKArray:
		for _, v := range obj.Arr {
			if v.IsHeap() && v.H != 0 {
				count++
			}
		}
	case OKArraySlice:
		if obj.ArrSliceBase != 0 {
			count++
		}
	case OKMap:
		for _, entry := range obj.MapEntries {
			if entry.Key.IsHeap() && entry.Key.H != 0 {
				count++
			}
			if entry.Value.IsHeap() && entry.Value.H != 0 {
				count++
			}
		}
	case OKStruct:
		for _, v := range obj.Fields {
			if v.IsHeap() && v.H != 0 {
				count++
			}
		}
	case OKTag:
		for _, v := range obj.Tag.Fields {
			if v.IsHeap() && v.H != 0 {
				count++
			}
		}
	case OKRange:
		if obj.Range.Kind == RangeArrayIter {
			if obj.Range.ArrayBase != 0 {
				count++
			}
		} else {
			if obj.Range.HasStart && obj.Range.Start.IsHeap() && obj.Range.Start.H != 0 {
				count++
			}
			if obj.Range.HasEnd && obj.Range.End.IsHeap() && obj.Range.End.H != 0 {
				count++
			}
		}
	default:
	}
	return count
}

func (vm *VM) objectSummary(obj *Object) string {
	if obj == nil {
		return "<invalid>"
	}
	rc := obj.RefCount
	switch obj.Kind {
	case OKString:
		lenBytes := 0
		lenCP := 0
		preview := ""
		repr := stringReprLabel(obj.StrKind)
		if vm != nil {
			lenBytes = vm.stringByteLen(obj)
			lenCP = vm.stringCPLen(obj)
			preview = truncateRunes(vm.stringBytes(obj), 32)
		} else {
			lenBytes = len(obj.Str)
			lenCP = lenBytes
			preview = truncateRunes(obj.Str, 32)
		}
		return fmt.Sprintf("string(rc=%d,len_cp=%d,len_bytes=%d,repr=%s,preview=%q)", rc, lenCP, lenBytes, repr, preview)
	case OKArray:
		return fmt.Sprintf("array(rc=%d,len=%d,cap=%d)", rc, len(obj.Arr), cap(obj.Arr))
	case OKArraySlice:
		return fmt.Sprintf("array_view(rc=%d,len=%d,cap=%d,start=%d)", rc, obj.ArrSliceLen, obj.ArrSliceCap, obj.ArrSliceStart)
	case OKMap:
		return fmt.Sprintf("map(rc=%d,len=%d,type=%s)", rc, len(obj.MapEntries), typeLabel(vm.Types, obj.TypeID))
	case OKStruct:
		return fmt.Sprintf("struct(rc=%d,type=%s)", rc, typeLabel(vm.Types, obj.TypeID))
	case OKTag:
		return fmt.Sprintf("tag(rc=%d,type=%s,tag=%s)", rc, typeLabel(vm.Types, obj.TypeID), vm.tagName(obj))
	case OKRange:
		return fmt.Sprintf("range(rc=%d,kind=%s)", rc, rangeKindLabel(obj.Range.Kind))
	case OKBigInt:
		return fmt.Sprintf("bigint(rc=%d,value=%s)", rc, bignum.FormatInt(obj.BigInt))
	case OKBigUint:
		return fmt.Sprintf("biguint(rc=%d,value=%s)", rc, bignum.FormatUint(obj.BigUint))
	case OKBigFloat:
		s, err := bignum.FormatFloat(obj.BigFloat)
		if err != nil {
			return fmt.Sprintf("bigfloat(rc=%d,<%v>)", rc, err)
		}
		return fmt.Sprintf("bigfloat(rc=%d,value=%s)", rc, s)
	default:
		return fmt.Sprintf("%s(rc=%d)", vm.objectKindLabel(obj.Kind), rc)
	}
}

func stringReprLabel(kind StringKind) string {
	switch kind {
	case StringFlat:
		return "flat"
	case StringConcat:
		return "rope"
	case StringSlice:
		return "slice"
	default:
		return "unknown"
	}
}

func rangeKindLabel(kind RangeKind) string {
	switch kind {
	case RangeDescriptor:
		return "descriptor"
	case RangeArrayIter:
		return "iter"
	default:
		return "unknown"
	}
}

func (vm *VM) tagName(obj *Object) string {
	if obj == nil || obj.Kind != OKTag || vm == nil || vm.tagLayouts == nil {
		return "<unknown>"
	}
	tagName := "<unknown>"
	if layout, ok := vm.tagLayouts.Layout(vm.valueType(obj.TypeID)); ok && layout != nil {
		if tc, ok := layout.CaseBySym(obj.Tag.TagSym); ok && tc.TagName != "" {
			tagName = tc.TagName
		}
	}
	if tagName == "<unknown>" && obj.Tag.TagSym.IsValid() {
		if name, ok := vm.tagLayouts.AnyTagName(obj.Tag.TagSym); ok && name != "" {
			tagName = name
		}
	}
	return tagName
}
