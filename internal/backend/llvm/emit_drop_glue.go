package llvm

import (
	"fmt"
	"sort"

	"surge/internal/mir"
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
//
// This is the backend's structural leg of the OwnsHeap axis that sema names
// in `internal/sema/ownership_axes.go`. The two legs answer the same question
// by different means — sema asks whether the type is Copy, this one walks the
// composite and looks for a heap-bearing leaf — and they must stay in
// agreement: sema decides WHETHER a value carries a drop obligation, this
// decides whether that drop has any glue to call. Widening one without the
// other either drops nothing or drops through a function that was never
// emitted.
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

	if e.types.IsRefCountedScalar(id) {
		return true
	}
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

// isUnionValueType reports whether the value type is a tag union (whose
// runtime representation is always a heap box, even when no payload owns
// further heap).
func isUnionValueType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	id = resolveValueType(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindUnion
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

// registerCrossingDropState records that the crossing body `bodyID` ships
// an owned state of type `stateType`: the body FuncID doubles as the
// drop-fn id the runtime's abandon paths pass to `__surge_drop_call`,
// and the dispatch routes it to the state's recursive glue.
func (e *Emitter) registerCrossingDropState(bodyID mir.FuncID, stateType types.TypeID) error {
	resolved := resolveValueType(e.types, stateType)
	if e.crossingDropStates == nil {
		e.crossingDropStates = make(map[mir.FuncID]types.TypeID)
	}
	if prev, ok := e.crossingDropStates[bodyID]; ok && prev != resolved {
		return fmt.Errorf("crossing body %d registered two state types (%d, %d)", bodyID, prev, resolved)
	}
	e.crossingDropStates[bodyID] = resolved
	e.requireDropGlue(resolved)
	return nil
}

func dropResultGlueName(id types.TypeID) string { return fmt.Sprintf("drop_result.type%d", id) }

// emitResultDropDispatch emits `__surge_drop_result_call` (RV2-DEBT-053a):
// the owner-side release path reclaims a heap-carried body RESULT that no
// consumer took. Every crossing that ships a droppable result registered its
// result payload TypeID as the drop-fn id; the arm frees the raw result_bits
// through the value's drop wrapper. Copy/inert results keep id 0 (never
// dispatched), and the panic arm is the negative control.
func (e *Emitter) emitResultDropDispatch() {
	resultDropIDs := make([]types.TypeID, 0, len(e.crossingDropResults))
	for id := range e.crossingDropResults {
		resultDropIDs = append(resultDropIDs, id)
	}
	sort.Slice(resultDropIDs, func(i, j int) bool { return resultDropIDs[i] < resultDropIDs[j] })
	fmt.Fprintf(&e.buf, "define void @__surge_drop_result_call(i64 %%id, ptr %%value) {\n")
	fmt.Fprintf(&e.buf, "entry:\n")
	fmt.Fprintf(&e.buf, "  switch i64 %%id, label %%drop_result_default [\n")
	for _, id := range resultDropIDs {
		fmt.Fprintf(&e.buf, "    i64 %d, label %%drop_result.%d\n", id, id)
	}
	fmt.Fprintf(&e.buf, "  ]\n")
	for _, id := range resultDropIDs {
		fmt.Fprintf(&e.buf, "drop_result.%d:\n", id)
		fmt.Fprintf(&e.buf, "  call void @%s(ptr %%value)\n", dropResultGlueName(id))
		fmt.Fprintf(&e.buf, "  ret void\n")
	}
	fmt.Fprintf(&e.buf, "drop_result_default:\n")
	if sc, ok := e.stringConsts["missing drop function"]; ok && sc.globalName != "" {
		fmt.Fprintf(&e.buf, "  call void @rt_panic(ptr getelementptr inbounds ([%d x i8], ptr @%s, i64 0, i64 0), i64 %d)\n", sc.arrayLen, sc.globalName, sc.dataLen)
	}
	fmt.Fprintf(&e.buf, "  unreachable\n")
	fmt.Fprintf(&e.buf, "}\n\n")
}

// registerCrossingDropResult records that a crossing ships a heap-carried
// RESULT of type `resultType` over the reply edge: the resolved payload
// TypeID doubles as the drop-fn id the runtime's owner-side release path
// passes to `__surge_drop_result_call`. Callers gate on typeOwnsHeap, so
// id 0 (a Copy/inert result) is never registered and never dispatched.
// registerAbandonedStateDrop records that a suspend-point/scope-join state
// box may need reclaiming if a cancellation abandons it. The resolved
// TypeID doubles as the drop-fn id __surge_drop_abandoned_state_call
// dispatches on, routed to the state struct's recursive box-freeing glue
// (dropGlueName) — unlike registerCrossingDropResult, there is no inert/
// Copy case to gate callers on: the box always exists, so it always needs
// freeing, whether or not any of its fields separately own heap.
func (e *Emitter) registerAbandonedStateDrop(stateType types.TypeID) types.TypeID {
	resolved := resolveValueType(e.types, stateType)
	if e.abandonedStateDrops == nil {
		e.abandonedStateDrops = make(map[types.TypeID]struct{})
	}
	e.abandonedStateDrops[resolved] = struct{}{}
	e.requireDropGlue(resolved)
	return resolved
}

func (e *Emitter) registerCrossingDropResult(resultType types.TypeID) types.TypeID {
	resolved := resolveValueType(e.types, resultType)
	if e.crossingDropResults == nil {
		e.crossingDropResults = make(map[types.TypeID]struct{})
	}
	e.crossingDropResults[resolved] = struct{}{}
	if e.dropResultGlueNeeded == nil {
		e.dropResultGlueNeeded = make(map[types.TypeID]struct{})
	}
	e.dropResultGlueNeeded[resolved] = struct{}{}
	return resolved
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
	if e.types.IsRefCountedScalar(ty) {
		// The container is giving back the reference it held. Whether the block
		// dies here depends on who else still points at it.
		fmt.Fprintf(&e.buf, "  call void @rt_bigfloat_release(ptr %s)\n", val)
		return
	}
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
	doneResult := make(map[types.TypeID]struct{})
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
		for id := range e.dropResultGlueNeeded {
			if _, ok := doneResult[id]; ok {
				continue
			}
			doneResult[id] = struct{}{}
			e.emitDropResultGlueBody(id)
			progressed = true
		}
		if !progressed {
			break
		}
	}
	return nil
}

// emitDropResultGlueBody emits `@drop_result.typeN(ptr %val)`: drop a
// heap-carried reply-edge RESULT loaded as its raw bits pointer. Reuses
// emitDropValue, so a string frees via rt_string_free, a dynamic array
// via rt_array_free(_elems), and a boxed composite via its recursive
// glue — recording any nested glue the fixpoint then emits.
func (e *Emitter) emitDropResultGlueBody(id types.TypeID) {
	fmt.Fprintf(&e.buf, "define void @%s(ptr %%val) {\n", dropResultGlueName(id))
	fmt.Fprintf(&e.buf, "entry:\n")
	fmt.Fprintf(&e.buf, "  %%isnull = icmp eq ptr %%val, null\n")
	fmt.Fprintf(&e.buf, "  br i1 %%isnull, label %%ret, label %%body\n")
	fmt.Fprintf(&e.buf, "body:\n")
	e.emitDropValue("%val", id)
	fmt.Fprintf(&e.buf, "  br label %%ret\n")
	fmt.Fprintf(&e.buf, "ret:\n")
	fmt.Fprintf(&e.buf, "  ret void\n}\n")
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
