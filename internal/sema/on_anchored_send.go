package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

// ownStripped returns what a payload's `own` layers wrap. `own` is a MOVE
// annotation on a value -- it says who releases the thing and when the writer's
// binding dies -- and it never says the thing was copied out of where it lived.
// So `own &T` still names a place, and any question about the SHAPE of a
// payload has to be asked underneath the `own`.
func (tc *typeChecker) ownStripped(id types.TypeID) types.TypeID {
	if tc == nil || tc.types == nil || id == types.NoTypeID {
		return id
	}
	const maxOwnLayers = 8
	for range maxOwnLayers {
		resolved := tc.resolveAlias(id)
		tt, ok := tc.types.Lookup(resolved)
		if !ok || tt.Kind != types.KindOwn {
			return resolved
		}
		id = tt.Elem
	}
	return tc.resolveAlias(id)
}

// checkAnchoredSendPayloadIsAValue holds an anchored `send` to a payload that
// is a VALUE. It reports false, with the refusal emitted, for a payload that
// names a place -- with or without an `own` in front of it.
//
// Reading an element in place yields a reference and not a value: `xs[0]` where
// `xs: int[]` has type `&int`. The PLAIN CROSSING standing next to this sink is
// where that ends -- `on dst { ret xs[0]; }` is refused with "cannot assign
// TaskResult<&int> to TaskResult<int>" -- and this sink has to answer the same
// way, for a reason of its own on top of the type system's: a reference is an
// address in THIS shard's memory, the ring keeps the bits it is handed, and the
// shard that takes the value out reads those bits as an element.
//
// `own` DOES NOT CHANGE THAT, and the question is asked underneath it because
// the plain crossing answers the `own` form the same way: `on dst { ret own
// xs[0]; }` is refused "cannot assign TaskResult<own &int> to TaskResult<int>",
// measured for `int` and for a `@copy` struct alike, on this tree and at
// `63ecd58b`. Asking only the surface type left `own` a one-token bypass of
// this whole rule.
//
// WHAT THIS SINK ANSWERING LIKE THE CROSSING DOES NOT MEAN. It does not mean
// the reference is out of the language, and the claim that it was has been
// withdrawn: the far SELECT's send arm still takes `own <place>` and delivers
// it wrong. `select { ch.send(own ps[0]) => 1; stop.recv() => 2; }` over a far
// `Channel<Pair>`, `Pair` a `@copy @shard_movable` pair of `int`s, compiles and
// answers 11 where the two fields sum to 33 -- and 0 where they sum to 22 --
// with exit 0 and no diagnostic, at SURGE_SHARDS/THREADS 2 and at 8, on this
// tree and at `63ecd58b` alike; the same program with the element bound out
// first answers 33 at both widths. That arm refuses the bare `ps[0]` for its
// borrow (SEM3105), but it asks the payload's SURFACE type, which `own &Pair`
// is not; and the whole-binding rule that does refuse the `string[]` spelling
// (SEM3141) returns early when the element is Copy. So `own` walks past both,
// exactly as it walked past this sink. That is a second sink with payload rules
// of its own, it predates the capture gate this lane widened, and it is
// recorded rather than closed here (RV2-DEBT-351).
//
// Measured at SURGE_SHARDS/THREADS 2 and at 8, each on the last tree that still
// built the program. WITHOUT `own`, before this refusal existed: `ch.send(xs[0])`
// over a far `Channel<int>` with a captured `int[]` printed a DIFFERENT 70-digit
// negative number on every run -- the address read as an arbitrary-precision
// integer -- instead of 11, and the `@shard_movable` wrapper's `ch.send(b.a[0])`
// printed the same garbage at `63ecd58b`, where its capture was already accepted.
// WITH `own`, before this question was asked underneath it: `ch.send(own xs[0])`
// over a far `Channel<string>` died with "free(): double free detected in tcache
// 2"; its `Pair[]` twin answered 11 where the two fields sum to 33, a silent
// wrong answer with exit 0 at both widths; over a far `Channel<int>` it printed
// 11, and it is refused here all the same, because the plain crossing refuses
// that very program and a sink more permissive than the type system is the
// defect. Sending the VALUE instead -- `ch.send(xs[0] + 0)`, or a name bound
// before the block -- printed 11 at both widths, which is what says the
// reference is the defect and not the sink.
//
// The question that follows would not stop it. typesAssignable DEREFS a
// reference to a Copy element on its own, so `&int` and `own &int` both satisfy
// `int` outright. So the payload's own shape is asked here, first, of every
// element type alike.
//
// The refusal reuses the plain crossing's code so a reader who has met this
// once has met it everywhere; what it adds is a message about the reference
// rather than about the channel.
func (tc *typeChecker) checkAnchoredSendPayloadIsAValue(argType, element types.TypeID, span source.Span) bool {
	if tc == nil || argType == types.NoTypeID {
		return true
	}
	stripped := tc.ownStripped(argType)
	if !tc.isReferenceType(stripped) {
		return true
	}
	label := tc.typeLabel(element)
	moved := ""
	if stripped != tc.resolveAlias(argType) {
		moved = "; `own` in front of a place moves nothing out of the buffer, and a plain " +
			"crossing refuses `ret own xs[0]` for this same reference"
	}
	if b := diag.ReportError(tc.reporter, diag.SemaTypeMismatch, span,
		fmt.Sprintf("anchored `send` takes a value of `%s`, and `%s` is a borrowed read: "+
			"taking an element in place -- `xs[0]`, `m[k]` -- names a place in this shard's "+
			"memory instead of copying the element out, and the ring keeps the bits it is "+
			"handed, so the shard that receives would read this shard's address as the `%s` "+
			"it expects%s",
			label, tc.typeLabel(argType), label, moved)); b != nil {
		b.WithHelp(span, tc.anchoredSendValueHelp(element, label))
		b.Emit()
	}
	return false
}

