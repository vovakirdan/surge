package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) walkForInStmt(id ast.StmtID, stmt *ast.Stmt) {
	if stmt == nil {
		return
	}
	forIn := tc.builder.Stmts.ForIn(id)
	if forIn == nil {
		return
	}
	scope := tc.scopeForStmt(id)
	pushed := tc.pushScope(scope)

	iterableType := tc.typeExpr(forIn.Iterable)
	var containerPlace Place
	containerTracked := tc.isTaskContainerType(iterableType)
	if containerTracked {
		if place, ok := tc.taskContainerPlace(forIn.Iterable); ok {
			containerPlace = place
		} else {
			containerTracked = false
		}
	}

	elemType := tc.forInElementType(forIn, iterableType, scope, stmt.Span)

	var loopSym symbols.SymbolID
	if forIn.Pattern != source.NoStringID {
		if symID := tc.stmtSymbols[id]; symID.IsValid() && elemType != types.NoTypeID {
			tc.bindingTypes[symID] = elemType
			loopSym = symID
			// The loop binding does not own the element. It is bound by a
			// word-wise copy of the iterator's payload - visible in MIR as
			// `tag_payload_move copy` - and the frame emits no drop for it,
			// because the container is still the owner. Anything that frees
			// THROUGH this binding therefore frees the container's storage,
			// which the container then frees again on its own reclamation.
			// Measured: `for s in strs { s = mk(7); }` aborts natively with
			// `free(): double free detected in tcache 2`.
			tc.markNonOwningBinding(symID)
		}
	}

	movedBefore := tc.bindingMoved(loopSym)
	movedBeforeLoop := tc.snapshotMovedPlaces()
	tc.enterLoopDropScope()
	tc.walkStmt(forIn.Body)
	tc.rejectLoopBackEdgeMoves(movedBeforeLoop, "for-in loop")
	tc.leaveLoopDropScope()
	movedAfter := tc.bindingMoved(loopSym)
	if containerTracked {
		tc.checkForInTaskConsumed(forIn, containerPlace, movedBefore, movedAfter, stmt.Span)
	}
	if pushed {
		tc.leaveScope()
	}
}

func (tc *typeChecker) forInElementType(forIn *ast.ForInStmt, iterableType types.TypeID, scope symbols.ScopeID, span source.Span) types.TypeID {
	inferredElemType := types.NoTypeID
	if iterableType != types.NoTypeID {
		inferredElemType = tc.inferForInElementType(forIn.Iterable, iterableType, span)
	}
	if !forIn.Type.IsValid() {
		return inferredElemType
	}
	elemType := tc.resolveTypeExprWithScope(forIn.Type, scope)
	if elemType != types.NoTypeID && inferredElemType != types.NoTypeID && !tc.typesAssignable(elemType, inferredElemType, true) {
		tc.report(diag.SemaTypeMismatch, tc.typeSpan(forIn.Type),
			"iterator yields %s, not %s",
			tc.typeLabel(inferredElemType), tc.typeLabel(elemType))
	}
	return elemType
}

func (tc *typeChecker) checkForInTaskConsumed(forIn *ast.ForInStmt, containerPlace Place, movedBefore, movedAfter bool, span source.Span) {
	info := tc.taskContainers[containerPlace]
	if info == nil || !info.Pending {
		return
	}
	consumed := movedAfter && !movedBefore
	if !consumed {
		reportSpan := forIn.PatternSpan
		if reportSpan == (source.Span{}) {
			reportSpan = span
		}
		tc.report(diag.SemaTaskNotAwaited, reportSpan, "task in container is not consumed in for-in loop")
	}
	tc.markTaskContainerConsumed(containerPlace)
}
