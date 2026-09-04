package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// How a value that lives inline is spelled in LLVM.
//
// A composite is a run of bytes at a size, an alignment and a stride that
// `internal/layout` already froze. This backend spells that run as `[N x i8]`
// rather than as a nominal LLVM struct, and the reason is not taste: a nominal
// struct would make LLVM compute its own field offsets, padding and alignment,
// and there would then be TWO layout calculators that have to agree about every
// packed field, every over-aligned member and every union arm. When they
// disagreed, the disagreement would live in the emitted code with nothing to
// report it. A byte run has no opinion, so `internal/layout` stays the only
// authority, and every offset in the emitted IR is the one it published.
//
// It also matches how places are already walked: field and element addresses
// are computed as byte offsets from a base, so the storage type is exactly what
// those offsets index into.

// storageFacts are the frozen physical facts of a type that lives inline.
type storageFacts struct {
	// Size is the number of bytes the value occupies, which is what the
	// storage type spells. It is NOT the stride: an array element advances by
	// the stride, while a lone value occupies only its size.
	Size uint64
	// Align is the alignment every access to the value must state.
	Align uint64
	// Stride is the distance from one array element to the next.
	Stride uint64
}

// storageTypeOf spells the LLVM type of memory holding one value of id.
//
// Only a value composite has one. A scalar, a handle, a reference or a dynamic
// array is carried in its own native representation and has no byte-run
// spelling, and asking for one is a bug in the caller rather than a case to
// answer with something plausible.
func (e *Emitter) storageTypeOf(id types.TypeID) (string, error) {
	facts, err := e.storageFactsOf(id)
	if err != nil {
		return "", err
	}
	return storageTypeForSize(facts.Size), nil
}

// storageTypeForSize is the byte-run spelling of a size.
//
// A zero-sized value is spelled `[0 x i8]`, which is a real LLVM type with no
// bytes: it can be allocated, addressed and passed over without inventing a
// payload byte the language never gave it. Rounding it up to one byte would
// make two adjacent zero-sized members occupy different addresses and would put
// a byte of nothing inside every composite that holds one.
func storageTypeForSize(size uint64) string {
	return fmt.Sprintf("[%d x i8]", size)
}

// storageFactsOf reads the frozen size, alignment and stride of a value
// composite.
//
// An unresolved layout is refused rather than defaulted. A composite that
// quietly fell back to a pointer-sized slot would be storage the rest of the
// program does not agree about, and every read through it would be reading
// somewhere else.
func (e *Emitter) storageFactsOf(id types.TypeID) (storageFacts, error) {
	if e == nil || e.types == nil {
		return storageFacts{}, fmt.Errorf("missing type interner")
	}
	resolved := resolveAliasAndOwn(e.types, id)
	if !e.types.IsValueComposite(resolved) {
		return storageFacts{}, fmt.Errorf(
			"type#%d (%s) is not carried in inline storage",
			id, types.Label(e.types, id))
	}
	facts, err := e.layoutOf(resolved)
	if err != nil {
		return storageFacts{}, err
	}
	if facts.Align == 0 {
		return storageFacts{}, fmt.Errorf("type#%d has no proven alignment", id)
	}
	if facts.Stride < facts.Size {
		return storageFacts{}, fmt.Errorf(
			"type#%d has stride %d below its size %d", id, facts.Stride, facts.Size)
	}
	return storageFacts{Size: facts.Size, Align: facts.Align, Stride: facts.Stride}, nil
}

// hasInlineStorage reports whether values of a type live in inline storage
// rather than in storage somebody else owns.
//
// A compiler-synthesized suspension frame is the exception, and it is a
// lifetime exception rather than a layout one. Its fields sit at the offsets
// the layout registry published, exactly like any other composite's, but the
// frame itself is handed to the runtime when a computation suspends and read
// back when it resumes — so it cannot live in the slot of a function that has
// already returned. It keeps storage the runtime owns until each owner has
// typed storage of its own to keep it in.
//
// The rule itself is `mir.LivesInInlineStorage` and is deliberately NOT restated
// here. The call contract classifies arguments and results from the same call,
// and this backend's spelling and that classification are the two halves of one
// ABI: when this file owned a copy of the exemption, the contract did not have
// it, and every `blocking { }` capture set was classified by-value while being
// spelled as a handle.
func (e *Emitter) hasInlineStorage(id types.TypeID) bool {
	if e == nil || e.types == nil {
		return false
	}
	return mir.LivesInInlineStorage(e.types, id)
}

// memberAccessAlign is the alignment an access to a member at an offset may
// claim, given the alignment of the storage it sits in.
//
// A member is only as aligned as its address actually is. A `@packed` field
// sits at an offset its own type does not divide, and an access that claimed
// the type's natural alignment would be promising the hardware something false
// — so the claim is derived from the offset, never from the member's type. The
// containing storage's alignment bounds it from above, because that is all the
// base address itself guarantees.
func memberAccessAlign(baseAlign, offset uint64) uint64 {
	if baseAlign == 0 {
		return 1
	}
	if offset == 0 {
		return baseAlign
	}
	// The largest power of two dividing the offset, capped by what the base
	// address guarantees.
	align := offset & (^offset + 1)
	if align > baseAlign {
		return baseAlign
	}
	return align
}