// anchoredSendValueHelp names the way out of the refusal above for the element
// family this send has. Three families, because they take three different ways
// out and a diagnostic that offers the wrong one sends its reader straight to a
// second refusal.
//
// The branches ask exactly the questions the LATER rules ask, so the advice and
// the next gate cannot drift apart:
//
//  1. Not Copy: the element does not copy out of the buffer under a name at
//     all -- `let v: string = xs[0];` is itself refused for the same reference
//     -- so the way out is to send the whole array. Measured printing the right
//     answer at 2 shards and at 8 for `string[]`, for an array of a
//     `@shard_movable` struct, and for `int[][]`.
//  2. Copy, but the element may share a counted block: binding it out works,
//     and then checkAnchoredSendGivesThePayloadAway's first arm wants the
//     binding GIVEN AWAY. Offering a bare `ch.send(v)` here sent a reader
//     holding a `float` to SEM3212 one build later; `ch.send(own v)` is
//     measured printing 11 at both widths.
//  3. Copy and not counted: `let v: int = xs[0];` then `ch.send(v)`, measured
//     printing 11 at both widths.
//
// Building the value inside the block is deliberately NOT offered anywhere:
// `ch.send(xs[0] + "")` double-frees at both widths, an open row of its own,
// and a diagnostic must not send its reader there.
func (tc *typeChecker) anchoredSendValueHelp(element types.TypeID, label string) string {
	if !tc.isCopyType(element) {
		return fmt.Sprintf("a `%s` does not copy out of the buffer under a name either -- "+
			"`let v: %s = xs[0];` is refused for the same reference. Send the whole array "+
			"instead: `ch.send(own xs)`, over a far channel whose element is the array's own "+
			"type", label, label)
	}
	if tc.result != nil && tc.result.MayShareCountedBlock(element) {
		return fmt.Sprintf("bind the element to a name before the block and give that name "+
			"away: `let v: %s = xs[0];` outside the block, then `ch.send(own v)` -- the ring "+
			"takes the value's only reference, so the send has to end the binding", label)
	}
	return fmt.Sprintf("bind the element to a name before the block and send the name: "+
		"`let v: %s = xs[0];` outside the block, then `ch.send(v)`", label)
}

