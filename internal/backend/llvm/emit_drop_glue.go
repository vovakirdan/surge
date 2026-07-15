package llvm

import (
	"fmt"

	"surge/internal/types"
)

// Recursive drop glue: a composite value (struct, tuple, union, fixed
// array) that transitively owns heap state gets a generated
// `@drop.type<ID>(ptr)` function that frees its owning fields/elements
// and then the box itself. InstrDrop on such a value calls the glue;
// the leaf helpers (rt_string_free / rt_array_free) handle strings and
// dynamic arrays. Glue is generated on demand and emitted after the
// user functions (LLVM allows calls before definitions).

// typeOwnsHeap reports whether a value of this type (transitively) owns
// heap storage that a drop must reclaim.
func (e *Emitter) typeOwnsHeap(id types.TypeID) bool {
	return e.typeOwnsHeapRec(id, map[types.TypeID]struct{}{})
}

func (e *Emitter) typeOwnsHeapRec(id types.TypeID, seen map[types.TypeID]struct{}) bool {
	if id == types.NoTypeID || e.types == nil {
		return false
	}
	// A borrowed handle (&T, *T) owns nothing the drop glue may reclaim:
	// freeing it double-frees or frees a value the callee never owned. This
	// must be checked BEFORE resolveValueType, which strips a reference down
	// to its pointee (so `&string` would otherwise look like an owned string —
	// exactly what made FmtArg's `FmtStr(&string)` payload double-free).
	if base := resolveAliasAndOwn(e.types, id); base != types.NoTypeID {
		if tt, ok := e.types.Lookup(base); ok {
			switch tt.Kind {
			case types.KindReference, types.KindPointer:
				return false
			}
		}
	}
	id = resolveValueType(e.types, id)
	if _, ok := seen[id]; ok {
		// A recursive type reached itself: whether it owns heap is
		// decided by its OTHER members; this edge contributes nothing.
		return false
	}
	seen[id] = struct{}{}

	if isStringLike(e.types, id) {
		return true
	}
	if _, dynamic, isArray := arrayElemType(e.types, id); isArray && dynamic {
		return true
	}
	if elem, _, ok := arrayFixedInfo(e.types, id); ok {
		return e.typeOwnsHeapRec(elem, seen)
	}
	tt, ok := e.types.Lookup(id)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindStruct:
		for _, f := range e.types.StructFields(id) {
			if e.typeOwnsHeapRec(f.Type, seen) {
				return true
			}
		}
		return false
	case types.KindTuple:
		if info, ok := e.types.TupleInfo(id); ok && info != nil {
			for _, el := range info.Elems {
				if e.typeOwnsHeapRec(el, seen) {
					return true
				}
			}
		}
		return false
	case types.KindUnion:
		cases, err := e.tagCases(id)
		if err != nil {
			return false
		}
		for _, c := range cases {
			for _, pt := range c.PayloadTypes {
				if e.typeOwnsHeapRec(pt, seen) {
					return true
				}
			}
		}
		return false
	default:
		// Enums (bare tags), maps (deferred), references, pointers,
		// scalars: no owned heap the drop glue reclaims here.
		return false
	}
}

// isBoxedComposite reports whether the type is a heap-boxed composite
// whose drop is a generated glue function (as opposed to a leaf).
func (e *Emitter) isBoxedComposite(id types.TypeID) bool {
	id = resolveValueType(e.types, id)
	if isStringLike(e.types, id) {
		return false
	}
	if _, dynamic, isArray := arrayElemType(e.types, id); isArray && dynamic {
		return false
	}
	if _, _, ok := arrayFixedInfo(e.types, id); ok {
		return true
	}
	tt, ok := e.types.Lookup(id)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindStruct, types.KindTuple, types.KindUnion:
		return true
	default:
		return false
	}
}

func dropGlueName(id types.TypeID) string     { return fmt.Sprintf("drop.type%d", id) }
func dropElemGlueName(id types.TypeID) string { return fmt.Sprintf("drop_elem.type%d", id) }

func (e *Emitter) requireDropGlue(id types.TypeID) string {
	id = resolveValueType(e.types, id)
	if e.dropGlueNeeded == nil {
		e.dropGlueNeeded = make(map[types.TypeID]struct{})
	}
	e.dropGlueNeeded[id] = struct{}{}
	return dropGlueName(id)
}

