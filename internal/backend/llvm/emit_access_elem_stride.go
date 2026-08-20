package llvm

import "surge/internal/types"

// How far apart array elements sit, and how well-aligned each one is.
//
// These answers travel together on purpose. A stride without the matching
// alignment invites a caller to derive the alignment a second time, and a
// second derivation is a second answer waiting to disagree with the first --
// which is how a @packed element came to be accessed at an alignment its
// address did not have.

// arrayElemStride is the distance from one array element to the next.
//
// One answer now serves fixed and dynamic arrays alike, because both hold their
// elements at the element type's own layout. Two answers used to be needed: a
// dynamic array stored the emitted LLVM value, so a boxed composite element sat
// in one pointer-sized slot, while a fixed array already allocated the language
// layout — and every producer and consumer had to know which kind of array it
// was looking at to know which stride to walk by. A composite element that is
// its own bytes ends that split.
func (e *Emitter) arrayElemStride(elemType types.TypeID) (uint64, error) {
	if e.hasInlineStorage(elemType) {
		facts, err := e.storageFactsOf(elemType)
		if err != nil {
			return 0, err
		}
		return facts.Stride, nil
	}
	elemLLVM, err := e.llvmValueType(elemType)
	if err != nil {
		return 0, err
	}
	stride, _, err := llvmTypeStrideAlign(elemLLVM)
	if err != nil {
		return 0, err
	}
	return stride, nil
}

// There is no single "array element alignment", and the two functions below
// exist so that no site can ask for one.
//
// Where an array's elements live decides who gets to answer. A DYNAMIC array's
// elements sit in a buffer the runtime allocated for them and reached through
// the array header, so the element type is the authority. A FIXED array's
// elements sit INLINE in whatever holds the array, so the container is — and a
// `@packed` container places the array at an offset its element type does not
// divide. One function answering both questions is how an access to a packed
// container's element came to claim its element type's alignment against an
// address that is odd for every index (RV2-DEBT-226).
//
// Splitting the name is what turns the classification into a compile error: a
// site cannot reach an element without saying which storage it is reaching
// into, and the inline one cannot be called at all without producing the base
// address's alignment to fold against.

// handleArrayElemStrideAlign is the stride a DYNAMIC array's element buffer is
// walked by, together with the alignment its elements are allocated and
// accessed at.
//
// The buffer is the runtime's, allocated at the element type's own alignment,
// and it is reached by loading a data pointer out of the array header. Nothing
// about the container the HANDLE sits in reaches it: a handle stored inside a
// `@packed` struct is itself at an odd offset while the buffer it names is
// still aligned. So the element type is the right authority here, and deriving
// the alignment from it is correct rather than merely convenient.
func (e *Emitter) handleArrayElemStrideAlign(elemType types.TypeID) (stride, align uint64, err error) {
	stride, err = e.arrayElemStride(elemType)
	if err != nil {
		return 0, 0, err
	}
	align, err = e.arrayElemNaturalAlign(elemType)
	if err != nil {
		return 0, 0, err
	}
	return stride, align, nil
}

// inlineArrayElemStrideAlign is the stride a FIXED array is walked by, together
// with the alignment its elements may actually claim given what the array's own
// address is aligned to.
//
// A fixed array carries no header and no buffer of its own: it lives inline in
// whatever holds it, so its elements inherit that container's placement.
// Element i sits at base + i*stride, and one claim has to hold for EVERY index
// at once — element 0 sits exactly on the base, and the last one sits
// (len-1)*stride past it. The alignment every such address shares is the
// largest power of two dividing the stride, capped by what the base guarantees.
//
// baseAlign is what the ARRAY's own address is aligned to. It is a parameter
// because it is a property of the ADDRESS, and no type can be asked for it. The
// element type still caps the answer from the other side: an access never needs
// more alignment than its type needs, and taking the smaller of the two keeps
// this a narrowing of what may be claimed and never a widening.
func (e *Emitter) inlineArrayElemStrideAlign(baseAlign uint64, elemType types.TypeID) (stride, align uint64, err error) {
	stride, err = e.arrayElemStride(elemType)
	if err != nil {
		return 0, 0, err
	}
	natural, err := e.arrayElemNaturalAlign(elemType)
	if err != nil {
		return 0, 0, err
	}
	// A zero stride puts every element on the base itself, so the base's own
	// alignment is what they all share; memberAccessAlign reads offset 0 that
	// way already.
	align = memberAccessAlign(baseAlign, stride)
	// A natural alignment of 0 means the registry had none to give, not that
	// the address guarantees nothing; narrowing to it would produce `align 0`,
	// which is not an alignment LLVM accepts.
	if natural > 0 && natural < align {
		align = natural
	}
	return stride, align, nil
}

// opaqueBaseElemStrideAlign is the stride and alignment for elements reached
// through an address whose provenance this emission cannot see: a per-type
// glue's `ptr` parameter, or a data word read back out of a runtime cursor
// descriptor. Neither carries what it is aligned to, so this ASSUMES the base
// is placed at what the element type wants.
//
// The assumption is not established, and the name says so rather than letting a
// neutral-sounding call hide it. Both shapes it covers are open questions on
// RV2-DEBT-226: whether per-type drop/clone glue may assume its own alignment,
// and what an array cursor over a fixed array inside a `@packed` container may
// claim. Answering either means giving the descriptor or the glue signature an
// alignment to carry, which is a change to a runtime contract rather than to
// this file.
func (e *Emitter) opaqueBaseElemStrideAlign(elemType types.TypeID) (stride, align uint64, err error) {
	stride, err = e.arrayElemStride(elemType)
	if err != nil {
		return 0, 0, err
	}
	align, err = e.arrayElemNaturalAlign(elemType)
	if err != nil {
		return 0, 0, err
	}
	return stride, align, nil
}

// arrayElemNaturalAlign is the alignment an element of this type is placed at
// when nothing else constrains it. It is only ever half of an answer: see the
// functions above for which half.
//
// A type the registry has no alignment for answers 1 rather than 0, because 0
// is not an alignment: every address is 1-aligned, so it is the claim that
// promises nothing and is therefore always true.
func (e *Emitter) arrayElemNaturalAlign(elemType types.TypeID) (uint64, error) {
	elemLLVM, err := e.llvmValueType(elemType)
	if err != nil {
		return 0, err
	}
	align, err := e.storageAlignOf(elemType, elemLLVM)
	if err != nil {
		return 0, err
	}
	if align == 0 {
		return 1, nil
	}
	return align, nil
}
