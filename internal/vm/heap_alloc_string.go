package vm

import (
	"surge/internal/types"
)

// AllocString allocates a string object on the heap.
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

// AllocStringWithCPLen allocates a string object with a known code point length.
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

// AllocStringConcat allocates a concatenated string object.
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

// AllocStringSlice allocates a string slice object.
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
