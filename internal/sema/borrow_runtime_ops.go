package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

func (tc *typeChecker) observeMove(expr ast.ExprID, span source.Span) {
	if !expr.IsValid() || tc.borrow == nil {
		return
	}
	// Every move consumes the moved evaluation: whoever receives the
	// value owns its drop from here.
	tc.consumeTempCandidate(expr)

	// A compare and a ternary each hand a SUBEXPRESSION's value outward, so
	// consuming one consumes that too. Neither can settle it while being
	// typed: only the context knows whether the value is consumed at all, and
	// `peek(cond ? a : b)` merely borrows it.
	tc.consumeExprValue(expr)

	// Skip move tracking for Copy types - they can be implicitly copied
	// and the original value remains valid after the "copy".
	exprType := tc.result.ExprTypes[expr]
	if tc.isCopyType(exprType) {
		return
	}
	if tc.isArrayViewExpr(expr) {
		exprID := tc.unwrapArrayViewExpr(expr)
		if tc.builder == nil {
			return
		}
		if _, ok := tc.builder.Exprs.Ident(exprID); !ok {
			return
		}
	}
	if tc.builder != nil && tc.types != nil {
		if idx, ok := tc.builder.Exprs.Index(expr); ok && idx != nil {
			container := tc.result.ExprTypes[idx.Target]
			indexType := tc.result.ExprTypes[idx.Index]
			if container != types.NoTypeID && indexType != types.NoTypeID {
				base := tc.valueType(container)
				if base == tc.types.Builtins().String {
					if payload, ok := tc.rangePayload(indexType); ok {
						intType := tc.types.Builtins().Int
						if intType != types.NoTypeID && tc.sameType(payload, intType) {
							return
						}
					}
				}
			}
		}
	}
	if tc.result != nil && tc.result.ImplicitConversions != nil {
		if conv, ok := tc.result.ImplicitConversions[expr]; ok && conv.Kind == ImplicitConversionTo {
			if tc.result.ToSymbols != nil {
				if symID := tc.result.ToSymbols[expr]; symID.IsValid() {
					if sym := tc.symbolFromID(symID); sym != nil && sym.Signature != nil && len(sym.Signature.Params) > 0 {
						paramStr := strings.TrimSpace(string(sym.Signature.Params[0]))
						if strings.HasPrefix(paramStr, "&") {
							if !tc.isReferenceType(exprType) {
								op := ast.ExprUnaryRef
								if strings.HasPrefix(paramStr, "&mut ") {
									op = ast.ExprUnaryRefMut
								} else if _, ok := tc.resolvePlace(expr); !ok {
									return
								}
								tc.handleBorrow(expr, span, op, expr)
							}
							return
						}
					}
				}
			}
		}
	}
	if tc.isSharedRefDeref(expr) {
		return
	}
	if tc.isRefReborrow(expr) {
		return
	}

	// A projection whose base is a TEMPORARY never reaches `resolvePlace` — a
	// call is not a place — so the gate below cannot see it. It is the same
	// partial move against a value identified differently: see
	// handleTemporaryProjectionMove.
	if tc.handleTemporaryProjectionMove(expr, exprType, span) {
		return
	}

	desc, ok := tc.resolvePlace(expr)
	if !ok {
		return
	}
	base := desc.Base
	direct := len(desc.Segments) == 0
	// A compare-arm guard runs before the arm commits; a failed guard
	// falls through to the NEXT arm's tag test against the same
	// scrutinee, and this fix's own deep-drop release for a later
	// no-payload/wildcard arm still expects this arm's extracted payload
	// to be intact. Moving it out from inside the guard would free it
	// early on both counts, so it is rejected here rather than tracked.
	if base.IsValid() && tc.isCompareGuardBinding(base) {
		tc.reportCompareGuardMove(base, span)
		return
	}
	// Taking a PROJECTION out of a live binding is a partial move: the place that
	// left is gone, the places beside it stay, and the binding now drops only
	// what it still holds.
	//
	// Two shapes are refused rather than tracked. A plain read is refused because
	// the marker is the only thing at the use site that says the value was
	// emptied. A path through an element is refused because the remainder cannot
	// be named — see rejectUnnameableResidual.
	//
	// The segments are read BEFORE expandPlaceDescriptor on purpose. Expansion
	// rewrites a place through the borrow its base came from, which manufactures
	// segments the source never wrote; the question here is what the PROGRAM
	// said, not where the value ultimately lives.
	// An UNRESOLVED type is not an answer, and the gate must not read it as one.
	// Whether this read takes the value or duplicates it is decided by asking
	// whether the type is Copy, and `isCopyType` answers false for a type it does
	// not know — so an unresolved read looks exactly like a move of a move-only
	// value. Member access on an ANONYMOUS record records no type here, which is
	// how a plain `let p = { x: 1 }; let a = p.x;` came to be reported as a
	// partial move of an `int` (RV2-DEBT-089). The whole-binding path below is
	// unaffected: it needs no type to know a binding went.
	if !direct && base.IsValid() && exprType != types.NoTypeID {
		// Enumerability is asked FIRST, and the order is the diagnostic. Telling
		// someone to write `own a[0]` when `own a[0]` is refused too would send
		// them to a second error for a different reason; the shape they cannot
		// write at all is the thing to say.
		if tc.rejectUnnameableResidual(desc, expr, span) {
			return
		}
		if !tc.writtenAsOwn(expr) {
			// A place that has ALREADY gone gets no advice about how to take it.
			// The read has just been reported as a use-after-move, which is the
			// real problem; adding "write `own`" on top would tell the reader to
			// take a value that is not there, and following it changes nothing.
			if expanded, _ := tc.expandPlaceDescriptor(desc); tc.placeAlreadyGone(expanded) {
				return
			}
			tc.reportPartialMoveNeedsOwn(desc, expr, span)
			return
		}
		tc.recordPartialMoveRead(expr)
	}
	desc, _ = tc.expandPlaceDescriptor(desc)
	place := tc.canonicalPlace(desc)
	if !place.IsValid() {
		return
	}
	issue := tc.borrow.MoveAllowed(place)
	evSpan := span
	if evSpan == (source.Span{}) {
		evSpan = tc.exprSpan(expr)
	}
	tc.recordBorrowEvent(&BorrowEvent{
		Kind:        BorrowEvMove,
		Place:       place,
		Span:        evSpan,
		Scope:       tc.currentScope(),
		Issue:       issue.Kind,
		IssueBorrow: issue.Borrow,
	})
	if issue.Kind != BorrowIssueNone {
		if span == (source.Span{}) {
			span = evSpan
		}
		tc.reportBorrowMove(place, span, issue)
		return
	}
	switch {
	case direct && base.IsValid():
		tc.markBindingMoved(base, evSpan)
	case base.IsValid():
		// A partial move records the PLACE, so the sibling beside it stays
		// readable and the binding's own drop can be narrowed to the remainder.
		// The expanded place is the one to record: a read asks about the same
		// expansion, so recording the unexpanded one would store a key no query
		// ever looks up.
		tc.markPlaceMoved(place, evSpan)
	}
}