func (e *Emitter) requireDropElemGlue(id types.TypeID) string {
	id = resolveValueType(e.types, id)
	if e.dropElemGlueNeeded == nil {
		e.dropElemGlueNeeded = make(map[types.TypeID]struct{})
	}
	e.dropElemGlueNeeded[id] = struct{}{}
	return dropElemGlueName(id)
}

// glueTmp hands out fresh SSA names within a generated function.
type glueTmp struct{ n int }

func (g *glueTmp) next() string { g.n++; return fmt.Sprintf("%%g%d", g.n) }

// emitDropValue frees a value already loaded as `val` (a pointer for
// pointer-shaped owning types) of type `ty`. Records nested glue needs.
func (e *Emitter) emitDropValue(val string, ty types.TypeID) {
	ty = resolveValueType(e.types, ty)
	if isStringLike(e.types, ty) {
		fmt.Fprintf(&e.buf, "  call void @rt_string_free(ptr %s)\n", val)
		return
	}
	if elem, dynamic, isArray := arrayElemType(e.types, ty); isArray && dynamic {
		e.emitDropDynArray(val, elem)
		return
	}
	if e.isBoxedComposite(ty) && e.typeOwnsHeap(ty) {
		fmt.Fprintf(&e.buf, "  call void @%s(ptr %s)\n", e.requireDropGlue(ty), val)
	}
}

