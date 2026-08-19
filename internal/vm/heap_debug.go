package vm

import (
	"fmt"
	"math"

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

func (vm *VM) heapStatsSnapshot() (heapStatsSnapshot, error) {
	if vm == nil {
		return heapStatsSnapshot{}, nil
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
			objectBytes, err := vm.heapObjectBytes(obj)
			if err != nil {
				return heapStatsSnapshot{}, fmt.Errorf("heap object %d: %w", h, err)
			}
			snap.liveBlocks, err = checkedHeapAdd(snap.liveBlocks, 1, "live block count")
			if err != nil {
				return heapStatsSnapshot{}, err
			}
			snap.liveBytes, err = checkedHeapAdd(snap.liveBytes, objectBytes, "live byte total")
			if err != nil {
				return heapStatsSnapshot{}, err
			}
		}
	}

	if vm.rawMem != nil {
		for _, alloc := range vm.rawMem.allocs {
			if alloc == nil || alloc.freed {
				continue
			}
			var err error
			snap.liveBlocks, err = checkedHeapAdd(snap.liveBlocks, 1, "live block count")
			if err != nil {
				return heapStatsSnapshot{}, err
			}
			snap.liveBytes, err = checkedHeapAdd(snap.liveBytes, safeUint64FromInt(len(alloc.data)), "live byte total")
			if err != nil {
				return heapStatsSnapshot{}, err
			}
		}
	}

	return snap, nil
}

func (vm *VM) heapObjectBytes(obj *Object) (uint64, error) {
	if obj == nil {
		return 0, nil
	}
	switch obj.Kind {
	case OKString:
		if vm != nil {
			return safeUint64FromInt(vm.stringByteLen(obj)), nil
		}
		return safeUint64FromInt(len(obj.Str)), nil
	case OKArray:
		elemSize, err := vm.arrayElemSize(obj)
		if err != nil {
			return 0, err
		}
		return checkedHeapMul(safeUint64FromInt(obj.ArrLen), elemSize, "array length * element size")
	case OKArraySlice:
		return 0, nil
	case OKMap:
		if obj.TypeID == types.NoTypeID {
			return 0, nil
		}
		if vm == nil || vm.Layouts == nil || vm.Types == nil {
			return 0, fmt.Errorf("typed map requires finalized layout registry and type interner")
		}
		keyType, valueType, ok := vm.Types.MapInfo(obj.TypeID)
		if !ok || keyType == types.NoTypeID || valueType == types.NoTypeID {
			return 0, fmt.Errorf("typed map %s has no key/value metadata", types.Label(vm.Types, obj.TypeID))
		}
		keySize, err := vm.Layouts.SizeOf(keyType)
		if err != nil {
			return 0, fmt.Errorf("map key layout: %w", err)
		}
		valueSize, err := vm.Layouts.SizeOf(valueType)
		if err != nil {
			return 0, fmt.Errorf("map value layout: %w", err)
		}
		entrySize, err := checkedHeapAdd(keySize, valueSize, "map key size + value size")
		if err != nil {
			return 0, err
		}
		return checkedHeapMul(safeUint64FromInt(obj.MapLen), entrySize, "map length * entry size")
	case OKResource:
		return vm.typedObjectSize(obj)
	case OKRange:
		if obj.TypeID == types.NoTypeID {
			return 0, nil
		}
		if vm == nil || vm.Layouts == nil {
			return 0, fmt.Errorf("typed %s requires finalized layout registry", vm.objectKindLabel(obj.Kind))
		}
		size, err := vm.Layouts.SizeOf(obj.TypeID)
		if err != nil {
			return 0, fmt.Errorf("typed %s layout: %w", vm.objectKindLabel(obj.Kind), err)
		}
		return size, nil
	case OKBigInt:
		return checkedHeapMul(safeUint64FromInt(len(obj.BigInt.Limbs)), 4, "bigint limb bytes")
	case OKBigUint:
		return checkedHeapMul(safeUint64FromInt(len(obj.BigUint.Limbs)), 4, "biguint limb bytes")
	case OKBigFloat:
		return checkedHeapMul(safeUint64FromInt(len(obj.BigFloat.Mant.Limbs)), 4, "bigfloat limb bytes")
	default:
		return 0, nil
	}
}