// placeAlreadyGone reports whether this place, or something covering it, has
// already been given away — so a diagnostic about how to take it would be
// advice about a value that is not there.
func (tc *typeChecker) placeAlreadyGone(desc placeDescriptor) bool {
	place := tc.canonicalPlace(desc)
	if !place.IsValid() {
		return false
	}
	_, _, found := tc.movedPlaceCovering(place)
	return found
}

// writtenAsOwn reports whether the source spelled `own` on this expression.
//
// It has to be read off the SYNTAX: resolvePlace looks through `own`, because
// the place is the same either way, so by the time there is a place the marker
// is gone. Groups are transparent — `own (o.inner)` and `(own o.inner)` both
// say it.
func (tc *typeChecker) writtenAsOwn(expr ast.ExprID) bool {
	if tc.builder == nil {
		return false
	}
	cur := expr
	// Bounded rather than `for {}`: a malformed tree must not hang the checker.
	for range 64 {
		node := tc.builder.Exprs.Get(cur)
		if node == nil {
			return false
		}
		switch node.Kind {
		case ast.ExprUnary:
			unary, ok := tc.builder.Exprs.Unary(cur)
			if !ok || unary == nil {
				return false
			}
			return unary.Op == ast.ExprUnaryOwn
		case ast.ExprGroup:
			group, ok := tc.builder.Exprs.Group(cur)
			if !ok || group == nil {
				return false
			}
			cur = group.Inner
		default:
			return false
		}
	}
	return false
}

