package llvm

import (
	"fmt"
	"sort"

	"surge/internal/layout"
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

	// A reference-counted value owns one reference: a scalar's to its counted
	// block, a channel handle's to the runtime object it names. The channel
	// answered NO here for as long as it was "Copy, so nothing to reclaim",
	// which is what left every local channel unreclaimed and every composite
	// holding one with a drop body that freed nothing. The release it reaches
	// is `rt_channel_handle_drop`, in emitDropHandle; the two are widened
	// together, because a walk that claims storage the leaf cannot give back
	// is exactly that empty body again.
	if e.types.IsRefCounted(id) {
		return true
	}
	if isStringLike(e.types, id) {
		return true
	}
	// A Range is a heap object with no refcount, exactly like the two above, and
	// it was missing here rather than excluded: a struct whose only heap member
	// was a Range answered NO, so it got no glue at all and the range leaked
	// with nothing to notice.
	if _, isRange := rangeElemType(e.types, id); isRange {
		return true
	}
	// A value composite used to answer YES here whatever its fields held,
	// because it owned its box and something had to free it — which is why a
	// struct of plain scalars carried a drop obligation at all. It owns no box
	// now: its storage belongs to whoever declared it, and a drop reclaims only
	// what its members own. So the structural walk below is the whole answer,
	// and a composite of Copy scalars correctly needs no drop.
	if _, dynamic, isArray := arrayElemType(e.types, id); isArray && dynamic {
		return true
	}
	// A local task handle holds a reference the runtime counts, so a composite
	// that carries one owns something its drop has to give back. It was the
	// one handle family the walk excluded, and the exclusion is where an
	// abandoned frame's awaited-child handle leaked (RV2-DEBT-198's residual).
	if isTaskType(e.types, id) {
		return true
	}
	// A map owns its entry storage and every key and value in it, and until
	// this arm existed it answered NO: no glue was emitted, so a dropped map
	// reclaimed nothing at all and neither did a struct holding one
	// (RV2-DEBT-156).
	if _, _, isMap := e.types.MapInfo(id); isMap {
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
		// The FULL membership, not the flattened tag view. A bare type member
		// carries its own type and owns whatever that type owns; reading the tag
		// view here made such a member invisible and its contents leaked
		// (RV2-DEBT-233).
		cases, _, err := e.unionCases(id)
		if err != nil {
			// Fail CLOSED. A union whose membership cannot be established may
			// own heap, and answering "no" is how the leak happened: a union of
			// only bare members has no tag-layout entry at all.
			return true
		}
		for index := range cases {
			for _, pt := range cases[index].PayloadTypes {
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

// registerAbandonedStateDrop records that a suspend-point/scope-join state
// box may need reclaiming if a cancellation abandons it, and answers with the
// resolved TypeID the runtime names it by.
//
// The runtime turns that id back into the type's DESCRIPTOR and performs both
// halves itself: the members through drop_in_place, then the storage at the
// width and alignment the descriptor states. There is no inert/Copy case to
// gate callers on -- the box always exists, so it always needs freeing,
// whether or not any of its fields separately own heap.
//
// Only the yield that finds its task already cancelled abandons a frame this
// way, and it abandons one it has just finished packing, so the frame is the
// only owner of what is in it.
func (e *Emitter) registerAbandonedStateDrop(stateType types.TypeID) types.TypeID {
	return resolveValueType(e.types, stateType)
}

// suspensionFrameReleaseName is the entry point that reclaims a suspension
// frame the runtime is holding and will never resume.
func suspensionFrameReleaseName(id types.TypeID) string {
	return fmt.Sprintf("release.frame.type%d", id)
}

// requireSuspensionFrameRelease records that a frame type needs the entry
// point and returns its name.
func (e *Emitter) requireSuspensionFrameRelease(id types.TypeID) string {
	id = resolveValueType(e.types, id)
	if e.suspensionFrameReleases == nil {
		e.suspensionFrameReleases = make(map[types.TypeID]struct{})
	}
	e.suspensionFrameReleases[id] = struct{}{}
	return suspensionFrameReleaseName(id)
}

// emitSuspensionFrameReleaseBody emits one frame release: the storage goes
// back to the allocator and NOTHING walks what is in it.
//
// This is the release for a frame whose payload is spent. A poll takes its
// locals OUT of the payload on entry and leaves those bytes behind, so from
// then on the frame holds a bitwise duplicate of values those locals own; a
// walk would free a string, a task handle or a channel a second time. The
// terminator that ends a poll by cancellation reclaims its frame this way, and
// so does the poll's own release of a completed frame.
//
// The frame a YIELD abandons is the other case and takes the full release:
// there the packing store has just run, so the frame is the only owner of what
// it holds and walking is the only thing that reclaims it. Which of the two a
// frame is, is known at the site that gives it up rather than by the frame,
// which is why nothing here has to ask.
func (e *Emitter) emitSuspensionFrameReleaseBody(id types.TypeID) error {
	facts, err := e.layoutOf(id)
	if err != nil {
		return err
	}
	align := facts.Align
	if align == 0 {
		align = 1
	}
	fmt.Fprintf(&e.buf, "define void @%s(ptr %%p) {\n", suspensionFrameReleaseName(id))
	fmt.Fprintf(&e.buf, "entry:\n")
	fmt.Fprintf(&e.buf, "  %%isnull = icmp eq ptr %%p, null\n")
	fmt.Fprintf(&e.buf, "  br i1 %%isnull, label %%ret, label %%body\n")
	fmt.Fprintf(&e.buf, "body:\n")
	fmt.Fprintf(&e.buf, "  call void @rt_free(ptr %%p, i64 %d, i64 %d)\n",
		transportAllocationSize(facts.Size), align)
	fmt.Fprintf(&e.buf, "  br label %%ret\n")
	fmt.Fprintf(&e.buf, "ret:\n")
	fmt.Fprintf(&e.buf, "  ret void\n}\n\n")
	return nil
}

// registerCrossingDropResult answers with the resolved TypeID a crossing names
// its result by. The runtime turns that number back into the type's descriptor
// -- there is no dispatch table between the two any more -- so all this does is
// resolve, and it stays a named step because the CALL SITES read better for
// saying which id they mean.
func (e *Emitter) registerCrossingDropResult(resultType types.TypeID) types.TypeID {
	return resolveValueType(e.types, resultType)
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

// emitDropHandle releases a value whose whole representation is the word
// already loaded as `val`: a counted scalar, a channel handle, a string, a
// dynamic array.
//
// A value composite is not one of these and never reaches here. Its bytes are
// not a word, so there is nothing to have loaded; it is dropped through
// emitDropStorage, which is given where the bytes are.
func (e *Emitter) emitDropHandle(val string, ty types.TypeID) {
	ty = resolveValueType(e.types, ty)
	if e.types.IsRefCountedScalar(ty) {
		// The container is giving back the reference it held. Whether the block
		// dies here depends on who else still points at it.
		fmt.Fprintf(&e.buf, "  call void @rt_bigfloat_release(ptr %s)\n", val)
		return
	}
	if e.types.IsRefCountedHandle(ty) {
		// The same act on the runtime's count: this holder is done with the
		// channel, and the object is destroyed — every payload still in its
		// ring dropped — only when nothing else names it (RUNTIME_V2
		// section 7). Not `close`: a channel other holders still send on is
		// left exactly as it was.
		fmt.Fprintf(&e.buf, "  call void @rt_channel_handle_drop(ptr %s)\n", val)
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
	if _, _, isMap := e.types.MapInfo(ty); isMap {
		// The map destroys its own keys and values through its own
		// descriptors, so this end of the drop names no element type.
		fmt.Fprintf(&e.buf, "  call void @rt_map_free(ptr %s)\n", val)
		return
	}
	if _, isRange := rangeElemType(e.types, ty); isRange {
		fmt.Fprintf(&e.buf, "  call void @rt_range_free(ptr %s)\n", val)
		return
	}
	if isTaskType(e.types, ty) {
		// The container gives back the handle's reference. Nothing is
		// cancelled: the task finishes on its own, and the last reference on
		// a finished task is what frees it and the result nobody took.
		fmt.Fprintf(&e.buf, "  call void @rt_task_handle_drop(ptr %s)\n", val)
	}
}

// emitDropStorage releases what the value living at `ptr` owns, leaving the
// storage itself alone: it belongs to whoever declared it, and this drop is not
// the end of its life.
func (e *Emitter) emitDropStorage(ptr string, ty types.TypeID) {
	ty = resolveValueType(e.types, ty)
	if e.hasInlineStorage(ty) && e.typeOwnsHeap(ty) {
		fmt.Fprintf(&e.buf, "  call void @%s(ptr %s)\n", e.requireDropGlue(ty), ptr)
	}
}

// emitDropAt releases the value of type `ty` at `ptr`, whichever shape it has:
// storage is dropped in place, a handle is read out of its slot first.
func (e *Emitter) emitDropAt(g *glueTmp, ptr string, ty types.TypeID, align uint64) {
	if e.hasInlineStorage(ty) {
		e.emitDropStorage(ptr, ty)
		return
	}
	word := g.next()
	fmt.Fprintf(&e.buf, "  %s = load ptr, ptr %s, align %d\n", word, ptr, align)
	e.emitDropHandle(word, ty)
}

// emitDropHandleRootAt reads the handle word out of the slot at `ptr` and
// releases it — and does neither when there is nothing to release.
//
// The load and the decision to release live in ONE place on purpose. Loading
// first and asking afterwards is not a wasted instruction, it is a wrong one:
// the load is typed `ptr`, so it reads a machine word out of a slot whose type
// may be narrower than a word. A `bool` root is one byte, and reading eight from
// its slot reads seven bytes that belong to whatever is laid out next.
//
// The predicate is typeOwnsHeap, which is the same question emitMemberDropAt
// asks before dropping a MEMBER, so a handle at a root and the same handle one
// level down are decided by one rule. (emitMemberDropAt also asks
// fieldDropIsExclusive; at a root it adds nothing, because every non-composite
// family typeOwnsHeap admits — string, dynamic array, counted scalar — already
// answers yes to it.)
//
// The load cannot move into emitDropHandle instead, which is where it would
// otherwise belong: that function is given a WORD, and two of its callers have
// no slot to load it from — a result the runtime holds arrives as the word
// itself.
func (e *Emitter) emitDropHandleRootAt(g *glueTmp, ptr string, ty types.TypeID, align uint64) {
	if !e.typeOwnsHeap(ty) {
		return
	}
	word := g.next()
	fmt.Fprintf(&e.buf, "  %s = load ptr, ptr %s, align %d\n", word, ptr, align)
	e.emitDropHandle(word, ty)
}

// emitDropGlue emits every needed glue function, processing to a
// fixpoint (a glue body can require more glue).
//
// All four kinds are drained in ONE fixpoint because they ask for each other in
// both directions: dropping a value the runtime is holding reaches for the
// release entry point of its type, and a release entry point reaches back for
// the ordinary drop glue of the same type. Draining them in separate passes
// emits whichever ran second and silently skips whatever it asked of the first
// — leaving a call to a function the module never defines.
func (e *Emitter) emitDropGlue() error {
	done := make(map[types.TypeID]struct{})
	doneElem := make(map[types.TypeID]struct{})
	doneFrame := make(map[types.TypeID]struct{})
	for {
		progressed := false
		for _, id := range takePendingGlue(e.dropGlueNeeded, done) {
			if err := e.emitDropGlueBody(id); err != nil {
				return err
			}
			progressed = true
		}
		for _, id := range takePendingGlue(e.dropElemGlueNeeded, doneElem) {
			e.emitDropElemGlueBody(id)
			progressed = true
		}
		for _, id := range takePendingGlue(e.suspensionFrameReleases, doneFrame) {
			if err := e.emitSuspensionFrameReleaseBody(id); err != nil {
				return err
			}
			progressed = true
		}
		if !progressed {
			break
		}
	}
	return nil
}

// takePendingGlue is the not-yet-emitted part of a requested glue set, in type
// order, marked emitted as it is taken.
//
// The order matters beyond tidiness. A map's iteration order is deliberately
// unspecified, so walking one of these sets directly makes two compilations of
// one unchanged program place the same glue bodies at different points in the
// module. Output that differs run to run cannot be compared: a reader diffing
// two builds cannot tell an ordering wobble from a change in what the compiler
// emits, which is exactly the question a representation change has to answer.
func takePendingGlue(needed, done map[types.TypeID]struct{}) []types.TypeID {
	pending := make([]types.TypeID, 0, len(needed))
	for id := range needed {
		if _, already := done[id]; already {
			continue
		}
		done[id] = struct{}{}
		pending = append(pending, id)
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i] < pending[j] })
	return pending
}

// emitDropElemGlueBody emits `@drop_elem.typeN(ptr %slot)`: drop one element of
// a dynamic array's buffer (rt_array_free_elems calls this once per slot).
//
// The slot IS the element now when the element lives inline, so the drop goes
// straight through it. Only a handle element still has a word to read out.
func (e *Emitter) emitDropElemGlueBody(id types.TypeID) {
	fmt.Fprintf(&e.buf, "define void @%s(ptr %%slot) {\n", dropElemGlueName(id))
	fmt.Fprintf(&e.buf, "entry:\n")
	g := &glueTmp{}
	align, err := e.arrayElemSlotAlign(id)
	if err != nil {
		align = 1
	}
	e.emitDropAt(g, "%slot", id, align)
	fmt.Fprintf(&e.buf, "  ret void\n}\n")
}

// arrayElemSlotAlign is the alignment one element slot is ASSUMED to guarantee
// inside `@drop_elem.typeN(ptr %slot)`.
//
// OPAQUE BASE: the glue's only input is a bare pointer, and it is reached for a
// fixed array's elements as well as a dynamic one's — see
// opaqueBaseElemStrideAlign.
func (e *Emitter) arrayElemSlotAlign(elem types.TypeID) (uint64, error) {
	_, align, err := e.opaqueBaseElemStrideAlign(elem)
	return align, err
}

// emitDropGlueBody emits `@drop.typeN(ptr %p)`: release everything the value
// whose storage is at %p owns, and leave the storage alone.
//
// The `rt_free` that used to close this body is gone with the box it freed. A
// composite's storage is now a local's slot, a field of an enclosing composite
// or an element of an array, and freeing any of those would be freeing memory
// this value never owned. Null-safe, because the runtime's abandon paths reach
// the same glue for a transport allocation that may not have been filled.
//
// %p is an ADDRESS in every shape, which is what the default arm below turns
// on: a composite's bytes are at %p, and a handle's WORD is at %p. That is the
// same contract emitDropAt states for a member, and it is why the arm loads
// before it releases — calling a leaf helper on %p itself would hand the
// allocator the address of the slot instead of the block the slot names.
func (e *Emitter) emitDropGlueBody(id types.TypeID) error {
	id = resolveValueType(e.types, id)
	layoutInfo, err := e.layoutOf(id)
	if err != nil {
		return err
	}
	align := layoutInfo.Align
	if align == 0 {
		align = 1
	}
	fmt.Fprintf(&e.buf, "define void @%s(ptr %%p) {\n", dropGlueName(id))
	fmt.Fprintf(&e.buf, "entry:\n")
	fmt.Fprintf(&e.buf, "  %%isnull = icmp eq ptr %%p, null\n")
	fmt.Fprintf(&e.buf, "  br i1 %%isnull, label %%ret, label %%body\n")
	fmt.Fprintf(&e.buf, "body:\n")

	g := &glueTmp{}
	if !e.types.IsValueComposite(id) {
		// The default arm. A root that is not laid out inline is a HANDLE — a
		// string, a dynamic array, a counted scalar — and its whole
		// representation is the word in the slot at %p. The word is read out
		// and released exactly the way a handle MEMBER of a composite already
		// is (emitMemberDropAt), which is the same act one level up.
		//
		// Without this arm such a root got `entry -> ret void`: a body that
		// satisfies every "droppable implies a drop function exists" check
		// there is, and reclaims nothing. It is an arm here and not a guard at
		// a binding site because a binding site fixes the one caller it is
		// written for, while the next type to arrive arrives through whichever
		// caller nobody thought about.
		//
		// The selector is IsValueComposite and NOT the type's kind, because the
		// container handles — `Array<T>`, `Map<K, V>` and the opaque runtime
		// resources — are nominal STRUCTS. Asking the kind sent every one of
		// them into the struct arm, where walking their (absent) fields emitted
		// nothing at all; a dynamic array root is precisely the case that
		// exposed it.
		//
		// What the arm releases is exactly what typeOwnsHeap counts as owned,
		// which is what keeps the walk and the emitted body from parting: a far
		// handle's lease is deliberately not returned here, because the
		// structural walk does not count it as heap this glue owns.
		e.emitDropHandleRootAt(g, "%p", id, align)
	} else if elem, length, ok := arrayFixedInfo(e.types, id); ok {
		e.emitFixedArrayElemDrops(g, elem, "%p", align, int(length))
	} else if tt, ok := e.types.Lookup(id); ok {
		switch tt.Kind {
		case types.KindStruct:
			fields := e.types.StructFields(id)
			fieldOffsets, err := requireAggregateFieldOffsets(e.types, &layoutInfo, len(fields), "struct", id)
			if err != nil {
				return err
			}
			for i, f := range fields {
				e.emitMemberDropAt(g, f.Type, "%p", align, fieldOffsets[i])
			}
		case types.KindTuple:
			if info, ok := e.types.TupleInfo(id); ok && info != nil {
				fieldOffsets, err := requireAggregateFieldOffsets(e.types, &layoutInfo, len(info.Elems), "tuple", id)
				if err != nil {
					return err
				}
				for i, el := range info.Elems {
					e.emitMemberDropAt(g, el, "%p", align, fieldOffsets[i])
				}
			}
		case types.KindUnion:
			if err := e.emitUnionPayloadDrops(g, id, &layoutInfo, align); err != nil {
				return err
			}
		}
	}

	fmt.Fprintf(&e.buf, "  br label %%ret\n")
	fmt.Fprintf(&e.buf, "ret:\n")
	fmt.Fprintf(&e.buf, "  ret void\n}\n")
	return nil
}

func requireAggregateFieldOffsets(typesIn *types.Interner, facts *layout.PhysicalFacts, want int, kind string, id types.TypeID) ([]uint64, error) {
	offsets := facts.FieldOffsets()
	if len(offsets) != want {
		return nil, fmt.Errorf("finalized %s layout for %s has %d field offsets, want %d", kind, types.Label(typesIn, id), len(offsets), want)
	}
	return offsets, nil
}

// emitUnionPayloadDrops reads the tag and drops the active variant's owning
// payload. An arm with nothing to drop falls straight through to the join.
//
// Only the ACTIVE arm is walked, which is the point: the inactive arms' bytes
// were never written, and reading them as payloads would release whatever
// happened to be there.
func (e *Emitter) emitUnionPayloadDrops(g *glueTmp, id types.TypeID, facts *layout.PhysicalFacts, baseAlign uint64) error {
	// The FULL membership, in the enumeration the layout is indexed by. The
	// flattened tag view cannot serve here for two reasons: a bare member has no
	// entry in it at all, so whatever it owns was never dropped, and its index
	// is not the physical one, so feeding it to facts.UnionCase either selects
	// the wrong arm or runs off the end.
	cases, _, err := e.unionCases(id)
	if err != nil {
		return err
	}
	// Collect cases with droppable payloads.
	type dropCase struct {
		idx           int
		payloadOffset uint64
		offsets       []uint64
		payloadTys    []types.TypeID
	}
	var droppable []dropCase
	for index := range cases {
		c := &cases[index]
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
		// The case's own index, which is also the discriminant the writer
		// stored — the two are the same number by construction now.
		ci := c.PhysicalCaseIndex
		caseLayout, ok := facts.UnionCase(ci)
		if !ok {
			return fmt.Errorf("missing finalized union case %d for type#%d", ci, id)
		}
		offs := caseLayout.FieldOffsets()
		if len(offs) != len(c.PayloadTypes) {
			return fmt.Errorf("finalized union case %d for type#%d has %d payload offsets, want %d", ci, id, len(offs), len(c.PayloadTypes))
		}
		droppable = append(droppable, dropCase{idx: ci, payloadOffset: caseLayout.PayloadOffset, offsets: offs, payloadTys: c.PayloadTypes})
	}
	if len(droppable) == 0 {
		return nil
	}
	tag := g.next()
	fmt.Fprintf(&e.buf, "  %s = load i32, ptr %%p, align %d\n", tag, memberAccessAlign(baseAlign, 0))
	// Fix the block-id ONCE: field drops below call g.next(), which would
	// otherwise drift the label names apart from the switch targets.
	uid := g.n
	join := fmt.Sprintf("u%d_join", uid)
	fmt.Fprintf(&e.buf, "  switch i32 %s, label %%%s [", tag, join)
	for _, dc := range droppable {
		fmt.Fprintf(&e.buf, " i32 %d, label %%u%d_c%d", dc.idx, uid, dc.idx)
	}
	fmt.Fprintf(&e.buf, " ]\n")
	for _, dc := range droppable {
		fmt.Fprintf(&e.buf, "u%d_c%d:\n", uid, dc.idx)
		for i, pt := range dc.payloadTys {
			e.emitMemberDropAt(g, pt, "%p", baseAlign, dc.payloadOffset+dc.offsets[i])
		}
		fmt.Fprintf(&e.buf, "  br label %%%s\n", join)
	}
	fmt.Fprintf(&e.buf, "%s:\n", join)
	return nil
}

// fieldDropIsExclusive reports whether freeing this member as part of its
// container's drop is safe — that is, whether the container is the member's
// only holder.
//
// It exists because a COPY composite's members are, by definition, not
// exclusively held: a type is Copy only when its members are, and duplicating
// the composite duplicates them by sharing. The clone glue reflects that — it
// gives the fresh box its own claim on a reference-counted scalar and its own
// box for a nested composite, and SHARES anything else, because there is
// nothing else it could safely do. A drop that then freed those shared members
// would break the invariant the two glues exist to keep: clone and drop must
// walk the same members, or a copy outlives the thing it points at.
//
// `@copy type Semaphore = { permits: Channel<nothing> }` is the shape that
// found this, back when the clone SHARED the channel word: two semaphores
// named one channel with one reference between them, and freeing it once per
// semaphore was a use-after-free in the second one. The clone retains the
// channel now (emitLeafCloneAt), so each semaphore holds a reference of its
// own and the drop of each gives back exactly that one — the channel handle
// joined the families below the day its retain and its release both existed,
// and not a day earlier, because the two halves are one invariant.
func (e *Emitter) fieldDropIsExclusive(fieldType types.TypeID) bool {
	if e == nil || e.types == nil {
		return true
	}
	id := resolveValueType(e.types, fieldType)
	if !e.types.IsCopy(id) {
		// Move-only: the container is the sole holder and must reclaim it.
		return true
	}
	// The Copy families the clone DOES duplicate, so each copy owns one: a
	// counted scalar and a channel handle take a reference, a value composite
	// gets bytes of its own.
	return e.types.IsRefCounted(id) || e.types.IsValueComposite(id)
}
