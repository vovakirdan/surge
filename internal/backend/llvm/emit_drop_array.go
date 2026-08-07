package llvm

import (
	"fmt"

	"surge/internal/types"
)

// emitDropDynArray drops a dynamic array handle `val` with element type
// `elem`. Copyable elements use the leaf rt_array_free; droppable
// elements route through rt_array_free_elems with a per-element drop fn.
func (e *Emitter) emitDropDynArray(val string, elem types.TypeID) {
	stride, elemAlign, err := e.arrayElemStrideAlign(elem)
	if err != nil {
		return
	}
	if e.typeOwnsHeap(elem) {
		fmt.Fprintf(&e.buf,
			"  call void @rt_array_free_elems(ptr %s, i64 %d, i64 %d, ptr @%s)\n",
			val, stride, elemAlign, e.requireDropElemGlue(elem))
		return
	}
	fmt.Fprintf(&e.buf,
		"  call void @rt_array_free(ptr %s, i64 %d, i64 %d)\n", val, stride, elemAlign)
}

// emitMemberDropAt drops the member of memberType living at byte offset `off`
// inside the storage `base`, which is aligned to baseAlign.
//
// The two member shapes part company here, and the parting is the whole of what
// the representation change did to a drop. A member that IS a handle — a
// string, a dynamic array, a counted scalar — still has its word read out and
// released. A member that is a value composite no longer has a word at all: its
// bytes are at this offset, so its glue is called on the ADDRESS, and the load
// that used to stand between the two would now read the first eight bytes of
// the member and free them as if they were a pointer.
func (e *Emitter) emitMemberDropAt(g *glueTmp, memberType types.TypeID, base string, baseAlign, off uint64) {
	if !e.typeOwnsHeap(memberType) || !e.fieldDropIsExclusive(memberType) {
		return
	}
	memberPtr := g.next()
	fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", memberPtr, base, off)
	if e.hasInlineStorage(memberType) {
		e.emitDropStorage(memberPtr, memberType)
		return
	}
	word := g.next()
	fmt.Fprintf(&e.buf, "  %s = load ptr, ptr %s, align %d\n",
		word, memberPtr, memberAccessAlign(baseAlign, off))
	e.emitDropHandle(word, memberType)
}

// emitFixedArrayElemDrops drops each owning element of a fixed array's inline
// storage.
func (e *Emitter) emitFixedArrayElemDrops(g *glueTmp, elem types.TypeID, base string, baseAlign uint64, length int) {
	if length <= 0 || !e.typeOwnsHeap(elem) {
		return
	}
	stride, err := e.arrayElemStride(elem)
	if err != nil {
		return
	}
	for i := range length {
		e.emitMemberDropAt(g, elem, base, baseAlign, uint64(i)*stride)
	}
}