// recordPartialMoveRead flags the field read that performs a partial move, so
// the lowering can take the value instead of duplicating it.
//
// The `own` marker and any parentheses are stripped first: they carry the
// intent, but the expression the lowering will turn into a field read is the
// projection underneath, and that is the one the flag has to name.
func (tc *typeChecker) recordPartialMoveRead(expr ast.ExprID) {
	projection := tc.unwrapOwnMarker(expr)
	if !projection.IsValid() || tc.result == nil {
		return
	}
	if tc.result.PartialMoveReads == nil {
		tc.result.PartialMoveReads = make(map[ast.ExprID]struct{})
	}
	tc.result.PartialMoveReads[projection] = struct{}{}
}

// unwrapOwnMarker strips `own` and grouping parentheses to reach the projection
// the marker was written on.
func (tc *typeChecker) unwrapOwnMarker(expr ast.ExprID) ast.ExprID {
	if tc.builder == nil {
		return expr
	}
	cur := expr
	// Bounded rather than `for {}`: a malformed tree must not hang the checker.
	for range 64 {
		node := tc.builder.Exprs.Get(cur)
		if node == nil {
			return cur
		}
		switch node.Kind {
		case ast.ExprGroup:
			group, ok := tc.builder.Exprs.Group(cur)
			if !ok || group == nil || !group.Inner.IsValid() {
				return cur
			}
			cur = group.Inner
		case ast.ExprUnary:
			unary, ok := tc.builder.Exprs.Unary(cur)
			if !ok || unary == nil || unary.Op != ast.ExprUnaryOwn || !unary.Operand.IsValid() {
				return cur
			}
			cur = unary.Operand
		default:
			return cur
		}
	}
	return cur
}

// partialMoveRead reports whether this expression TAKES a place out of a live
// value, rather than reading one the container keeps owning.
//
// It is the question that decides who drops the result. A plain field read
// yields interior state its container still owns, so the binding receiving it
// must never drop — that is what markAliasedBinding records. A partial move is
// the opposite: the place has LEFT the container, the container's own drop is
// narrowed to the remainder, and the binding is a real owner whose drop nobody
// else will perform.
//
// Kept in step with observeMove's own refusals on purpose — same `own` marker,
// same Copy question, same field-only paths — because a read those two disagree
// about is either dropped twice or not at all.
func (tc *typeChecker) partialMoveRead(expr ast.ExprID) bool {
	if !expr.IsValid() || tc.result == nil || !tc.writtenAsOwn(expr) {
		return false
	}
	if tc.isCopyType(tc.result.ExprTypes[expr]) {
		return false
	}
	desc, ok := tc.resolvePlace(expr)
	if !ok || !desc.Base.IsValid() || len(desc.Segments) == 0 {
		return false
	}
	for _, seg := range desc.Segments {
		if !placeSegmentEnumerable(seg.Kind) {
			return false
		}
	}
	return true
}

// rejectUnnameableResidual refuses a partial move whose remainder cannot be
// listed, and reports whether it refused.
//
// After `own o.inner` the binding still holds its other fields, and releasing
// them at scope exit means naming them one at a time. A struct's fields come
// from its type, so that list exists. An array or tuple ELEMENT is chosen by
// position: naming the survivors of `[T; N]` costs N-1 drops at every exit, and
// for a computed index there is no static list at all. A tag payload needs the
// runtime tag to know which variant is even there.
//
// The alternative both shapes really want is a runtime drop flag per element,
// which is how Rust answers this and which this language has decided against.
// So they stay on the whole-binding drop, and the refusal says so at the point
// the move is written rather than leaving residualDropPlan to silently decline
// and emit a drop of the whole container over a field that has left.
func (tc *typeChecker) rejectUnnameableResidual(desc placeDescriptor, expr ast.ExprID, span source.Span) bool {
	for _, seg := range desc.Segments {
		if placeSegmentEnumerable(seg.Kind) {
			continue
		}
		tc.reportUnnameableResidual(desc, seg.Kind, expr, span)
		return true
	}
	return false
}

