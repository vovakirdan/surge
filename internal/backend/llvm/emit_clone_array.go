package llvm

import (
	"fmt"

	"surge/internal/types"
)

// Per-element duplication for an array buffer: the copy-side twin of the
// per-element drop next door in emit_drop_array.go.
//
// The runtime cannot answer this on its own. It walks a buffer knowing only a
// stride, and what makes a second element INDEPENDENT is entirely a property of
// the element type — a string has to duplicate its bytes, a counted block only
// has to be counted again, a composite has members of its own with the same
// question to answer. So the runtime is handed one function per element type
// and calls it, exactly as rt_array_free_elems is handed a drop.
//
// An element that owns nothing gets no glue at all. Its bits ARE the value, so
// the caller passes a null function pointer and the runtime moves the bytes
// itself; asking for a per-element call there would cost a call per element to
// do what one memcpy already did.

func cloneElemGlueName(id types.TypeID) string { return fmt.Sprintf("clone_elem.type%d", id) }

// requireCloneElemGlue records that `id` needs per-element clone glue and
// returns its name. The clone fixpoint (emitCloneGlue) then walks what the
// recursion adds.
func (e *Emitter) requireCloneElemGlue(id types.TypeID) string {
	id = resolveValueType(e.types, id)
	if e.cloneElemGlueNeeded == nil {
		e.cloneElemGlueNeeded = make(map[types.TypeID]struct{})
	}
	e.cloneElemGlueNeeded[id] = struct{}{}
	return cloneElemGlueName(id)
}

// emitCloneElemGlueBody emits `@clone_elem.typeN(ptr %dst, ptr %src)`: make the
// element slot at %dst an independent copy of the one at %src.
//
// The body is the clone glue's own shape — move the bytes, then fix up only the
// members whose bits are not the whole value — and the fixup is literally that
// glue's walk, applied at offset zero. Sharing the walk is the point: an
// element and a struct field of the same type cannot end up duplicated two
// different ways, and a shape added to one is added to both.
//
// The byte move is a whole STRIDE rather than the element's size, because that
// is the distance the buffer is walked by; the trailing padding it carries is
// inside the same allocation on both ends.
func (e *Emitter) emitCloneElemGlueBody(id types.TypeID) error {
	id = resolveValueType(e.types, id)
	// OPAQUE BASE: the glue is `@clone_elem.typeN(ptr %dst, ptr %src)` and
	// neither pointer says what it is aligned to. It is reached for a fixed
	// array's elements as well as a dynamic one's, so a `@packed` container's
	// element can arrive here — see opaqueBaseElemStrideAlign.
	stride, align, err := e.opaqueBaseElemStrideAlign(id)
	if err != nil {
		return err
	}
	if align == 0 {
		align = 1
	}
	fmt.Fprintf(&e.buf, "define void @%s(ptr %%dst, ptr %%src) {\n", cloneElemGlueName(id))
	fmt.Fprintf(&e.buf, "entry:\n")
	e.emitGlueStorageCopy("%dst", "%src", stride, align)
	e.emitFieldCloneAt(&glueTmp{}, id, align, 0)
	fmt.Fprintf(&e.buf, "  ret void\n}\n")
	return nil
}
