package llvm

import (
	"fmt"

	"surge/internal/types"
)

// Handing an inline value to the runtime, and taking one back.
//
// Ordinary code has exactly one representation for a composite: a run of bytes
// at a place the program named. That is what makes the representation
// reviewable — there is no second shape to keep in mind, and no site that has
// to decide which one it is looking at.
//
// The runtime is not ordinary code. A channel holds a payload after the sender
// has returned, a task holds a result after the body's frame is gone, and a
// select stages values that outlive the arm that produced them. None of those
// can point at a caller's slot: the slot is a stack frame that has already
// been left. The typed owner storage that will hold them belongs to the waves
// that own those surfaces.
//
// Until then, a value crossing a transport boundary is copied INTO an
// allocation the transport owns on the way out, and copied back OUT of it on
// the way in. The pair below is the only place that happens. It is deliberately
// not spread through the emitters that call it: keeping it to one publish, one
// adopt and one release is what stops the box coming back as a second ordinary
// representation, and it is what makes the retirement a deletion of this file
// rather than an audit of every transport site.
//
// This is an interval measure. Giving each owner its own typed storage retires
// it, and retiring it is a deletion of this file.

// emitPublishToTransportAllocation copies the value at `storagePtr` into a
// fresh transport-owned allocation and returns it.
//
// The copy is what makes the transfer safe: the caller's storage is a frame
// slot, a field or an array element that the caller keeps owning and may
// overwrite or leave the moment this returns.
func (fe *funcEmitter) emitPublishToTransportAllocation(storagePtr string, id types.TypeID) (string, error) {
	facts, err := fe.emitter.storageFactsOf(id)
	if err != nil {
		return "", err
	}
	allocation := fe.nextTemp()
	size := facts.Size
	if size == 0 {
		// A zero-sized value still needs an address to be a payload, and an
		// allocator asked for nothing may legally answer with nothing.
		size = 1
	}
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n",
		allocation, size, facts.Align)
	fe.emitStorageCopy(allocation, storagePtr, facts.Size, facts.Align)
	return allocation, nil
}

// emitAdoptFromTransportAllocation copies a transport-owned allocation into the
// storage at `storagePtr` and releases the allocation.
//
// The value itself is MOVED: its bytes, and every claim they carry, become the
// destination's. Only the allocation that carried them is freed here, which is
// why this is a plain rt_free and not a drop.
func (fe *funcEmitter) emitAdoptFromTransportAllocation(allocation, storagePtr string, id types.TypeID) error {
	facts, err := fe.emitter.storageFactsOf(id)
	if err != nil {
		return err
	}
	fe.emitStorageCopy(storagePtr, allocation, facts.Size, facts.Align)
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_free(ptr %s, i64 %d, i64 %d)\n",
		allocation, transportAllocationSize(facts.Size), facts.Align)
	return nil
}

// runtimeOwnedReleaseName is the entry point the runtime's abandon paths call
// to destroy a value it is holding in an allocation of its own.
func runtimeOwnedReleaseName(id types.TypeID) string {
	return fmt.Sprintf("release.type%d", id)
}

// requireRuntimeOwnedRelease records that a type needs the release entry point
// and returns its name.
func (e *Emitter) requireRuntimeOwnedRelease(id types.TypeID) string {
	id = resolveValueType(e.types, id)
	if e.runtimeOwnedReleaseNeeded == nil {
		e.runtimeOwnedReleaseNeeded = make(map[types.TypeID]struct{})
	}
	e.runtimeOwnedReleaseNeeded[id] = struct{}{}
	e.requireDropGlue(id)
	return runtimeOwnedReleaseName(id)
}

// emitRuntimeOwnedReleaseBody emits one release entry point.
//
// Both halves are needed and in this order: the value's own members are
// released through the ordinary glue, which knows nothing about who allocated
// the storage, and then the allocation itself is freed. Reaching only the first
// would leak the allocation; reaching only the second would leak everything the
// value owned. The two used to be one function, because a composite's storage
// and its box were the same thing — they are not any more, and the value's glue
// is now correct for a value that lives in a caller's slot.
//
// It is drained by the drop-glue fixpoint rather than by a pass of its own,
// because it names drop glue and drop glue names it.
func (e *Emitter) emitRuntimeOwnedReleaseBody(id types.TypeID) error {
	facts, err := e.layoutOf(id)
	if err != nil {
		return err
	}
	align := facts.Align
	if align == 0 {
		align = 1
	}
	fmt.Fprintf(&e.buf, "define void @%s(ptr %%p) {\n", runtimeOwnedReleaseName(id))
	fmt.Fprintf(&e.buf, "entry:\n")
	fmt.Fprintf(&e.buf, "  %%isnull = icmp eq ptr %%p, null\n")
	fmt.Fprintf(&e.buf, "  br i1 %%isnull, label %%ret, label %%body\n")
	fmt.Fprintf(&e.buf, "body:\n")
	fmt.Fprintf(&e.buf, "  call void @%s(ptr %%p)\n", e.requireDropGlue(id))
	fmt.Fprintf(&e.buf, "  call void @rt_free(ptr %%p, i64 %d, i64 %d)\n",
		transportAllocationSize(facts.Size), align)
	fmt.Fprintf(&e.buf, "  br label %%ret\n")
	fmt.Fprintf(&e.buf, "ret:\n")
	fmt.Fprintf(&e.buf, "  ret void\n}\n\n")
	return nil
}

// transportAllocationSize is the size actually requested for a payload, which
// differs from the value's size only for a zero-sized one.
func transportAllocationSize(size uint64) uint64 {
	if size == 0 {
		return 1
	}
	return size
}
