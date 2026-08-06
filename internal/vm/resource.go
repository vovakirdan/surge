package vm

import (
	"fmt"

	"surge/internal/types"
)

// resourceOpaqueField is the compiler-private member every runtime-resource
// type declares. The language cannot name it; it exists so the type has a
// layout, and it is where the native backend puts the word.
const resourceOpaqueField = "__opaque"

// A runtime resource — a task, a channel, an open file, a socket, a point in
// time — is named in the language by a nominal struct with that one private
// member. It is NOT an inline composite: `types.IsValueComposite` says so, its
// member is unreachable from source, and the value's whole meaning is the word
// the runtime handed out.
//
// So the VM carries it as one word rather than as a field list. Six of these
// types used to be built as a struct object holding a single boxed integer,
// each with its own copy of the same twenty lines; that shape made a resource
// look like an aggregate to every walk that asks the object what it is, and
// invited a release walk to follow a member that was never a reference.
//
// Their storage is untouched by this: the runtime still owns what the word
// names, and reclaiming a resource still means telling the runtime, never
// releasing something underneath the word.

// AllocResource allocates a runtime-resource object holding one opaque word.
func (h *Heap) AllocResource(typeID types.TypeID, word int64) Handle {
	handle, obj := h.alloc(OKResource, typeID)
	obj.Resource = word
	if h.vm != nil && h.vm.Trace != nil {
		h.vm.Trace.TraceHeapAlloc(obj.Kind, handle, obj)
	}
	return handle
}

// resourceValue builds a resource of one type, refusing a type that does not
// declare the private member. The check is kept even though the member is no
// longer materialized: it is what stops a caller handing this an ordinary
// struct type and getting back a value nothing can read.
func (vm *VM) resourceValue(word int64, typeID types.TypeID, what string) (Value, *VMError) {
	layout, vmErr := vm.layouts.Struct(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	if _, ok := layout.IndexByName[resourceOpaqueField]; !ok {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("%s missing %s field", what, resourceOpaqueField))
	}
	return MakeResource(vm.Heap.AllocResource(typeID, word), typeID), nil
}

// resourceWord reads the opaque word out of a resource.
//
// The integer cases are not a fallback: intrinsics and the runtime hand ids
// back as plain numbers, and a resource built from one has to answer the same
// question as a resource that arrived through a binding. `what` names the type
// and `unit` names the number, so a refusal says "Task" or "file handle"
// rather than "value".
func (vm *VM) resourceWord(val Value, what, unit string) (int64, *VMError) {
	if val.Kind == VKRef || val.Kind == VKRefMut {
		loaded, vmErr := vm.loadLocationRaw(val.Loc)
		if vmErr != nil {
			return 0, vmErr
		}
		val = loaded
	}
	switch val.Kind {
	case VKInt:
		return val.Int, nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok {
			return 0, vm.eb.makeError(PanicInvalidHandle, unit+" out of range")
		}
		return n, nil
	case VKBigUint:
		u, vmErr := vm.mustBigUint(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok || n >= uint64(1)<<63 {
			return 0, vm.eb.makeError(PanicInvalidHandle, unit+" out of range")
		}
		return int64(n), nil //nolint:gosec // range-checked above
	case VKResource:
		obj := vm.Heap.Get(val.H)
		if obj == nil || obj.Kind != OKResource {
			return 0, vm.eb.typeMismatch(what, fmt.Sprintf("%v", obj.Kind))
		}
		return obj.Resource, nil
	default:
		return 0, vm.eb.typeMismatch(what, val.Kind.String())
	}
}
