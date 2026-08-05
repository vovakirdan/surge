package llvm

import (
	"fmt"

	"surge/internal/types"
)

// emitDropDynArray drops a dynamic array handle `val` with element type
// `elem`. Copyable elements use the leaf rt_array_free; droppable
// elements route through rt_array_free_elems with a per-element drop fn.
func (e *Emitter) emitDropDynArray(val string, elem types.TypeID) {
	stride, elemAlign, ok := e.emittedElemStrideAlign(elem)
	if !ok {
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

func (e *Emitter) emittedElemStrideAlign(elem types.TypeID) (stride, align uint64, ok bool) {
	llvmTy, err := llvmValueType(e.types, elem)
	if err != nil {
		return 0, 0, false
	}
	stride, align, err = llvmTypeStrideAlign(llvmTy)
	if err != nil {
		return 0, 0, false
	}
	return stride, align, true
}

// emitFieldDropAt drops a pointer-shaped owning field/element stored at
// byte offset `off` within the box `%p`.
func (e *Emitter) emitFieldDropAt(g *glueTmp, fieldType types.TypeID, off uint64) {
	if !e.typeOwnsHeap(fieldType) || !e.fieldDropIsExclusive(fieldType) {
		return
	}
	fp := g.next()
	fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds i8, ptr %%p, i64 %d\n", fp, off)
	fv := g.next()
	fmt.Fprintf(&e.buf, "  %s = load ptr, ptr %s\n", fv, fp)
	e.emitDropValue(fv, fieldType)
}

// emitFixedArrayElemDrops drops each of the N pointer-shaped owning
// elements of a fixed array box.
func (e *Emitter) emitFixedArrayElemDrops(g *glueTmp, elem types.TypeID, length int) {
	if length <= 0 || !e.typeOwnsHeap(elem) {
		return
	}
	elemLayout, err := e.layoutOf(elem)
	if err != nil {
		return
	}
	stride := elemLayout.Stride
	for i := range length {
		e.emitFieldDropAt(g, elem, uint64(i)*stride)
	}
}
