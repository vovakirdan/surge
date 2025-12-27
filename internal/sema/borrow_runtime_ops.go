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

func (tc *typeChecker) handleAssignment(op ast.ExprBinaryOp, left, right ast.ExprID, span source.Span) {
	// Check @readonly attribute before allowing assignment
	if tc.checkReadonlyFieldWrite(left, span) {
		return // @readonly violation reported
	}

	desc, ok := tc.resolvePlace(left)
	if !ok {
		return
	}
	if desc.Base.IsValid() && len(desc.Segments) == 0 {
		tc.clearBindingMoved(desc.Base)
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
	tc.typeExpr(expr)
	symID := tc.symbolForExpr(expr)
	if !symID.IsValid() {
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
