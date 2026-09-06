package llvm

import (
	"fmt"

	"surge/internal/types"
)

// The relinquishing walk: make a value's counted leaves PRIVATE before it is
// given up across a shard or thread boundary.
//
// Why the compiler and not the runtime. A reference-counted scalar is Copy, so
// `let b = a` and `P{ v: a }` RETAIN rather than duplicate, and a value can
// hold a block a sibling binding still holds. Moving that value hands the
// destination one reference while the source keeps another, and the count is
// deliberately not atomic. The runtime cannot detect the case: at a far
// select's SEND arm a Copy payload reaches it as the bare address of a live
// local with no reference taken at all, so a count of one there means "one
// holder and it is still yours", not "yours alone to give". Only the frame
// that relinquishes the value knows which references it is giving up, so the
// barrier belongs in that frame -- the owner's ruling of 2026-09-06.
//
// What the walk does per member is the smallest thing that makes the claim
// true:
//
//   - a reference-counted scalar: rt_bigfloat_unshare, which keeps the pointer
//     when the block has one holder and otherwise duplicates it and gives up
//     the reference this value held;
//   - a nested value composite: recurse, because its own members may hold one;
//   - a string, a channel handle, a plain word: nothing. A move transfers the
//     single reference the value holds and the source stops owning it, so
//     there is nothing to make private.
//
// It is emitted per RESOLVED layout, like the clone walk and unlike the cross
// applies: it reads nothing but the layout and every entry resolving to the
// same one wants the same body.

func unshareWalkName(id types.TypeID) string { return fmt.Sprintf("unshare.type%d", id) }

// requireUnshareGlue records that the resolved form of `id` needs a walk body
// and returns its name. emitUnshareGlue drains what the recursion adds.
func (e *Emitter) requireUnshareGlue(id types.TypeID) string {
	id = resolveValueType(e.types, id)
	if e.unshareGlueNeeded == nil {
		e.unshareGlueNeeded = make(map[types.TypeID]struct{})
	}
	e.unshareGlueNeeded[id] = struct{}{}
	return unshareWalkName(id)
}

// emitUnshareGlue drains the demand, emitting each body and the nested bodies
// it recurses into. Same fixpoint the clone and cross-clone glue use.
func (e *Emitter) emitUnshareGlue() error {
	done := make(map[types.TypeID]struct{})
	for {
		pending := takePendingGlue(e.unshareGlueNeeded, done)
		if len(pending) == 0 {
			return nil
		}
		for _, id := range pending {
			if err := e.emitUnshareWalkBody(id); err != nil {
				return err
			}
		}
	}
}

// typeMayShareCountedBlock reports whether a value of this type can hold, at
// any depth, a reference into a counted block that another holder on this
// shard may hold too. It is what decides whether a relinquishing site emits a
// call at all.
//
// The emitter keeps its own predicate rather than asking sema's: sema's
// ContainsRefCountedScalar answers the Copy-bits question and deliberately
// stops at unions, and the backend has no sema.Result to ask anyway. A union
// arm holding a counted scalar is exactly a case this walk must reach, so the
// two predicates are near-neighbours that must not be confused --
// sema.Result.MayShareCountedBlock is the same question on the other side, and
// the refusal it drives is what keeps a shape the emitter cannot walk from
// reaching a crossing.
func (e *Emitter) typeMayShareCountedBlock(id types.TypeID) bool {
	return e.mayShareCountedBlockRec(id, map[types.TypeID]struct{}{})
}

func (e *Emitter) mayShareCountedBlockRec(id types.TypeID, seen map[types.TypeID]struct{}) bool {
	if e == nil || e.types == nil || id == types.NoTypeID {
		return false
	}
	resolved := resolveValueType(e.types, id)
	if e.types.IsRefCountedScalar(resolved) {
		return true
	}
	if _, ok := seen[resolved]; ok {
		// A recursive shape contributes nothing through this edge; the answer
		// comes from its other members.
		return false
	}
	seen[resolved] = struct{}{}

	if payloads, ok := e.types.RuntimeHandlePayloads(resolved); ok {
		for _, payload := range payloads {
			if e.mayShareCountedBlockRec(payload, seen) {
				return true
			}
		}
		return false
	}
	if elem, _, ok := arrayFixedInfo(e.types, resolved); ok {
		return e.mayShareCountedBlockRec(elem, seen)
	}
	tt, ok := e.types.Lookup(resolved)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindReference, types.KindPointer, types.KindFar, types.KindFn:
		// Storage named but not carried: the pointee answers for itself, and
		// a far handle names another shard's storage entirely.
		return false
	case types.KindStruct:
		for _, f := range e.types.StructFields(resolved) {
			if e.mayShareCountedBlockRec(f.Type, seen) {
				return true
			}
		}
	case types.KindTuple:
		if info, ok := e.types.TupleInfo(resolved); ok && info != nil {
			for _, el := range info.Elems {
				if e.mayShareCountedBlockRec(el, seen) {
					return true
				}
			}
		}
	case types.KindUnion:
		cases, _, err := e.unionCases(resolved)
		if err != nil {
			// A union whose membership cannot be read fails CLOSED: answering
			// "nothing to make private" for a shape nobody could inspect is
			// the answer that loses a block to a race.
			return true
		}
		for _, c := range cases {
			for _, pt := range c.PayloadTypes {
				if e.mayShareCountedBlockRec(pt, seen) {
					return true
				}
			}
		}
	}
	return false
}