func (vm *VM) arrayElemSize(obj *Object) (uint64, error) {
	if obj == nil {
		return 0, nil
	}
	// The DESCRIPTOR answers. It used to be asked of the first element that
	// happened to carry a type, through a resolution that unwraps `&` and `own`
	// alike — so an array of references reported the size of a referent, and an
	// array whose first slots were untyped reported a later slot's answer for
	// all of them. An element cannot answer a question about its container.
	if obj.TypeID == types.NoTypeID && obj.ArrElemType == types.NoTypeID {
		return 0, nil
	}
	// A TYPED array fails closed. Answering zero for one whose layout cannot be
	// resolved would be accounting that reports a number without measuring
	// anything, which is worse than refusing.
	if vm == nil || vm.Layouts == nil || vm.Types == nil {
		return 0, fmt.Errorf("typed array requires finalized layout registry and type interner")
	}
	if obj.ArrElemType == types.NoTypeID {
		return 0, fmt.Errorf("typed array %s has no element metadata", types.Label(vm.Types, obj.TypeID))
	}
	size, err := vm.Layouts.SizeOf(obj.ArrElemType)
	if err != nil {
		return 0, fmt.Errorf("array element layout: %w", err)
	}
	return size, nil
}

func checkedHeapAdd(a, b uint64, operation string) (uint64, error) {
	if a > math.MaxUint64-b {
		return 0, fmt.Errorf("heap accounting overflow: %s", operation)
	}
	return a + b, nil
}

func checkedHeapMul(a, b uint64, operation string) (uint64, error) {
	if a != 0 && b > math.MaxUint64/a {
		return 0, fmt.Errorf("heap accounting overflow: %s", operation)
	}
	return a * b, nil
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
		for i := range obj.ArrLen {
			ref, vmErr := vm.runElemSlot(obj, i)
			if vmErr != nil {
				continue
			}
			v, vmErr := vm.peekStorage(ref)
			if vmErr != nil {
				continue
			}
			if v.IsHeap() && v.H != 0 {
				count++
			}
		}
	case OKArraySlice:
		// An arena-backed slice owns no handle: its elements belong to whoever
		// owns the arena.
		if obj.ArrSliceBase != 0 {
			count++
		}
	case OKMap:
		for i := range obj.MapLen {
			for _, ref := range []func(*Object, int) (StorageRef, *VMError){vm.mapKeySlot, vm.mapValSlot} {
				slot, vmErr := ref(obj, i)
				if vmErr != nil {
					continue
				}
				v, vmErr := vm.peekStorage(slot)
				if vmErr != nil {
					continue
				}
				if v.IsHeap() && v.H != 0 {
					count++
				}
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
		return fmt.Sprintf("array(rc=%d,len=%d,cap=%d)", rc, obj.ArrLen, obj.ArrCap)
	case OKArraySlice:
		return fmt.Sprintf("array_view(rc=%d,len=%d,cap=%d,start=%d)", rc, obj.ArrSliceLen, obj.ArrSliceCap, obj.ArrSliceStart)
	case OKMap:
		return fmt.Sprintf("map(rc=%d,len=%d,type=%s)", rc, obj.MapLen, typeLabel(vm.Types, obj.TypeID))
	case OKResource:
		return fmt.Sprintf("resource(rc=%d,type=%s)", rc, typeLabel(vm.Types, obj.TypeID))
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

// typedObjectSize accounts an object by the layout of its type, which is what a
// resource shares with the aggregates: the object is exactly its type's bytes.
func (vm *VM) typedObjectSize(obj *Object) (uint64, error) {
	if obj.TypeID == types.NoTypeID {
		return 0, nil
	}
	if vm == nil || vm.Layouts == nil {
		return 0, fmt.Errorf("typed %s requires finalized layout registry", vm.objectKindLabel(obj.Kind))
	}
	size, err := vm.Layouts.SizeOf(obj.TypeID)
	if err != nil {
		return 0, fmt.Errorf("typed %s layout: %w", vm.objectKindLabel(obj.Kind), err)
	}
	return size, nil
}
