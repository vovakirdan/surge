package vm

import (
	"fmt"

	"surge/internal/symbols"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// Heap stores all owned runtime objects for the VM.
// Handles are monotonically increasing and never reused within a run.
type Heap struct {
	next        Handle
	nextAllocID uint64
	objs        map[Handle]*Object

	vm *VM
}

func (h *Heap) initIfNeeded() {
	if h.objs == nil {
		h.objs = make(map[Handle]*Object, 128)
	}
	if h.next == 0 {
		h.next = 1
	}
	if h.nextAllocID == 0 {
		h.nextAllocID = 1
	}
}

func (h *Heap) alloc(kind ObjectKind, typeID types.TypeID) (Handle, *Object) {
	h.initIfNeeded()
	handle := h.next
	h.next++
	allocID := h.nextAllocID
	h.nextAllocID++
	obj := &Object{
		HeapHeader: HeapHeader{
			Kind:     kind,
			RefCount: 1,
			Freed:    false,
		},
		TypeID:  typeID,
		AllocID: allocID,
	}
	h.objs[handle] = obj
	if h.vm != nil {
		h.vm.heapCounters.allocCount++
		h.vm.heapCounters.rcIncrCount++
	}
	return handle, obj
}

func (h *Heap) AllocString(typeID types.TypeID, s string) Handle {
	handle, obj := h.alloc(OKString, typeID)
	obj.Str = s
	obj.StrKind = StringFlat
	obj.StrFlatKnown = true
	obj.StrByteLen = len(s)
	obj.StrCPLen = 0
	obj.StrCPLenKnown = false
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

func (h *Heap) AllocStringWithCPLen(typeID types.TypeID, s string, cpLen int) Handle {
	handle, obj := h.alloc(OKString, typeID)
	obj.Str = s
	obj.StrKind = StringFlat
	obj.StrFlatKnown = true
	obj.StrByteLen = len(s)
	obj.StrCPLen = cpLen
	obj.StrCPLenKnown = true
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

func (h *Heap) AllocStringConcat(typeID types.TypeID, left, right Handle, byteLen, cpLen int, cpLenKnown bool) Handle {
	// Validate handles before retaining to avoid partial retain on panic
	if left != 0 {
		leftObj := h.Get(left)
		if leftObj.Kind != OKString {
			h.panic(PanicTypeMismatch, "left handle must be a string")
		}
	}
	if right != 0 {
		rightObj := h.Get(right)
		if rightObj.Kind != OKString {
			h.panic(PanicTypeMismatch, "right handle must be a string")
		}
	}

	handle, obj := h.alloc(OKString, typeID)
	obj.StrKind = StringConcat
	obj.StrFlatKnown = false
	obj.StrByteLen = byteLen
	obj.StrCPLen = cpLen
	obj.StrCPLenKnown = cpLenKnown
	obj.StrLeft = left
	obj.StrRight = right
	if left != 0 {
		h.Retain(left)
	}
	if right != 0 {
		h.Retain(right)
	}
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

func (h *Heap) AllocStringSlice(typeID types.TypeID, base Handle, startCP, cpLen, byteLen int) Handle {
	// Validate base handle before retaining
	if base != 0 {
		baseObj := h.Get(base)
		if baseObj.Kind != OKString {
			h.panic(PanicTypeMismatch, "base handle must be a string")
		}
	}

	handle, obj := h.alloc(OKString, typeID)
	obj.StrKind = StringSlice
	obj.StrFlatKnown = false
	obj.StrByteLen = byteLen
	obj.StrCPLen = cpLen
	obj.StrCPLenKnown = true
	obj.StrSliceBase = base
	obj.StrSliceStart = startCP
	obj.StrSliceLen = cpLen
	if base != 0 {
		h.Retain(base)
	}
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

func (h *Heap) AllocRange(typeID types.TypeID, start, end Value, hasStart, hasEnd, inclusive bool) Handle {
	handle, obj := h.alloc(OKRange, typeID)
	obj.Range = RangeObject{
		Kind:      RangeDescriptor,
		Start:     start,
		End:       end,
		HasStart:  hasStart,
		HasEnd:    hasEnd,
		Inclusive: inclusive,
	}
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

func (h *Heap) AllocArrayIterRange(typeID types.TypeID, base Handle, start, length int) Handle {
	handle, obj := h.alloc(OKRange, typeID)
	obj.Range = RangeObject{
		Kind:       RangeArrayIter,
		ArrayBase:  base,
		ArrayStart: start,
		ArrayLen:   length,
	}
	if base != 0 {
		h.Retain(base)
	}
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

func (h *Heap) AllocArray(typeID types.TypeID, elems []Value) Handle {
	handle, obj := h.alloc(OKArray, typeID)
	obj.Arr = append([]Value(nil), elems...)
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

func (h *Heap) AllocArraySlice(typeID types.TypeID, base Handle, start, length, capacity int) Handle {
	if base != 0 {
		baseObj := h.Get(base)
		if baseObj.Kind != OKArray {
			h.panic(PanicTypeMismatch, "base handle must be an array")
		}
	}
	handle, obj := h.alloc(OKArraySlice, typeID)
	obj.ArrSliceBase = base
	obj.ArrSliceStart = start
	obj.ArrSliceLen = length
	obj.ArrSliceCap = capacity
	if base != 0 {
		h.Retain(base)
	}
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

func (h *Heap) AllocMap(typeID types.TypeID) Handle {
	handle, obj := h.alloc(OKMap, typeID)
	obj.MapIndex = make(map[mapKey]int)
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

func (h *Heap) AllocStruct(typeID types.TypeID, fields []Value) Handle {
	handle, obj := h.alloc(OKStruct, typeID)
	obj.Fields = append([]Value(nil), fields...)
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

func (h *Heap) AllocTag(typeID types.TypeID, tagSym symbols.SymbolID, fields []Value) Handle {
	handle, obj := h.alloc(OKTag, typeID)
	obj.Tag.TagSym = tagSym
	obj.Tag.Fields = append([]Value(nil), fields...)
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

func (h *Heap) AllocBigInt(typeID types.TypeID, v bignum.BigInt) Handle {
	handle, obj := h.alloc(OKBigInt, typeID)
	obj.BigInt = v
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

func (h *Heap) AllocBigUint(typeID types.TypeID, v bignum.BigUint) Handle {
	handle, obj := h.alloc(OKBigUint, typeID)
	obj.BigUint = v
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

func (h *Heap) AllocBigFloat(typeID types.TypeID, v bignum.BigFloat) Handle {
	handle, obj := h.alloc(OKBigFloat, typeID)
	obj.BigFloat = v
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

func (h *Heap) Get(handle Handle) *Object {
	h.initIfNeeded()
	if handle == 0 {
		h.panic(PanicInvalidHandle, "invalid handle 0")
	}
	obj, ok := h.objs[handle]
	if !ok || obj == nil {
		h.panic(PanicInvalidHandle, fmt.Sprintf("invalid handle %d", handle))
	}
	if obj.Freed || obj.RefCount == 0 {
		h.panic(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: handle %d (alloc=%d)", handle, obj.AllocID))
	}
	return obj
}

func (h *Heap) Retain(handle Handle) {
	h.initIfNeeded()
	if handle == 0 {
		h.panic(PanicInvalidHandle, "invalid handle 0")
	}
	obj, ok := h.objs[handle]
	if !ok || obj == nil {
		h.panic(PanicInvalidHandle, fmt.Sprintf("invalid handle %d", handle))
	}
	if obj.Freed || obj.RefCount == 0 {
		h.panic(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: handle %d (alloc=%d)", handle, obj.AllocID))
	}

	obj.RefCount++
	if obj.RefCount == 0 {
		h.panic(PanicUnimplemented, fmt.Sprintf("refcount overflow: handle %d (alloc=%d)", handle, obj.AllocID))
	}
	if h.vm != nil {
		h.vm.heapCounters.rcIncrCount++
	}

	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapRetain(obj.Kind, handle, obj.RefCount)
	}
}

func (h *Heap) Release(handle Handle) {
	h.initIfNeeded()
	if handle == 0 {
		h.panic(PanicInvalidHandle, "invalid handle 0")
	}
	obj, ok := h.objs[handle]
	if !ok || obj == nil {
		h.panic(PanicInvalidHandle, fmt.Sprintf("invalid handle %d", handle))
	}
	if obj.Freed || obj.RefCount == 0 {
		h.panic(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: handle %d (alloc=%d)", handle, obj.AllocID))
	}

	obj.RefCount--
	if h.vm != nil {
		h.vm.heapCounters.rcDecrCount++
	}
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapRelease(obj.Kind, handle, obj.RefCount)
	}
	if obj.RefCount == 0 {
		h.Free(handle)
	}
}

func (h *Heap) Free(handle Handle) {
	h.initIfNeeded()
	if handle == 0 {
		h.panic(PanicInvalidHandle, "invalid handle 0")
	}
	obj, ok := h.objs[handle]
	if !ok || obj == nil {
		h.panic(PanicInvalidHandle, fmt.Sprintf("invalid handle %d", handle))
	}
	if obj.Freed {
		h.panic(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: handle %d (alloc=%d)", handle, obj.AllocID))
	}
	if obj.RefCount != 0 {
		h.panic(PanicUnimplemented, fmt.Sprintf("free called with non-zero refcount: handle %d rc=%d (alloc=%d)", handle, obj.RefCount, obj.AllocID))
	}

	if h.vm != nil {
		h.vm.heapCounters.freeCount++
	}
	obj.Freed = true

	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapFree(obj.Kind, obj)
	}

	switch obj.Kind {
	case OKArray:
		for _, v := range obj.Arr {
			h.releaseContainedValue(v)
		}
		obj.Arr = nil
	case OKArraySlice:
		if obj.ArrSliceBase != 0 {
			h.Release(obj.ArrSliceBase)
		}
		obj.ArrSliceBase = 0
		obj.ArrSliceStart = 0
		obj.ArrSliceLen = 0
		obj.ArrSliceCap = 0
	case OKMap:
		for _, entry := range obj.MapEntries {
			h.releaseContainedValue(entry.Key)
			h.releaseContainedValue(entry.Value)
		}
		obj.MapEntries = nil
		obj.MapIndex = nil
	case OKStruct:
		for _, v := range obj.Fields {
			h.releaseContainedValue(v)
		}
		obj.Fields = nil
	case OKString:
		if obj.StrLeft != 0 {
			h.Release(obj.StrLeft)
		}
		if obj.StrRight != 0 {
			h.Release(obj.StrRight)
		}
		if obj.StrSliceBase != 0 {
			h.Release(obj.StrSliceBase)
		}
		obj.Str = ""
		obj.StrKind = StringFlat
		obj.StrFlatKnown = false
		obj.StrByteLen = 0
		obj.StrCPLen = 0
		obj.StrCPLenKnown = false
		obj.StrLeft = 0
		obj.StrRight = 0
		obj.StrSliceBase = 0
		obj.StrSliceStart = 0
		obj.StrSliceLen = 0
	case OKTag:
		for _, v := range obj.Tag.Fields {
			h.releaseContainedValue(v)
		}
		obj.Tag.Fields = nil
		obj.Tag.TagSym = 0
	case OKRange:
		if obj.Range.Kind == RangeArrayIter {
			if obj.Range.ArrayBase != 0 {
				h.Release(obj.Range.ArrayBase)
			}
		} else {
			if obj.Range.HasStart {
				h.releaseContainedValue(obj.Range.Start)
			}
			if obj.Range.HasEnd {
				h.releaseContainedValue(obj.Range.End)
			}
		}
		obj.Range = RangeObject{}
	case OKBigInt:
		obj.BigInt = bignum.BigInt{}
	case OKBigUint:
		obj.BigUint = bignum.BigUint{}
	case OKBigFloat:
		obj.BigFloat = bignum.BigFloat{}
	default:
	}
}

func (h *Heap) releaseContainedValue(v Value) {
	switch v.Kind {
	case VKHandleString, VKHandleArray, VKHandleMap, VKHandleStruct, VKHandleTag, VKHandleRange, VKBigInt, VKBigUint, VKBigFloat:
		if v.H != 0 {
			h.Release(v.H)
		}
	}
}

func (h *Heap) lookup(handle Handle) (*Object, bool) {
	if h == nil {
		return nil, false
	}
	h.initIfNeeded()
	obj, ok := h.objs[handle]
	return obj, ok && obj != nil
}

func (h *Heap) panic(code PanicCode, msg string) {
	if h != nil && h.vm != nil {
		h.vm.panic(code, msg)
	}
	panic(&VMError{Code: code, Message: msg})
}
