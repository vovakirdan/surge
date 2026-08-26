package vm

import (
	"fmt"

	"surge/internal/asyncrt"
	"surge/internal/types"
)

// resourceOpaqueField is the private member a runtime-resource type declares.
// It exists so the type has a layout, and it is where the native backend puts
// the word.
//
// The name alone does NOT make a type a resource. `Duration`, `TcpListener` and
// `TcpConn` spell their member the same way and are built and read FROM SURGE
// SOURCE — stdlib/time/time.sg constructs `Duration { __opaque = nanos }`, and
// stdlib/net/net.sg and stdlib/http/http.sg both construct and project the net
// ones. They are ordinary composites wearing a naming convention, they belong
// to the inline-storage work, and carrying them here would break the moment
// source built one. The test for membership is whether the word is
// unreachable from source, not what the member is called.
const resourceOpaqueField = "__opaque"

// A runtime resource — a task, a channel, an open file — is named in the
// language by a nominal struct with that one private member. It is NOT an inline composite: `types.IsValueComposite` says so, its
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
//
// Which of the two shapes a resource takes is the STORAGE model's answer, not
// this function's. `Task` and `Channel` are declared runtime-owned handles and
// have no extent, so their word is all there is; `File` is a nominal struct the
// layout registry lays out inline, exactly as the native backend does — its
// word is the private member, and building a handle instead would hand every
// slot that keeps one a value it cannot store.
func (vm *VM) resourceValue(word int64, typeID types.TypeID, what string) (Value, *VMError) {
	layout, vmErr := vm.layouts.Struct(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	index, ok := layout.IndexByName[resourceOpaqueField]
	if !ok {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("%s missing %s field", what, resourceOpaqueField))
	}
	if vm.storageCellKind(typeID) != cellComposite {
		return MakeResource(vm.Heap.AllocResource(typeID, word), typeID), nil
	}
	if index >= len(layout.FieldTypes) {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("%s %s field has no type", what, resourceOpaqueField))
	}
	opaque, vmErr := vm.makeIntForType(layout.FieldTypes[index], word)
	if vmErr != nil {
		return Value{}, vmErr
	}
	fields := make([]Value, index+1)
	fields[index] = opaque
	return vm.buildStruct(vm.currentFrame(), typeID, fields)
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
	case VKComposite:
		return vm.opaqueMemberWord(val, what, unit)
	default:
		return 0, vm.eb.typeMismatch(what, val.Kind.String())
	}
}

// opaqueMemberWord reads the word out of a resource that is laid out inline.
//
// The member is found by NAME rather than by position: the private member is
// the only thing that makes the type a resource, and a resource type that grew
// a second member would otherwise start answering with whichever member came
// first.
func (vm *VM) opaqueMemberWord(val Value, what, unit string) (int64, *VMError) {
	ref, ok := val.Storage()
	if !ok {
		return 0, vm.eb.typeMismatch(what, val.Kind.String())
	}
	layout, vmErr := vm.layouts.Struct(ref.TypeID)
	if vmErr != nil {
		return 0, vmErr
	}
	index, ok := layout.IndexByName[resourceOpaqueField]
	if !ok {
		return 0, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("%s missing %s field", what, resourceOpaqueField))
	}
	member, vmErr := vm.peekMember(ref, index)
	if vmErr != nil {
		return 0, vmErr
	}
	return vm.resourceWord(member, what, unit)
}

// resourceFreed tells the runtime that one resource word is gone.
//
// This is the half of the resource contract the heap used to skip: freeing the
// object released nothing (the runtime owns what the word names) and also SAID
// nothing, so a task could not learn that the last handle able to consume its
// result had died. Only tasks answer this today; a channel or a file word is
// reclaimed by its own close path.
func (h *Heap) resourceFreed(obj *Object) {
	if h == nil || h.vm == nil || obj == nil || obj.Resource <= 0 {
		return
	}
	if !h.vm.isTaskType(obj.TypeID) {
		return
	}
	h.vm.taskHandleReleased(asyncrt.TaskID(obj.Resource)) //nolint:gosec // positive, and task ids are executor-bounded
}
