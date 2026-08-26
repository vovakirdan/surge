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
// the way in. Keeping that to one publish, one adopt and one release is what
// stopped the box coming back as a second ordinary representation, and it is
// what makes the retirement a deletion of this file rather than an audit of
// every transport site.
//
// THE PUBLISH LEG IS GONE. Every owner that used to copy a value in on the way
// out now has typed storage of its own, and the map was the last of them: the
// helper that boxed a value for the runtime, and the word bridge that reached
// it, are deleted rather than kept for a caller that no longer exists. What
// remains is the adopt leg, still reached from the ordinary runtime-call
// marshaller (`emit_call_site.go`), and it retires the same way -- when the
// last surface that hands a composite back through a word has storage of its
// own.

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

// payloadNeedsRuntimeRelease reports whether a payload the runtime may hold and
// never deliver has to be released by the runtime rather than forgotten.
//
// `typeOwnsHeap` alone was the whole question while a composite WAS its heap
// box: forgetting an inert composite forgot nothing, because the value and the
// allocation carrying it were one and the same, and whoever adopted it freed
// it. A composite now travels in a transport allocation of its own whatever its
// members hold, so an inert composite that is never delivered leaks that
// allocation with nothing left pointing at it. Both halves belong in the
// question: what the value owns, and what carried it.
func (e *Emitter) payloadNeedsRuntimeRelease(id types.TypeID) bool {
	if e == nil || id == types.NoTypeID {
		return false
	}
	return e.typeOwnsHeap(id) || e.hasInlineStorage(id)
}

func transportAllocationSize(size uint64) uint64 {
	if size == 0 {
		return 1
	}
	return size
}