// checkAnchoredSendGivesThePayloadAway holds the payload of an anchored
// body's `ch.send` to the one shape that is safe when the ring takes storage
// the body would otherwise still owe a release for: `own <binding>`, a whole
// binding the body captured, given away. It reports false, with the refusal
// emitted, for any other shape.
//
// The ring takes the payload's bits and the receiving shard may hold them
// before this body ends, so the body cannot keep a reference of its own: the
// count is not atomic. And the body cannot make the payload private on the
// spot either — an anchored body has no async split, a send that parks
// re-enters the body from its first instruction, and a retain or a clone made
// there would be made again on every wake while the runtime consumed the
// first. What is left is the only shape with nothing to replay: the binding's
// own reference, made private by the caller when the capture entered the
// state, leaves with the send, and the binding is dead from then on. The
// move is recorded here directly: observeMove leaves Copy types alone, and
// a `float` is Copy at the surface.
//
// TWO ARMS ASK FOR THAT SHAPE, AND THE ORDER IS DELIBERATE.
//
//  1. The channel ELEMENT may share a counted block. The older arm, and it
//     keeps its words: the reader wrote the element type and the message names
//     it.
//
//  2. The channel's element IS a dynamic array and the PAYLOAD names a binding
//     this crossing captured as one. Asked only where arm 1 declines, so a
//     `float[]` payload — whose element is counted — is answered once, by arm 1.
//
// Arm 2 is here because the capture it guards is new: until the `on` gate
// admitted a bare `[T]` (ON-CAP-V005) no anchored send could name one. An
// array owns a header and a buffer, and the body owes them a drop from the
// moment the capture moved in (registerCrossingBodyOwnership) — so a send that
// stages the header into the ring WITHOUT ending the binding leaves two
// owners, and the ring's reclaim frees what the body's scope exit already
// freed. Measured before this arm existed: `on ch { ch.send(own xs); ret
// nothing; }` over a far `Channel<int[]>` with a captured `int[]` died with
// "free(): double free detected in tcache 2" at SURGE_SHARDS/THREADS 2 and at
// 8, and under valgrind reported 2 invalid frees, 6 invalid reads and 39 bytes
// definitely lost. The `float[]` twin of the same program was already clean,
// because a float element makes arm 1 fire — which is the whole argument for
// arm 2: the two arms notice the same storage through different questions, and
// only one of them was being asked of an array.
//
// WHAT ARM 2 DOES NOT REACH, and deliberately. It needs BOTH of its facts, and
// each one keeps out a shape that is none of its business. A payload that names
// no captured array — a literal built inside the body, or a window sliced out
// of a `@shard_movable` capture's field — reaches this sink with no walk and no
// shape rule, as it did before this gate opened; both were measured crashing at
// `63ecd58b`, where the program above did not compile at all, and they are
// RV2-DEBT-349's. And a send whose ELEMENT is not an array keeps no array of
// the capture, because there is no array in the ring for it to keep — which is
// arm 2's precondition and nothing more. It is not a licence for the payloads
// that shape lets through: `ch.send(xs[0])` over a far `Channel<int>`, and
// `ch.send(own xs[0])` with it, are both refused BEFORE this function is called,
// and refused for the reference an index read yields under either spelling
// (checkAnchoredSendPayloadIsAValue), not for the capture.
func (tc *typeChecker) checkAnchoredSendGivesThePayloadAway(valueExpr ast.ExprID, element types.TypeID, span source.Span) bool {
	if tc.result == nil || tc.builder == nil || !valueExpr.IsValid() {
		return true
	}
	counted := element != types.NoTypeID && tc.result.MayShareCountedBlock(element)
	capturedArray := !counted && tc.anchoredSendIsOfACapturedArray(valueExpr, element)
	if !counted && !capturedArray {
		return true
	}
	label := tc.typeLabel(element)
	help := "bind the value outside the block and write `ch.send(own name)`; " +
		"the binding cannot be read after the send"
	if capturedArray {
		help = "build the array you mean to send outside the block, bind it to a name, and write " +
			"`ch.send(own name)`; the binding cannot be read after the send"
	}
	refuse := func(reason string) bool {
		if b := diag.ReportError(tc.reporter, diag.SemaAnchoredSendGiveAway, span,
			fmt.Sprintf("an anchored `send` of `%s` must give a captured binding away: %s", label, reason)); b != nil {
			b.WithHelp(span, help)
			b.Emit()
		}
		return false
	}
	keptReason := "the ring takes the value's only reference and the receiving shard may " +
		"already hold it before this block ends, so the block cannot keep a reference of its own, " +
		"and it cannot take a fresh one here either (a parked send re-enters the block from the top " +
		"and would take it again)"
	if capturedArray {
		keptReason = "the ring keeps the array's header, and the buffer behind it, after this block " +
			"ends, while the block still owes its captured array a drop -- so a payload that is not " +
			"that binding itself, given away, leaves the ring holding storage this block frees"
	}
	unary, isUnary := tc.builder.Exprs.Unary(valueExpr)
	if !isUnary || unary == nil || unary.Op != ast.ExprUnaryOwn {
		return refuse(keptReason)
	}
	desc, ok := tc.resolvePlace(unary.Operand)
	if !ok || !desc.Base.IsValid() || len(desc.Segments) != 0 {
		return refuse("`own` must name a whole binding the block captured, not a field, an element, " +
			"or a value built inside the block (which a parked send would build again)")
	}
	if _, _, gone := tc.movedPlaceCovering(wholePlace(desc.Base)); gone {
		// The read just above reported the use-after-move.
		return false
	}
	tc.markBindingMoved(desc.Base, span)
	if last := len(tc.onCrossingStack) - 1; last >= 0 {
		tc.onCrossingStack[last].givenAway = append(tc.onCrossingStack[last].givenAway, desc.Base)
	}
	return true
}

