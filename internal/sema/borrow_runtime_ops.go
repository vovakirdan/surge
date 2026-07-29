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
	// Taking a PROJECTION out of a live binding is a partial move, and sema
	// tracks moves per binding rather than per place. It can therefore neither
	// invalidate just the place that left nor drop only the places that stayed,
	// so the extraction silently ALIASES the container instead of taking from
	// it: mutating the extracted value is visible through the original.
	//
	// The segments are read BEFORE expandPlaceDescriptor on purpose. Expansion
	// rewrites a place through the borrow its base came from, which manufactures
	// segments the source never wrote; the question here is what the PROGRAM
	// said, not where the value ultimately lives.
	//
	// Scaffolding, and removed once the moved-set is place-keyed and drop
	// obligations carry paths. It exists so those two can land in separate
	// steps: a place-aware moved-set with binding-granular drops would release a
	// field that had already moved, and this keeps that window unreachable from
	// user code. Nothing in the corpus writes this shape today.
	if !direct && base.IsValid() {
		tc.reportPartialMove(desc, expr, span)
		return
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
	if direct && base.IsValid() {
		tc.markBindingMoved(base, evSpan)
	}
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
	if desc.Base.IsValid() && len(desc.Segments) == 0 {
		// The RHS is fully evaluated by now, so moved-ness decides the
		// overwritten-value drop (x = f(x) suppresses it); the store
		// then revives the binding with the new value.
		tc.recordReassignOldDrop(exprID, desc.Base)
		tc.clearBindingMoved(desc.Base)
		// Ownership of the NEW value: a projection read stays with its
		// container (the binding becomes an alias); anything else makes
		// the binding a fresh owner again.
		if tc.isProjectionRead(right) {
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
		if tc.rejectProjectionDrop(expr, span) {
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

// rejectProjectionDrop refuses `@drop o.inner` and says why, instead of letting
// it fall into "drop target must be a binding" — the target IS addressable, and
// a reader told otherwise has no way to find the real reason.
//
// Epic 24's settled decisions make this legal, and sema is ready for it: the
// moved-set can name a field, so releasing one and leaving the siblings
// readable is expressible. NEITHER BACKEND CAN PERFORM IT. A projected
// `InstrDrop` is a silent no-op on the native backend, and the VM's
// `execInstrDrop` ignores the projection and drops the WHOLE local — measured,
// `@drop o.inner; return o.label;` panics with a use-after-free on the next
// line. So accepting it here would ship a statement that does nothing on one
// backend and corrupts on the other.
//
// Lifted by step 7, which teaches both backends a projected drop, together with
// step 6, which stops the binding's own scope-exit drop from releasing a field
// that has already gone.
func (tc *typeChecker) rejectProjectionDrop(expr ast.ExprID, span source.Span) bool {
	desc, ok := tc.resolvePlace(expr)
	if !ok || !desc.Base.IsValid() || len(desc.Segments) == 0 {
		return false
	}
	strs := tc.builder.StringsInterner
	if strs == nil && tc.symbols != nil && tc.symbols.Table != nil {
		strs = tc.symbols.Table.Strings
	}
	label := formatPlaceSegments(tc.bindingName(desc.Base), desc.Segments, strs)
	tc.report(diag.SemaPartialMoveUnsupported, span,
		"cannot drop `%s` on its own yet: releasing one field and keeping the rest needs a drop neither backend can perform, so it would do nothing here and free the whole value elsewhere; drop `%s` instead",
		label, tc.bindingName(desc.Base))
	return true
}
