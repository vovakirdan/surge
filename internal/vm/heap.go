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

// AllocStruct allocates a struct object on the heap.
func (h *Heap) AllocStruct(typeID types.TypeID, fields []Value) Handle {
	handle, obj := h.alloc(OKStruct, typeID)
	obj.Fields = append([]Value(nil), fields...)
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

// AllocTag allocates a tagged union object on the heap.
func (h *Heap) AllocTag(typeID types.TypeID, tagSym symbols.SymbolID, fields []Value) Handle {
	handle, obj := h.alloc(OKTag, typeID)
	obj.Tag.TagSym = tagSym
	obj.Tag.Fields = append([]Value(nil), fields...)
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

// Get retrieves an object from the heap by handle.
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

// Retain increments the reference count of an object.
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

// Release decrements the reference count of an object and frees it if the count reaches zero.
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

// Free frees an object on the heap.
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
		for i := range obj.MapEntries {
			h.releaseContainedValue(obj.MapEntries[i].Key)
			h.releaseContainedValue(obj.MapEntries[i].Value)
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
	if v.Kind == VKResource {
		if v.H != 0 {
			h.Release(v.H)
		}
		return
	}
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
