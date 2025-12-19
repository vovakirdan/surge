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
	return handle, obj
}

func (h *Heap) AllocString(typeID types.TypeID, s string) Handle {
	handle, obj := h.alloc(OKString, typeID)
	obj.Str = s
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

	obj.Freed = true

	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapFree(handle)
	}

	switch obj.Kind {
	case OKArray:
		for _, v := range obj.Arr {
			h.releaseContainedValue(v)
		}
		obj.Arr = nil
	case OKStruct:
		for _, v := range obj.Fields {
			h.releaseContainedValue(v)
		}
		obj.Fields = nil
	case OKString:
		obj.Str = ""
	case OKTag:
		for _, v := range obj.Tag.Fields {
			h.releaseContainedValue(v)
		}
		obj.Tag.Fields = nil
		obj.Tag.TagSym = 0
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
	case VKHandleString, VKHandleArray, VKHandleStruct, VKHandleTag, VKBigInt, VKBigUint, VKBigFloat:
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
