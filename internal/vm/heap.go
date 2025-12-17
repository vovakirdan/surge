package vm

import (
	"fmt"

	"surge/internal/types"
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
		Kind:    kind,
		TypeID:  typeID,
		Alive:   true,
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

func (h *Heap) Get(handle Handle) *Object {
	h.initIfNeeded()
	if handle == 0 {
		h.panic(PanicInvalidHandle, "invalid handle 0")
	}
	obj, ok := h.objs[handle]
	if !ok || obj == nil {
		h.panic(PanicInvalidHandle, fmt.Sprintf("invalid handle %d", handle))
	}
	if !obj.Alive {
		h.panic(PanicUseAfterFree, fmt.Sprintf("use after free: handle %d (alloc=%d)", handle, obj.AllocID))
	}
	return obj
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
	if !obj.Alive {
		h.panic(PanicDoubleFree, fmt.Sprintf("double free: handle %d (alloc=%d)", handle, obj.AllocID))
	}

	obj.Alive = false

	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapFree(handle)
	}

	switch obj.Kind {
	case OKArray:
		for _, v := range obj.Arr {
			h.freeContainedValue(v)
		}
		obj.Arr = nil
	case OKStruct:
		for _, v := range obj.Fields {
			h.freeContainedValue(v)
		}
		obj.Fields = nil
	case OKString:
		obj.Str = ""
	default:
	}
}

func (h *Heap) freeContainedValue(v Value) {
	switch v.Kind {
	case VKHandleString, VKHandleArray, VKHandleStruct:
		if v.H != 0 {
			h.Free(v.H)
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