// emitDropDynArray drops a dynamic array handle `val` with element type
// `elem`. Copyable elements use the leaf rt_array_free; droppable
// elements route through rt_array_free_elems with a per-element drop fn.
func (e *Emitter) emitDropDynArray(val string, elem types.TypeID) {
	stride, elemAlign, ok := e.elemStrideAlign(elem)
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

func (e *Emitter) elemStrideAlign(elem types.TypeID) (stride, align int, ok bool) {
	elemLLVM, err := llvmValueType(e.types, elem)
	if err != nil {
		return 0, 0, false
	}
	elemSize, elemAlign, err := llvmTypeSizeAlign(elemLLVM)
	if err != nil {
		return 0, 0, false
	}
	if elemAlign <= 0 {
		elemAlign = 1
	}
	return roundUpInt(elemSize, elemAlign), elemAlign, true
}

// emitDropGlue emits every needed glue function, processing to a
// fixpoint (a glue body can require more glue).
func (e *Emitter) emitDropGlue() error {
	done := make(map[types.TypeID]struct{})
	doneElem := make(map[types.TypeID]struct{})
	for {
		progressed := false
		for id := range e.dropGlueNeeded {
			if _, ok := done[id]; ok {
				continue
			}
			done[id] = struct{}{}
			if err := e.emitDropGlueBody(id); err != nil {
				return err
			}
			progressed = true
		}
		for id := range e.dropElemGlueNeeded {
			if _, ok := doneElem[id]; ok {
				continue
			}
			doneElem[id] = struct{}{}
			e.emitDropElemGlueBody(id)
			progressed = true
		}
		if !progressed {
			break
		}
	}
	return nil
}

// emitDropElemGlueBody emits `@drop_elem.typeN(ptr %slot)`: load the
// element value from its slot and drop it (rt_array_free_elems calls
// this once per element slot).
func (e *Emitter) emitDropElemGlueBody(id types.TypeID) {
	fmt.Fprintf(&e.buf, "define void @%s(ptr %%slot) {\n", dropElemGlueName(id))
	fmt.Fprintf(&e.buf, "entry:\n")
	g := &glueTmp{}
	v := g.next()
	fmt.Fprintf(&e.buf, "  %s = load ptr, ptr %%slot\n", v)
	e.emitDropValue(v, id)
	fmt.Fprintf(&e.buf, "  ret void\n}\n")
}

// emitDropGlueBody emits `@drop.typeN(ptr %p)` for a boxed composite:
// free every owning field/element/payload, then the box. Null-safe.
func (e *Emitter) emitDropGlueBody(id types.TypeID) error {
	id = resolveValueType(e.types, id)
	layoutInfo, err := e.layoutOf(id)
	if err != nil {
		return err
	}
	align := layoutInfo.Align
	if align <= 0 {
		align = 1
	}
	fmt.Fprintf(&e.buf, "define void @%s(ptr %%p) {\n", dropGlueName(id))
	fmt.Fprintf(&e.buf, "entry:\n")
	fmt.Fprintf(&e.buf, "  %%isnull = icmp eq ptr %%p, null\n")
	fmt.Fprintf(&e.buf, "  br i1 %%isnull, label %%ret, label %%body\n")
	fmt.Fprintf(&e.buf, "body:\n")

	g := &glueTmp{}
	if elem, length, ok := arrayFixedInfo(e.types, id); ok {
		e.emitFixedArrayElemDrops(g, elem, int(length))
	} else if tt, ok := e.types.Lookup(id); ok {
		switch tt.Kind {
		case types.KindStruct:
			for _, f := range e.types.StructFields(id) {
				e.emitFieldDrop(g, f.Type, layoutInfo.FieldOffsets, structFieldIndex(e, id, f))
			}
		case types.KindTuple:
			if info, ok := e.types.TupleInfo(id); ok && info != nil {
				for i, el := range info.Elems {
					e.emitFieldDropAt(g, el, offsetAt(layoutInfo.FieldOffsets, i))
				}
			}
		case types.KindUnion:
			e.emitUnionPayloadDrops(g, id, layoutInfo.PayloadOffset)
		}
	}

	fmt.Fprintf(&e.buf, "  call void @rt_free(ptr %%p, i64 %d, i64 %d)\n", layoutInfo.Size, align)
	fmt.Fprintf(&e.buf, "  br label %%ret\n")
	fmt.Fprintf(&e.buf, "ret:\n")
	fmt.Fprintf(&e.buf, "  ret void\n}\n")
	return nil
}

func structFieldIndex(e *Emitter, id types.TypeID, target types.StructField) int {
	for i, f := range e.types.StructFields(id) {
		if f.Name == target.Name {
			return i
		}
	}
	return -1
}

func offsetAt(offsets []int, i int) int {
	if i < 0 || i >= len(offsets) {
		return -1
	}
	return offsets[i]
}

// emitFieldDrop drops one struct field (by index into FieldOffsets).
func (e *Emitter) emitFieldDrop(g *glueTmp, fieldType types.TypeID, offsets []int, idx int) {
	e.emitFieldDropAt(g, fieldType, offsetAt(offsets, idx))
}

// emitFieldDropAt drops a pointer-shaped owning field/element stored at
// byte offset `off` within the box `%p`.
func (e *Emitter) emitFieldDropAt(g *glueTmp, fieldType types.TypeID, off int) {
	if off < 0 || !e.typeOwnsHeap(fieldType) {
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
	stride, _, ok := e.elemStrideAlign(elem)
	if !ok {
		return
	}
	for i := range length {
		e.emitFieldDropAt(g, elem, i*stride)
	}
}

// emitUnionPayloadDrops reads the tag and drops the active variant's
// owning payload. A default (no droppable payload) falls straight to
// freeing the box.
func (e *Emitter) emitUnionPayloadDrops(g *glueTmp, id types.TypeID, payloadOffset int) {
	cases, err := e.tagCases(id)
	if err != nil {
		return
	}
	// Collect cases with droppable payloads.
	type dropCase struct {
		idx        int
		offsets    []int
		payloadTys []types.TypeID
	}
	var droppable []dropCase
	for ci, c := range cases {
		if len(c.PayloadTypes) == 0 {
			continue
		}
		anyOwns := false
		for _, pt := range c.PayloadTypes {
			if e.typeOwnsHeap(pt) {
				anyOwns = true
				break
			}
		}
		if !anyOwns {
			continue
		}
		offs, offErr := e.payloadOffsets(c.PayloadTypes)
		if offErr != nil {
			continue
		}
		droppable = append(droppable, dropCase{idx: ci, offsets: offs, payloadTys: c.PayloadTypes})
	}
	if len(droppable) == 0 {
		return
	}
	tag := g.next()
	fmt.Fprintf(&e.buf, "  %s = load i32, ptr %%p\n", tag)
	// Fix the block-id ONCE: field drops below call g.next(), which would
	// otherwise drift the label names apart from the switch targets.
	uid := g.n
	freebox := fmt.Sprintf("u%d_free", uid)
	fmt.Fprintf(&e.buf, "  switch i32 %s, label %%%s [", tag, freebox)
	for _, dc := range droppable {
		fmt.Fprintf(&e.buf, " i32 %d, label %%u%d_c%d", dc.idx, uid, dc.idx)
	}
	fmt.Fprintf(&e.buf, " ]\n")
	for _, dc := range droppable {
		fmt.Fprintf(&e.buf, "u%d_c%d:\n", uid, dc.idx)
		for i, pt := range dc.payloadTys {
			e.emitFieldDropAt(g, pt, payloadOffset+dc.offsets[i])
		}
		fmt.Fprintf(&e.buf, "  br label %%%s\n", freebox)
	}
	fmt.Fprintf(&e.buf, "%s:\n", freebox)
}