// placeSegmentEnumerable reports whether the survivors of this projection step
// can be listed. A struct field and a tuple element both name their container's
// parts statically; an array element does not, and a place behind a reference is
// not this value's to give away at all.
func placeSegmentEnumerable(kind PlaceSegmentKind) bool {
	return kind == PlaceSegmentField || kind == PlaceSegmentTupleIndex
}

func (tc *typeChecker) isSharedRefDeref(expr ast.ExprID) bool {
	if !expr.IsValid() || tc.builder == nil || tc.types == nil || tc.result == nil {
		return false
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil || node.Kind != ast.ExprUnary {
		return false
	}
	unary, ok := tc.builder.Exprs.Unary(expr)
	if !ok || unary == nil || unary.Op != ast.ExprUnaryDeref {
		return false
	}
	operandType := tc.result.ExprTypes[unary.Operand]
	if operandType == types.NoTypeID {
		return false
	}
	operandType = tc.resolveAlias(operandType)
	tt, ok := tc.types.Lookup(operandType)
	if !ok || tt.Kind != types.KindReference {
		return false
	}
	return !tt.Mutable
}

func (tc *typeChecker) isRefReborrow(expr ast.ExprID) bool {
	if !expr.IsValid() || tc.builder == nil || tc.types == nil || tc.result == nil {
		return false
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil || node.Kind != ast.ExprUnary {
		return false
	}
	unary, ok := tc.builder.Exprs.Unary(expr)
	if !ok || unary == nil {
		return false
	}
	if unary.Op != ast.ExprUnaryRef && unary.Op != ast.ExprUnaryRefMut {
		return false
	}
	innerNode := tc.builder.Exprs.Get(unary.Operand)
	if innerNode == nil || innerNode.Kind != ast.ExprUnary {
		return false
	}
	innerUnary, ok := tc.builder.Exprs.Unary(unary.Operand)
	if !ok || innerUnary == nil || innerUnary.Op != ast.ExprUnaryDeref {
		return false
	}
	operandType := tc.result.ExprTypes[innerUnary.Operand]
	if operandType == types.NoTypeID {
		return false
	}
	operandType = tc.resolveAlias(operandType)
	tt, ok := tc.types.Lookup(operandType)
	if !ok || tt.Kind != types.KindReference {
		return false
	}
	return true
}

func (tc *typeChecker) handleBorrow(exprID ast.ExprID, span source.Span, op ast.ExprUnaryOp, operand ast.ExprID) {
	if tc.borrow == nil {
		return
	}
	if op == ast.ExprUnaryRef && tc.isStringLiteralExpr(operand) {
		return
	}
	desc, ok := tc.resolvePlace(operand)
	if !ok {
		tc.report(diag.SemaBorrowNonAddressable, span, "expression is not addressable")
		return
	}
	desc, parent := tc.expandPlaceDescriptor(desc)
	place := tc.canonicalPlace(desc)
	if !place.IsValid() {
		return
	}
	scope := tc.currentScope()
	if !scope.IsValid() {
		return
	}
	kind := BorrowShared
	if op == ast.ExprUnaryRefMut {
		if !tc.ensureMutablePlace(place, span) {
			return
		}
		kind = BorrowMut
	}
	// A direct `&mut` call argument reserves instead of activating: sibling
	// arguments may still read the place until the whole list is evaluated
	// (two_phase_borrow.go). Activation runs when the call's arguments end.
	if kind == BorrowMut {
		if frame := tc.reserveTwoPhaseBorrow(exprID); frame != nil {
			bid, issue := tc.borrow.BeginBorrowReserved(exprID, span, place, scope, parent)
			tc.recordBorrowEvent(&BorrowEvent{
				Kind:        BorrowEvBorrowStart,
				Borrow:      bid,
				BorrowKind:  kind,
				Place:       place,
				Span:        span,
				Scope:       scope,
				Issue:       issue.Kind,
				IssueBorrow: issue.Borrow,
			})
			if issue.Kind != BorrowIssueNone {
				tc.reportBorrowConflict(place, span, issue, kind)
				return
			}
			frame.reserved = append(frame.reserved, bid)
			return
		}
	}
	bid, issue := tc.borrow.BeginBorrow(exprID, span, kind, place, scope, parent)
	tc.recordBorrowEvent(&BorrowEvent{
		Kind:        BorrowEvBorrowStart,
		Borrow:      bid,
		BorrowKind:  kind,
		Place:       place,
		Span:        span,
		Scope:       scope,
		Issue:       issue.Kind,
		IssueBorrow: issue.Borrow,
	})
	if issue.Kind != BorrowIssueNone {
		tc.reportBorrowConflict(place, span, issue, kind)
	}
}

func (tc *typeChecker) handleAssignment(exprID ast.ExprID, op ast.ExprBinaryOp, left, right ast.ExprID, span source.Span) {
	// Check @readonly attribute before allowing assignment
	if tc.checkReadonlyFieldWrite(left, span) {
		return // @readonly violation reported
	}

	desc, ok := tc.resolvePlace(left)
	if !ok {
		return
	}
	// Asked before any of the bookkeeping below, and before the place is
	// expanded through its borrow: a write the language does not allow has no
	// drop to record and no place to revive, and expanding it first would
	// report the referent's borrow conflicting with itself rather than naming
	// the reference that cannot carry the write.
	if tc.storesThroughSharedRef(desc) {
		tc.reportStoreThroughSharedRef(desc, span)
		return
	}
	if desc.Base.IsValid() && len(desc.Segments) == 0 {
		// The RHS is fully evaluated by now, so moved-ness decides the
		// overwritten-value drop (x = f(x) suppresses it); the store
		// then revives the binding with the new value.
		tc.recordReassignOldDrop(exprID, desc.Base)
		tc.clearBindingMoved(desc.Base)
		// Ownership of the NEW value: a projection read stays with its
		// container (the binding becomes an alias); anything else makes
		// the binding a fresh owner again.
		if tc.projectionReadAliasesItsSource(right, tc.bindingType(desc.Base)) {
			tc.markAliasedBinding(desc.Base)
		} else {
			tc.clearAliasedBinding(desc.Base)
		}
	} else if desc.Base.IsValid() {
		// Assigning INTO a place reinitializes it. A field that was given away
		// comes back, and with nothing else moved under the binding the whole
		// value is readable again.
		//
		// The store's own drop obligation for the overwritten field is NOT
		// recorded here: obligations are still binding-shaped, and a per-field
		// one has no way to reach a backend that cannot drop a projection yet.
		reviveTarget := desc
		if !tc.isWriteThroughMutRef(desc) {
			reviveTarget, _ = tc.expandPlaceDescriptor(desc)
		}
		if target := tc.canonicalPlace(reviveTarget); target.IsValid() {
			if blockedBy, blockedSpan, revived := tc.revivePlace(target); !revived {
				tc.reportAssignIntoMovedValue(target, blockedBy, span, blockedSpan)
			}
		}
	}

	// Check if this is a write through a mutable reference binding (*r = value).
	// In this case, we should NOT expand through the borrow because writing
	// through &mut is allowed - that's the whole point of exclusive borrows.
	writeThroughMutRef := tc.isWriteThroughMutRef(desc)

	if !writeThroughMutRef {
		desc, _ = tc.expandPlaceDescriptor(desc)
	}
	place := tc.canonicalPlace(desc)
	if !place.IsValid() {
		return
	}
	var issue BorrowIssue
	if tc.borrow != nil && !writeThroughMutRef {
		// Only check for mutation conflicts if not writing through a &mut reference.
		// Writes through &mut references are allowed by design.
		issue = tc.borrow.MutationAllowed(place)
		tc.recordBorrowEvent(&BorrowEvent{
			Kind:        BorrowEvWrite,
			Place:       place,
			Span:        span,
			Scope:       tc.currentScope(),
			Issue:       issue.Kind,
			IssueBorrow: issue.Borrow,
		})
		if issue.Kind != BorrowIssueNone {
			tc.reportBorrowMutation(place, span, issue)
		}
	} else if tc.borrow != nil {
		// Still record the write event for diagnostics/debugging
		tc.recordBorrowEvent(&BorrowEvent{
			Kind:  BorrowEvWrite,
			Place: place,
			Span:  span,
			Scope: tc.currentScope(),
			Note:  "write_through_mut_ref",
		})
	}
	if op == ast.ExprBinaryAssign {
		tc.observeMove(right, tc.exprSpan(right))
		if !writeThroughMutRef {
			tc.updateBindingValue(place.Base, right)
		}
		return
	}
	if tc.bindingBorrow != nil && !writeThroughMutRef {
		tc.bindingBorrow[place.Base] = NoBorrowID
	}
}

func (tc *typeChecker) handleDrop(expr ast.ExprID, span source.Span) {
	exprType := tc.typeExpr(expr)
	symID := tc.symbolForExpr(expr)
	if !symID.IsValid() {
		if tc.handleProjectionDrop(expr, span) {
			return
		}
		tc.report(diag.SemaBorrowNonAddressable, span, "drop target must be a binding")
		return
	}
	if tc.bindingBorrow == nil {
		return
	}
	bid := tc.bindingBorrow[symID]
	if bid == NoBorrowID {
		tc.recordBorrowEvent(&BorrowEvent{
			Kind:    BorrowEvDrop,
			Binding: symID,
			Span:    span,
			Scope:   tc.currentScope(),
			Note:    "drop",
		})
		// Dropping an owned non-copy value consumes it: later uses
		// (including a second drop) are use-after-move.
		if exprType != types.NoTypeID && !tc.isCopyType(exprType) {
			tc.markBindingMoved(symID, span)
		}
		return
	}
	var place Place
	if tc.borrow != nil {
		if info := tc.borrow.Info(bid); info != nil {
			place = info.Place
		}
	}
	tc.recordBorrowEvent(&BorrowEvent{
		Kind:    BorrowEvDrop,
		Borrow:  bid,
		Place:   place,
		Binding: symID,
		Span:    span,
		Scope:   tc.currentScope(),
	})
	if tc.borrow != nil {
		tc.borrow.DropBorrow(bid)
	}
	tc.bindingBorrow[symID] = NoBorrowID
	tc.recordBorrowEvent(&BorrowEvent{
		Kind:    BorrowEvBorrowEnd,
		Borrow:  bid,
		Place:   place,
		Binding: symID,
		Span:    span,
		Scope:   tc.currentScope(),
		Note:    "drop",
	})
}

// handleProjectionDrop performs `@drop o.inner` — releasing one place and
// leaving the rest of the binding readable. Reports whether the target was a
// projection, so the caller can fall through to its own error for a target that
// is no place at all.
//
// An explicit drop of a place IS a move: the value goes somewhere it can never
// come back from. So it records exactly what a move records, and everything
// downstream follows from that — reading `o.inner` afterwards is a
// use-after-move, reading `o` whole is too, the sibling is untouched, and the
// binding's own scope-exit drop reclaims the remainder rather than the whole.
//
// No `own` marker is wanted here, unlike a move into a binding: `@drop` already
// says the value is being disposed of, and the read cannot be mistaken for a
// borrow or a copy.
func (tc *typeChecker) handleProjectionDrop(expr ast.ExprID, span source.Span) bool {
	desc, ok := tc.resolvePlace(expr)
	if !ok || !desc.Base.IsValid() || len(desc.Segments) == 0 {
		return false
	}
	if tc.rejectUnnameableResidual(desc, expr, span) {
		return true
	}
	// Reading it is checked before it is emptied: `@drop o.inner` twice is a
	// use-after-move on the second, and so is dropping a field of a value that
	// has already gone whole.
	tc.checkPlaceUseAfterMove(expr, span)
	expanded, _ := tc.expandPlaceDescriptor(desc)
	place := tc.canonicalPlace(expanded)
	if !place.IsValid() {
		return true
	}
	tc.recordBorrowEvent(&BorrowEvent{
		Kind:  BorrowEvDrop,
		Place: place,
		Span:  span,
		Scope: tc.currentScope(),
		Note:  "drop",
	})
	tc.markPlaceMoved(place, span)
	return true
}

// handleTemporaryProjectionMove takes a field out of a value nothing holds —
// `let e = mk().inner`. Reports whether it handled the expression.
//
// The base is a temporary the lowering itself materialized, so nobody else can
// be using it and handing one of its fields onward ought to be the easy case.
// It was the worst one: the temporary was released WHOLE at the end of the
// statement, which freed the field the binding had just taken, and the binding
// then freed it again — a segfault on the native backend, invalid reads and
// invalid frees under valgrind (RV2-DEBT-084).
//
// The fix is the residual drop applied to a value identified by an EXPRESSION
// instead of by a symbol. What makes that answerable is that a temporary cannot
// outlive its statement and cannot be named again, so the paths taken out of it
// inside this statement are all the paths there will ever be — the plan is
// complete at the point the statement's temp frame closes.
//
// No `own` marker is required, unlike a move out of a live binding. The marker
// is there so a reader can see that the container was emptied and that reading
// it afterwards will fail; here there is no container left to read and no later
// line that could be surprised.
func (tc *typeChecker) handleTemporaryProjectionMove(expr ast.ExprID, exprType types.TypeID, span source.Span) bool {
	// An unresolved type means the expression is already broken; adding a
	// second opinion about it only buries the real diagnostic.
	if exprType == types.NoTypeID || tc.builder == nil {
		return false
	}
	base, path, ok := tc.temporaryProjectionBase(expr)
	if !ok {
		return false
	}
	// Only a PENDING temp candidate is ours to narrow. An evaluation something
	// else already consumed has an owner who will release it whole, and taking
	// a field out from under that owner is the double free this refusal exists
	// to prevent.
	if !tc.pendingTempCandidate(base) {
		tc.report(diag.SemaPartialMoveFromTemporary, span,
			"cannot take a field out of this value: it is released as a whole by whoever owns it, and would take the field with it; bind the value first (`let tmp = ...;`) and take the field from that")
		return true
	}
	for _, seg := range path {
		if placeSegmentEnumerable(seg.Kind) {
			continue
		}
		// Same rule as a named binding, and for the same reason: what survives
		// has to be listable, and an array element is chosen at runtime.
		tc.reportUnnameableResidual(placeDescriptor{}, seg.Kind, expr, span)
		return true
	}
	tc.recordTemporaryTaken(base, path)
	tc.recordPartialMoveRead(expr)
	return true
}

// temporaryProjectionBase walks the projection chain to the evaluation it reads
// out of, returning that expression and the path taken. It parts company with
// resolvePlace exactly where resolvePlace gives up: at a base that is not a
// named binding.
func (tc *typeChecker) temporaryProjectionBase(expr ast.ExprID) (ast.ExprID, []PlaceSegment, bool) {
	cur := expr
	var reversed []PlaceSegment
	// Bounded rather than `for {}`: a malformed tree must not hang the checker.
	for range 64 {
		node := tc.builder.Exprs.Get(cur)
		if node == nil {
			return ast.NoExprID, nil, false
		}
		switch node.Kind {
		case ast.ExprMember:
			data, ok := tc.builder.Exprs.Member(cur)
			if !ok || data == nil {
				return ast.NoExprID, nil, false
			}
			reversed = append(reversed, PlaceSegment{Kind: PlaceSegmentField, Name: data.Field})
			cur = data.Target
		case ast.ExprIndex:
			data, ok := tc.builder.Exprs.Index(cur)
			if !ok || data == nil {
				return ast.NoExprID, nil, false
			}
			reversed = append(reversed, PlaceSegment{Kind: PlaceSegmentIndex})
			cur = data.Target
		case ast.ExprTupleIndex:
			data, ok := tc.builder.Exprs.TupleIndex(cur)
			if !ok || data == nil {
				return ast.NoExprID, nil, false
			}
			reversed = append(reversed, PlaceSegment{Kind: PlaceSegmentTupleIndex, Elem: uint32(data.Index)})
			cur = data.Target
		case ast.ExprGroup:
			data, ok := tc.builder.Exprs.Group(cur)
			if !ok || data == nil {
				return ast.NoExprID, nil, false
			}
			cur = data.Inner
		case ast.ExprUnary:
			data, ok := tc.builder.Exprs.Unary(cur)
			if !ok || data == nil || data.Op != ast.ExprUnaryOwn {
				return ast.NoExprID, nil, false
			}
			cur = data.Operand
		case ast.ExprIdent:
			// A named base is the gate's own case, handled with a place.
			return ast.NoExprID, nil, false
		default:
			if len(reversed) == 0 {
				return ast.NoExprID, nil, false
			}
			path := make([]PlaceSegment, len(reversed))
			for i, seg := range reversed {
				path[len(reversed)-1-i] = seg
			}
			return cur, path, true
		}
	}
	return ast.NoExprID, nil, false
}
