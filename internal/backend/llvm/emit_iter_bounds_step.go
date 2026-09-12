package llvm

import (
	"fmt"

	"surge/internal/symbols"
	"surge/internal/types"
)

// The bounds-descriptor arm of a range step, and the arithmetic roster it picks
// from. It lives beside emit_iter.go rather than in it because the two shapes a
// Range object can be are two different subjects: the file next door is about
// the OBJECT — how it is duplicated, sized and reclaimed, and how a cursor over
// an array is built — and this one is about walking a pair of bounds with the
// arbitrary-precision runtime.

// boundsArith is the roster of runtime entry points one bound kind is walked
// with. The three kinds have identical signatures, so the arm below is one body
// with the names swapped, and adding a kind is adding a row rather than a
// branch.
//
// `release` is what a step gives back. It reads as an odd member of an
// arithmetic roster until you notice that every other member ALLOCATES: `one`
// mints a block per step and `add` mints the next bound, and on the integer
// kinds both answers are usually fixnum-tagged words with no block behind them,
// which is why the missing release stayed invisible there and leaked a block
// per iteration for a `float`, whose every value is a block.
type boundsArith struct {
	cmp     string
	add     string
	fromI64 string
	release string
}

// boundsArithFor picks the roster by the range's STATIC element type. Nothing
// here reads the object's bound byte: that byte exists for readers who hold
// only a `void*` — the C reclamation and the relinquishing walk — and this
// emitter has the type in hand. The two must agree, and they are written from
// the same three predicates so they cannot disagree about a kind.
//
// A `Range<float>` used to fall to the int roster by default and hand
// rt_bigint_cmp a SurgeBigFloat, which read the float's mantissa words as a
// length and dereferenced them (RV2-DEBT-357). It compiled, so nothing but a
// run said so.
func boundsArithFor(typesIn *types.Interner, elemType types.TypeID) boundsArith {
	switch {
	case isBigUintType(typesIn, elemType):
		return boundsArith{"rt_biguint_cmp", "rt_biguint_add", "rt_biguint_from_u64", "rt_biguint_release"}
	case isBigFloatType(typesIn, elemType):
		return boundsArith{"rt_bigfloat_cmp", "rt_bigfloat_add", "rt_bigfloat_from_i64", "rt_bigfloat_release"}
	default:
		return boundsArith{"rt_bigint_cmp", "rt_bigint_add", "rt_bigint_from_i64", "rt_bigint_release"}
	}
}

// rangeBoundKindFor is the byte the constructor writes into the object for the
// same element type, and it sits here so that the two answers are one screen
// apart: the roster above is what THIS compilation walks the bounds with, and
// the byte is what a later reader holding only a `void*` — the C reclamation,
// the relinquishing walk — walks them with. A tree where they disagreed would
// release a bound through one layout and read it through another.
func rangeBoundKindFor(typesIn *types.Interner, elemType types.TypeID) int {
	switch {
	case isBigUintType(typesIn, elemType):
		return rangeBoundUint
	case isBigFloatType(typesIn, elemType):
		return rangeBoundFloat
	default:
		return rangeBoundInt
	}
}

// emitRangeBoundsStep emits the bounds-descriptor arm of a range step: it
// compares the current bound against the end (honoring inclusive), and on a hit
// yields the current value and advances the bound by one through the roster
// above.
//
// A range with no start begins at zero and records that it now has one, and a
// range with no end never runs out — both are what the VM's
// rangeDescriptorNextValue does, and the second is why the end comparison sits
// behind a branch rather than a select: the comparison helper would be handed a
// null bound on a range that never had one.
//
// WHO OWNS WHAT. The start slot owns the block it names, and this arm keeps
// that true across the step. The defaulted zero is minted only on the arm that
// stores it, so a range that had a start never allocates one to throw away, and
// the `one` the step added is released, because nothing else ever names it —
// both used to be dropped on the floor, which cost a block per step and stayed
// invisible only because the integer kinds answer both with a fixnum-tagged
// word that has no block behind it.
//
// The stepped-past value is NOT released here. It is handed to the `Some` the
// step answers with — the tag constructor STORES its payload rather than
// retaining it, so this is a transfer, and a release here would free a block
// the loop is about to read. The generated pattern binding owns the transferred
// scalar and MIR registers its lexical drop; this arm must not retain it again.
func (fe *funcEmitter) emitRangeBoundsStep(rangePtr string, elemType, optType types.TypeID, someIndex int, payloadType types.TypeID, resPtr, contBB string) error {
	arith := boundsArithFor(fe.emitter.types, elemType)

	curPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", curPtr, rangePtr, rangeStartOff)
	hasStartPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", hasStartPtr, rangePtr, rangeHasStartOff)
	hasStart := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", hasStart, hasStartPtr)
	hasStartB := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ne i8 %s, 0\n", hasStartB, hasStart)

	// Defaulting the start turns an open-ended range into a walked one, exactly
	// as the VM does it. It is a branch rather than a select over a zero minted
	// up front because minting is an allocation on the float kind: a select
	// would allocate a zero on every step of every range and drop it unnamed.
	defaultBB := fe.nextInlineBlock()
	startedBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", hasStartB, startedBB, defaultBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", defaultBB)
	zero := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @%s(i64 0)\n", zero, arith.fromI64)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", zero, curPtr)
	fmt.Fprintf(&fe.emitter.buf, "  store i8 1, ptr %s\n", hasStartPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", startedBB)

	// The slot is the single source of truth for where the walk has reached, on
	// both arms above, so the current bound is read back out of it.
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", startedBB)
	cur := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", cur, curPtr)

	hasEndPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", hasEndPtr, rangePtr, rangeHasEndOff)
	hasEnd := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", hasEnd, hasEndPtr)
	hasEndB := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ne i8 %s, 0\n", hasEndB, hasEnd)

	boundedBB := fe.nextInlineBlock()
	yieldBB := fe.nextInlineBlock()
	doneBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", hasEndB, boundedBB, yieldBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", boundedBB)
	endPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", endPtr, rangePtr, rangeEndOff)
	end := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", end, endPtr)
	inclPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", inclPtr, rangePtr, rangeInclusiveOff)
	incl := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", incl, inclPtr)
	cmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i32 @%s(ptr %s, ptr %s)\n", cmp, arith.cmp, cur, end)
	inclB := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ne i8 %s, 0\n", inclB, incl)
	leCmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sle i32 %s, 0\n", leCmp, cmp)
	ltCmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i32 %s, 0\n", ltCmp, cmp)
	has := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = select i1 %s, i1 %s, i1 %s\n", has, inclB, leCmp, ltCmp)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", has, yieldBB, doneBB)

	// An exhausted range keeps answering nothing: the start it stopped at is
	// still past the end, so asking again lands here again. The slot still owns
	// that bound, and rt_range_free is what gives it back.
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", doneBB)
	nothingVal, err := fe.emitTagValue(optType, "nothing", symbols.NoSymbolID, nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", nothingVal, resPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", yieldBB)
	one := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @%s(i64 1)\n", one, arith.fromI64)
	nextCur := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @%s(ptr %s, ptr %s)\n", nextCur, arith.add, cur, one)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", nextCur, curPtr)
	if isBigFloatType(fe.emitter.types, elemType) {
		fmt.Fprintf(&fe.emitter.buf, "  call void @%s(ptr %s)\n", arith.release, one)
	}
	someVal, err := fe.emitTagValueSinglePayload(optType, someIndex, payloadType, cur, handleType, elemType)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", someVal, resPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)
	return nil
}
