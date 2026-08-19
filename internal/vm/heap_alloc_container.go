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
//
// The elements are ADOPTED here rather than at each of the dozen call sites,
// because every one of them owes the same thing and none of them is where the
// obligation lives: a composite element has to be copied into storage the array
// owns, since the array outlives the activation that built it.
func (h *Heap) AllocArray(typeID types.TypeID, elems []Value) Handle {
	handle, obj := h.alloc(OKArray, typeID)
	if h.vm != nil {
		elem, ok := h.vm.arrayRunElemType(typeID)
		if !ok {
			// An array whose element type is unknown cannot reserve a run: the
			// stride, the alignment and the cell kind all come from it, and
			// guessing any of them from what the first element happens to hold
			// is the thing the run exists to stop.
			h.panic(PanicTypeMismatch,
				"an array must know its element type before it can hold anything")
		}
		if vmErr := h.vm.reserveArrayRun(obj, elem, len(elems)); vmErr != nil {
			h.panic(vmErr.Code, vmErr.Message)
		}
		frame := h.vm.currentFrame()
		for i := range elems {
			if vmErr := h.vm.initRunElem(frame, obj, i, elems[i]); vmErr != nil {
				h.panic(vmErr.Code, vmErr.Message)
			}
		}
		if vmErr := h.vm.setArrayLen(obj, len(elems)); vmErr != nil {
			h.panic(vmErr.Code, vmErr.Message)
		}
	}
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

// AllocArraySliceStorage allocates an array slice whose elements live in an
// arena, which is where a fixed array's elements live.
//
// It retains nothing. A heap slice retains its base object because the base
// outlives the slice only if someone holds it; an arena extent belongs to the
// frame or the container that owns it, and a slice header has no say in that.
// What keeps the read honest instead is the generation stamped in the ref: a
// slice that outlives its frame is refused when it resolves, rather than
// reading whatever moved in.
//
// The capacity equals the length because such a slice can never grow — every
// growing operation goes through arrayOwnedFromValue, which already refuses an
// OKArraySlice of any kind.
func (h *Heap) AllocArraySliceStorage(typeID types.TypeID, base StorageRef, start, length int) Handle {
	handle, obj := h.alloc(OKArraySlice, typeID)
	obj.ArrSliceStorage = base
	obj.ArrSliceStart = start
	obj.ArrSliceLen = length
	obj.ArrSliceCap = length
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

// AllocMap allocates a map object on the heap.
func (h *Heap) AllocMap(typeID types.TypeID) Handle {
	handle, obj := h.alloc(OKMap, typeID)
	obj.MapIndex = make(map[mapKey]int)
	if h.vm != nil {
		keyType, valType := h.vm.mapValueTypes(typeID)
		if keyType == types.NoTypeID || valType == types.NoTypeID {
			// The key and value types are the only source of stride, alignment
			// and cell kind for the two runs. A map that does not know them
			// cannot reserve storage, and guessing from the first entry is the
			// thing the runs exist to stop.
			h.panic(PanicTypeMismatch,
				"a map must know its key and value types before it can hold anything")
		}
		if vmErr := h.vm.reserveMapRuns(obj, keyType, valType, 0); vmErr != nil {
			h.panic(vmErr.Code, vmErr.Message)
		}
	}
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
