package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

// checkAnchoredSendGivesCountedPayloadAway holds the payload of an anchored
// body's `ch.send` to the one shape that is safe when the element may share a
// counted block: `own <binding>`, a whole binding the body captured, given
// away. It reports false, with the refusal emitted, for any other shape.
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
func (tc *typeChecker) checkAnchoredSendGivesCountedPayloadAway(valueExpr ast.ExprID, element types.TypeID, span source.Span) bool {
	if tc.result == nil || tc.builder == nil || !valueExpr.IsValid() ||
		element == types.NoTypeID || !tc.result.MayShareCountedBlock(element) {
		return true
	}
	label := tc.typeLabel(element)
	refuse := func(reason string) bool {
		if b := diag.ReportError(tc.reporter, diag.SemaAnchoredSendGiveAway, span,
			fmt.Sprintf("an anchored `send` of `%s` must give a captured binding away: %s", label, reason)); b != nil {
			b.WithHelp(span, "bind the value outside the block and write `ch.send(own name)`; "+
				"the binding cannot be read after the send")
			b.Emit()
		}
		return false
	}
	unary, isUnary := tc.builder.Exprs.Unary(valueExpr)
	if !isUnary || unary == nil || unary.Op != ast.ExprUnaryOwn {
		return refuse("the ring takes the value's only reference and the receiving shard may " +
			"already hold it before this block ends, so the block cannot keep a reference of its own, " +
			"and it cannot take a fresh one here either (a parked send re-enters the block from the top " +
			"and would take it again)")
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
