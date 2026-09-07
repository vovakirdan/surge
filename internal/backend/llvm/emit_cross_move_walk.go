package llvm

import (
	"fmt"

	"surge/internal/mir"
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
//   - a dynamic array whose element may share: rt_array_unshare_walk, handed
//     the array's slot, the element stride and the element's own walk body,
//     which the runtime calls once per slot of the buffer it owns. The walk
//     cannot address those slots itself -- they are at no fixed offset from
//     the value -- so the runtime iterates and this body says what to do at
//     each one. Nesting drains through the one worklist: `float[][]` walks
//     with the `float[]` body, which walks with the `float` body;
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

// emitInstrUnshare is the one call site of the walk: the MIR `unshare P`
// instruction the relinquishing operand emitted before a value is given up
// across a thread boundary. The lowering decided WHERE the act sits and its
// validator that every boundary was reached; this side only runs the walk on
// the place's storage, in place.
//
// A type that cannot hold a counted block emits nothing: its walk body would
// be empty, and the instruction is then the no-op it means.
//
// The refusal below is the last line of defence, not a gate. A value that may
// share a counted block no walk reaches -- a map's table, a channel's ring --
// must have been refused by sema's crossing gate, where the shape is still
// legible and the diagnostic has a code. Reaching here with one means that
// gate admitted a shape the emitter cannot make private, and the honest
// answer is a build failure naming the predicate that disagreed, never a
// silent no-op that would ship the shared block.
func (fe *funcEmitter) emitInstrUnshare(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	e := fe.emitter
	place := ins.Unshare.Place
	valueType, err := fe.droppedPlaceType(place)
	if err != nil {
		return err
	}
	if valueType == types.NoTypeID || !e.typeMayShareCountedBlock(valueType) {
		return nil
	}
	if !e.canUnshareValue(valueType) {
		return fmt.Errorf("unshare of %s (type#%d): the value may share a counted block that the walk "+
			"cannot make private (a map's table or a channel's ring has no walk); "+
			"sema.Result.MayShareCountedBlock admits the shape and the crossing gate that "+
			"reads it must refuse it -- the refusal belongs there, not in the emitter",
			types.Label(e.types, valueType), valueType)
	}
	ptr, slotTy, _, err := fe.emitPlaceStorage(place)
	if err != nil {
		return err
	}
	// The walk reads the value's own bytes through %val: a counted scalar's
	// handle word in the slot, an inline composite's storage, a dynamic
	// array's handle word, whose buffer the runtime walks from that slot. A
	// slot that holds the ADDRESS of the value instead -- a suspension frame's
	// -- is a shape no relinquishing site produces, and handing the walk the
	// slot would have it un-share the address word; it is refused rather than
	// guessed at.
	resolved := resolveValueType(e.types, valueType)
	_, isDynamicArray := e.types.DynamicArrayElem(resolved)
	if slotTy == handleType && !e.types.IsRefCountedScalar(resolved) && !e.hasInlineStorage(resolved) && !isDynamicArray {
		return fmt.Errorf("unshare of %s (type#%d): the walk reads a value's own bytes -- a counted "+
			"scalar's handle word, an inline composite's storage, or a dynamic array's handle word "+
			"whose buffer the runtime walks -- and this slot holds the value's address instead, "+
			"a shape no relinquishing site produces",
			types.Label(e.types, valueType), valueType)
	}
	fmt.Fprintf(&e.buf, "  call void @%s(ptr %s)\n", e.requireUnshareGlue(valueType), ptr)
	return nil
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
	// Aliases and `own` are looked through; a borrow or pointer is NOT, so the
	// kind switch below can answer for it. resolveValueType would strip it to
	// its pointee and report a `&float` as sharing what it only names.
	resolved := resolveAliasAndOwn(e.types, id)
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
	var walkErr error
	if err := e.walkGlueValue(g, id, &layoutInfo, align, unshareWalk{e: e, err: &walkErr}); err != nil {
		return err
	}
	if walkErr != nil {
		return walkErr
	}
	fmt.Fprintf(&e.buf, "  ret void\n}\n\n")
	return nil
}

// unshareWalk is the per-member half. It shares the enumeration with the clone
// and cross-clone walks, so a member shape one of them learns cannot go
// unvisited here.
//
// err is where a member arm that cannot finish -- an element stride the
// layout cannot give -- records the failure, because the walker's per-member
// answer is a bool. The body's emitter reads it and fails CLOSED: a body
// missing one member's walk would ship that member's blocks shared.
type unshareWalk struct {
	e   *Emitter
	err *error
}

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
	if elem, ok := e.types.DynamicArrayElem(resolved); ok {
		// A dynamic array's elements sit in a buffer the runtime owns, at no
		// offset this walk can address, so the runtime iterates: it takes the
		// SLOT holding the handle word (the convention every array helper
		// uses, never the word itself), the element stride, and the element's
		// own body, and calls that body once per slot. An element that cannot
		// share -- `int[]` today -- gets no call at all: a walk over plain
		// words would cost every crossing and make nothing private.
		if !e.typeMayShareCountedBlock(elem) {
			return false
		}
		stride, _, err := e.handleArrayElemStrideAlign(elem)
		if err != nil {
			*w.err = err
			return true
		}
		fp := g.next()
		fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds i8, ptr %%val, i64 %d\n", fp, off)
		fmt.Fprintf(&e.buf, "  call void @rt_array_unshare_walk(ptr %s, i64 %d, ptr @%s)\n",
			fp, stride, e.requireUnshareGlue(elem))
		return true
	}
	// Everything else the move carries by its bytes: the single reference the
	// value held travels with it, and the source stops owning it. A handle
	// whose payload may share and whose storage no per-element walk reaches --
	// a map's table, a channel's ring -- is refused at the caller rather than
	// silently skipped here; see canUnshareValue.
	return false
}

// canUnshareValue reports whether the walk can actually make every counted
// leaf of this type private. A relinquishing site must ask it before it emits
// a call, exactly as a duplicating site asks canDuplicateValue: answering
// "nothing to do" for a shape the walk cannot reach is how a shared block
// would travel unnoticed.
//
// A dynamic array answers for its element, because the runtime walks its
// buffer with the element's body. What is refused is a runtime handle whose
// payload may share a block and whose storage no per-element walk reaches: a
// `Map<K, float>`'s table, a `Channel<float>`'s ring, which stays on the
// creator's shard. Those shapes stay refused at sema for good, and this
// answer is their second belt; the two predicates are held in lock step by a
// labelled table (TestUnsharePredicatesAgreeWithSema).
func (e *Emitter) canUnshareValue(id types.TypeID) bool {
	return e.canUnshareValueRec(id, map[types.TypeID]struct{}{})
}

func (e *Emitter) canUnshareValueRec(id types.TypeID, seen map[types.TypeID]struct{}) bool {
	if e == nil || e.types == nil || id == types.NoTypeID {
		return true
	}
	resolved := resolveAliasAndOwn(e.types, id)
	if e.types.IsRefCountedScalar(resolved) {
		return true
	}
	if _, ok := seen[resolved]; ok {
		return true
	}
	seen[resolved] = struct{}{}
	// Before the handle arm, which would otherwise answer for the array as
	// for any handle whose payload may share.
	if elem, ok := e.types.DynamicArrayElem(resolved); ok {
		return e.canUnshareValueRec(elem, seen)
	}
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