// anchoredSendIsOfACapturedArray reports whether this send would hand the ring
// an ARRAY that a binding this crossing captured owns. It takes two facts, and
// needs both.
//
// The ELEMENT must be a dynamic array, because that is what the ring will keep
// after the block ends, and it is the whole hazard: two owners for one buffer,
// the ring's and the body's. A ring of `int` keeps no buffer whatever the
// payload was read out of, so there is no second owner and nothing for this
// rule to be about -- it would be answering a question its own message cannot
// state ("an anchored `send` of `int` ... the ring keeps the array's header").
//
// That half is load-bearing, and here is the program that says so rather than
// an argument that it must be. `@shard_movable type Pair = { a: int, b: int }`,
// a captured `Pair[]`, and `on ch { ch.send(xs[0].a); ret nothing; }` over a far
// `Channel<int>`: the payload is a VALUE read out of the buffer, its place
// resolves to the captured array, and the ring keeps an `int`. It prints 11 at
// SURGE_SHARDS/THREADS 2 and at 8, and with the element question deleted this
// rule refuses it.
//
// This half is a precondition, NOT a judgement that the payloads it lets past
// are safe. `on ch { ch.send(xs[0]); ret nothing; }` over a far `Channel<int>`
// with a captured `int[]` is not safe and never was: an index read yields a
// REFERENCE (`&int`), the ring keeps its bits, and the program printed a
// different 70-digit negative number on every run at SURGE_SHARDS/THREADS 2 and
// at 8. That is refused before this function is reached, on the payload's own
// shape, by checkAnchoredSendPayloadIsAValue -- which is where it belongs, since
// `ret xs[0]` and `ret own xs[0]` out of a plain crossing are both refused for
// the same reference and the same code, on this tree and at `63ecd58b`.
//
// What the precondition still costs is measured and written down rather than
// argued away, and it is smaller than it was: with the reference form refused,
// what still reaches this sink held to no shape is a payload that is a VALUE of
// an element that is neither counted nor an array but owns heap all the same --
// a `string` FIELD of a `@shard_movable` capture, `ch.send(b.name)`, which
// double-frees at 2 shards and at 8. There is no array in that program, and it
// does the same thing at `63ecd58b`, so it is not this rule's to close and not
// this lane's to have opened; DEBT.md carries it with both measurements.
//
// The PAYLOAD must name a binding the crossing captured as a dynamic array,
// asked through crossingCaptureMovesAsDynamicArray -- the same predicate
// ON-CAP-V005's acceptance and the body's drop registration already share, so
// the captures the body must drop and the payloads this rule guards are one set
// and cannot drift apart. Without this half the rule would reach payloads that
// owe nothing to a capture: a literal built inside the block, or a window out of
// a `@shard_movable` capture's field, both of which reached this sink before
// this gate opened (RV2-DEBT-349).
//
// A payload that is not a place at all -- `ch.send(total(own xs))`, whose value
// is computed from the capture and is an `int` -- resolves to no binding and is
// none of this rule's business either way: the capture is given to the callee,
// and that program runs clean.
func (tc *typeChecker) anchoredSendIsOfACapturedArray(valueExpr ast.ExprID, element types.TypeID) bool {
	if tc == nil || tc.types == nil || !valueExpr.IsValid() || element == types.NoTypeID {
		return false
	}
	if _, elementIsArray := tc.types.DynamicArrayElem(element); !elementIsArray {
		return false
	}
	desc, ok := tc.resolvePlace(valueExpr)
	if !ok || !desc.Base.IsValid() {
		return false
	}
	return tc.crossingCaptureMovesAsDynamicArray(tc.bindingType(desc.Base))
}

// refuseStoreIntoGivenAwayCapture refuses a write, inside the anchored body,
// to a binding the body's send gave away. The binding is moved, so the write
// would ordinarily revive it — but the release the body owed the capture was
// withheld when the ring took its reference, and a revived binding would hold
// a value nothing releases. It reports true, with the refusal emitted, when
// the store is refused.
func (tc *typeChecker) refuseStoreIntoGivenAwayCapture(desc placeDescriptor, span source.Span) bool {
	last := len(tc.onCrossingStack) - 1
	if last < 0 || !desc.Base.IsValid() {
		return false
	}
	for _, sym := range tc.onCrossingStack[last].givenAway {
		if sym != desc.Base {
			continue
		}
		name := tc.bindingName(sym)
		if b := diag.ReportError(tc.reporter, diag.SemaAnchoredSendGiveAway, span,
			fmt.Sprintf("'%s' was given to the channel by this block's `send` and cannot be assigned "+
				"again inside the block: the block has no release left for it", name)); b != nil {
			b.WithHelp(span, "bind the new value under another name")
			b.Emit()
		}
		return true
	}
	return false
}
