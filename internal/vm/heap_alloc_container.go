package vm

import (
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// AllocRange allocates a range object on the heap.
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

// AllocArrayIterRange allocates an array iterator range object.
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

// AllocArray allocates an array object on the heap.
func (h *Heap) AllocArray(typeID types.TypeID, elems []Value) Handle {
	handle, obj := h.alloc(OKArray, typeID)
	obj.Arr = append([]Value(nil), elems...)
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

// AllocArraySlice allocates an array slice object.
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

// AllocMap allocates a map object on the heap.
func (h *Heap) AllocMap(typeID types.TypeID) Handle {
	handle, obj := h.alloc(OKMap, typeID)
	obj.MapIndex = make(map[mapKey]int)
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

// AllocBigInt allocates a big integer object on the heap.
func (h *Heap) AllocBigInt(typeID types.TypeID, v bignum.BigInt) Handle {
	handle, obj := h.alloc(OKBigInt, typeID)
	obj.BigInt = v
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

// AllocBigUint allocates a big unsigned integer object on the heap.
func (h *Heap) AllocBigUint(typeID types.TypeID, v bignum.BigUint) Handle {
	handle, obj := h.alloc(OKBigUint, typeID)
	obj.BigUint = v
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

// AllocBigFloat allocates a big float object on the heap.
func (h *Heap) AllocBigFloat(typeID types.TypeID, v bignum.BigFloat) Handle {
	handle, obj := h.alloc(OKBigFloat, typeID)
	obj.BigFloat = v
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}