// emitUnshareWalkBody emits the walk for one resolved layout. It mutates the
// value in place -- there is no destination, because the value is not being
// copied anywhere: it is the caller's own storage, about to be given up.
func (e *Emitter) emitUnshareWalkBody(id types.TypeID) error {
	layoutInfo, err := e.layoutOf(id)
	if err != nil {
		return err
	}
	align := layoutInfo.Align
	if align == 0 {
		align = 1
	}
	fmt.Fprintf(&e.buf, "define void @%s(ptr %%val) {\nentry:\n", unshareWalkName(id))
	g := &glueTmp{}
	if err := e.walkGlueValue(g, id, &layoutInfo, align, unshareWalk{e: e}); err != nil {
		return err
	}
	fmt.Fprintf(&e.buf, "  ret void\n}\n\n")
	return nil
}

// unshareWalk is the per-member half. It shares the enumeration with the clone
// and cross-clone walks, so a member shape one of them learns cannot go
// unvisited here.
type unshareWalk struct{ e *Emitter }

func (unshareWalk) labelPrefix() string { return "us" }

// The value is read and written in place, so the discriminant comes from the
// one storage this walk has.
func (unshareWalk) tagStorage() string { return "%val" }

func (w unshareWalk) needsFixup(resolved types.TypeID) bool {
	return w.e.typeMayShareCountedBlock(resolved)
}

func (w unshareWalk) leafAt(g *glueTmp, resolved types.TypeID, baseAlign, off uint64) bool {
	e := w.e
	if e.types.IsRefCountedScalar(resolved) {
		// The one counted scalar today is WidthAny float. When int and uint
		// join they dispatch per kind here, because their fixnum form is a
		// tagged word with no block behind it and no count to read.
		fp := g.next()
		fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds i8, ptr %%val, i64 %d\n", fp, off)
		fv := g.next()
		fmt.Fprintf(&e.buf, "  %s = load ptr, ptr %s, align %d\n", fv, fp, memberAccessAlign(baseAlign, off))
		private := g.next()
		fmt.Fprintf(&e.buf, "  %s = call ptr @rt_bigfloat_unshare(ptr %s)\n", private, fv)
		fmt.Fprintf(&e.buf, "  store ptr %s, ptr %s, align %d\n", private, fp, memberAccessAlign(baseAlign, off))
		return true
	}
	// Everything else the move carries by its bytes: the single reference the
	// value held travels with it, and the source stops owning it. A CONTAINER
	// of counted elements is the one shape that would need more -- a runtime
	// iteration over a buffer this walk cannot address at a fixed offset -- and
	// it is refused at the caller rather than silently skipped here; see
	// canUnshareValue.
	return false
}

// canUnshareValue reports whether the walk can actually make every counted
// leaf of this type private. A relinquishing site must ask it before it emits
// a call, exactly as a duplicating site asks canDuplicateValue: answering
// "nothing to do" for a shape the walk cannot reach is how a shared block
// would travel unnoticed.
//
// The one shape it refuses is a container of counted elements. Making those
// private means walking a buffer at runtime, which is not built. Nothing
// compilable reaches it today: sema refuses to cross any value that may share
// a counted block, and `float[]` is one. It is built when step 5 lifts that
// refusal and a red row becomes possible -- RV2-DEBT-038.
func (e *Emitter) canUnshareValue(id types.TypeID) bool {
	return e.canUnshareValueRec(id, map[types.TypeID]struct{}{})
}

func (e *Emitter) canUnshareValueRec(id types.TypeID, seen map[types.TypeID]struct{}) bool {
	if e == nil || e.types == nil || id == types.NoTypeID {
		return true
	}
	resolved := resolveValueType(e.types, id)
	if e.types.IsRefCountedScalar(resolved) {
		return true
	}
	if _, ok := seen[resolved]; ok {
		return true
	}
	seen[resolved] = struct{}{}
	if payloads, ok := e.types.RuntimeHandlePayloads(resolved); ok {
		for _, payload := range payloads {
			if e.mayShareCountedBlockRec(payload, map[types.TypeID]struct{}{}) {
				return false
			}
		}
		return true
	}
	if elem, _, ok := arrayFixedInfo(e.types, resolved); ok {
		return e.canUnshareValueRec(elem, seen)
	}
	tt, ok := e.types.Lookup(resolved)
	if !ok {
		return true
	}
	switch tt.Kind {
	case types.KindStruct:
		for _, f := range e.types.StructFields(resolved) {
			if !e.canUnshareValueRec(f.Type, seen) {
				return false
			}
		}
	case types.KindTuple:
		if info, ok := e.types.TupleInfo(resolved); ok && info != nil {
			for _, el := range info.Elems {
				if !e.canUnshareValueRec(el, seen) {
					return false
				}
			}
		}
	case types.KindUnion:
		cases, _, err := e.unionCases(resolved)
		if err != nil {
			return false
		}
		for _, c := range cases {
			for _, pt := range c.PayloadTypes {
				if !e.canUnshareValueRec(pt, seen) {
					return false
				}
			}
		}
	}
	return true
}

func (w unshareWalk) compositeAt(g *glueTmp, resolved types.TypeID, baseAlign, off uint64) {
	e := w.e
	field := g.next()
	fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds i8, ptr %%val, i64 %d\n", field, off)
	fmt.Fprintf(&e.buf, "  call void @%s(ptr %s)\n", e.requireUnshareGlue(resolved), field)
}
