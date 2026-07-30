package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) typeExprCompare(id ast.ExprID, span source.Span) types.TypeID {
	cmp, ok := tc.builder.Exprs.Compare(id)
	if !ok || cmp == nil {
		return types.NoTypeID
	}
	movedBefore := tc.snapshotMovedPlaces()
	movedArms := make([]map[Place]source.Span, len(cmp.Arms))
	armClosed := make([]bool, len(cmp.Arms))
	valueType := tc.typeExpr(cmp.Value)
	tc.observeMove(cmp.Value, tc.exprSpan(cmp.Value))
	movedAfterValue := tc.snapshotMovedPlaces()
	expectedCompare := tc.expectedTypeForExpr(id)
	resultType := expectedCompare
	remainingMembers := tc.unionMembers(valueType)
	nothingType := types.NoTypeID
	if tc.types != nil {
		nothingType = tc.types.Builtins().Nothing
	}
	armTypes := make([]types.TypeID, len(cmp.Arms))
	compareDiscarded := tc.isExprDiscarded(id)

	for i, arm := range cmp.Arms {
		tc.restoreMovedPlaces(movedAfterValue)
		armSubject := valueType
		if narrowed := tc.narrowCompareSubjectType(valueType, remainingMembers); narrowed != types.NoTypeID {
			armSubject = narrowed
		}
		var armBindings []symbols.SymbolID
		tc.inferComparePatternTypes(arm.Pattern, armSubject, &armBindings)
		if arm.Guard.IsValid() {
			// A guard runs BEFORE this arm commits (payload extraction
			// already ran, but a failed guard falls through to the next
			// arm against the SAME scrutinee) — moving one of this arm's
			// own pattern bindings out of the guard would free storage
			// the next arm (or this fix's own deep-drop release) still
			// needs. Guards may only borrow; see reportCompareGuardMove.
			tc.pushCompareGuardBindings(armBindings)
			tc.ensureBoolContext(arm.Guard, tc.exprSpan(arm.Guard))
			tc.popCompareGuardBindings(armBindings)
		}
		if compareDiscarded {
			tc.pushDiscardedExpr(arm.Result)
		}
		armResult := tc.typeExprWithExpected(arm.Result, expectedCompare)
		tc.consumeTempCandidate(arm.Result)
		if compareDiscarded {
			tc.popDiscardedExpr()
		}
		armAbrupt := tc.compareArmAbruptExit(arm.Result)
		armClosed[i] = armAbrupt
		armTypes[i] = armResult
		if !armAbrupt && armResult != types.NoTypeID {
			if expectedCompare != types.NoTypeID {
				tc.ensureBindingTypeMatch(ast.NoTypeID, expectedCompare, armResult, arm.Result)
			} else {
				switch {
				case resultType == types.NoTypeID:
					resultType = armResult
				case nothingType != types.NoTypeID && resultType == nothingType:
					resultType = armResult
				case nothingType != types.NoTypeID && armResult == nothingType:
				case tc.typesAssignable(resultType, armResult, true):
				case tc.typesAssignable(armResult, resultType, true):
					resultType = armResult
				default:
					tc.report(diag.SemaTypeMismatch, tc.exprSpan(arm.Result), "compare arm type mismatch: expected %s, got %s", tc.typeLabel(resultType), tc.typeLabel(armResult))
				}
			}
		}
		if len(remainingMembers) > 0 {
			remainingMembers = tc.consumeCompareMembers(remainingMembers, arm)
		}
		movedArms[i] = tc.snapshotMovedPlaces()
	}

	targetCompare := resultType
	if expectedCompare != types.NoTypeID {
		targetCompare = expectedCompare
	}
	if expectedCompare == types.NoTypeID && targetCompare != types.NoTypeID && !tc.isExprDiscarded(id) && (nothingType == types.NoTypeID || targetCompare != nothingType) {
		for i, arm := range cmp.Arms {
			if armClosed[i] || armTypes[i] != nothingType {
				continue
			}
			tc.ensureBindingTypeMatch(ast.NoTypeID, targetCompare, armTypes[i], arm.Result)
		}
	}

	// Union across the arms: a value moved on ANY reachable arm is moved after
	// the compare, because a later use has to be rejected if some path gave it
	// away. An intersection was computed here alongside the union and never
	// read; it encoded "moved on every arm", which is not the condition a use
	// is checked against, so it was deleted rather than carried to places.
	var mergedMoves map[Place]source.Span
	for i := range cmp.Arms {
		if armClosed[i] {
			continue
		}
		if mergedMoves == nil {
			mergedMoves = movedArms[i]
			continue
		}
		mergedMoves = mergeMovedPlaces(mergedMoves, movedArms[i])
	}
	if mergedMoves != nil {
		// Per-arm drop synthesis: a droppable moved on some arms but live
		// on this one drops at this arm's end (not a hard error). After
		// the join every such binding is in the union moved-set, so using
		// it stays a use-of-moved error.
		for i := range cmp.Arms {
			if armClosed[i] {
				continue
			}
			drops, plans := tc.oneSidedObligations(movedArms[i], mergedMoves)
			if len(drops) == 0 {
				continue
			}
			if tc.result.ArmDropsExpr == nil {
				tc.result.ArmDropsExpr = make(map[ast.ExprID][]symbols.SymbolID)
			}
			tc.result.ArmDropsExpr[cmp.Arms[i].Result] = drops
			tc.recordOneSidedDrops(DropSite{Expr: cmp.Arms[i].Result}, plans)
		}
	}
	if mergedMoves == nil {
		tc.movedPlaces = movedBefore
	} else {
		tc.movedPlaces = mergedMoves
	}
	if expectedCompare == types.NoTypeID && resultType != types.NoTypeID {
		for i, arm := range cmp.Arms {
			tc.recordNumericWidening(arm.Result, armTypes[i], resultType)
		}
	}
	tc.checkCompareExhausiveness(cmp, valueType, span)
	return resultType
}

func (tc *typeChecker) taskBlockPayload(span source.Span, body ast.StmtID, async bool) types.TypeID {
	var returns []collectedResult
	tc.pushReturnContext(returnCtxTaskPayload, types.NoTypeID, span, &returns, nil)
	if async {
		tc.awaitDepth++
		tc.asyncBlockDepth++
	}
	tc.walkStmt(body)
	if async {
		tc.asyncBlockDepth--
		tc.awaitDepth--
	}
	tc.popReturnContext()

	payload := tc.types.Builtins().Nothing
	for _, result := range returns {
		rt := result.typ
		if rt == types.NoTypeID {
			continue
		}
		if payload == tc.types.Builtins().Nothing {
			payload = rt
			continue
		}
		if !tc.typesAssignable(payload, rt, true) && !tc.typesAssignable(rt, payload, true) {
			payload = types.NoTypeID
		}
	}
	if payload == types.NoTypeID {
		payload = tc.types.Builtins().Nothing
	}
	return tc.taskType(payload, span)
}

func (tc *typeChecker) typeExprAsync(id ast.ExprID, span source.Span) types.TypeID {
	asyncData, ok := tc.builder.Exprs.Async(id)
	if !ok || asyncData == nil {
		return types.NoTypeID
	}
	return tc.taskBlockPayload(span, asyncData.Body, true)
}

func (tc *typeChecker) typeExprBlocking(id ast.ExprID, span source.Span) types.TypeID {
	blockingData, ok := tc.builder.Exprs.Blocking(id)
	if !ok || blockingData == nil {
		return types.NoTypeID
	}
	tc.blockingDepth++
	resultType := tc.taskBlockPayload(span, blockingData.Body, false)
	tc.blockingDepth--
	captures := tc.collectBlockingCaptures(blockingData.Body)
	tc.recordBlockingCaptures(id, captures)
	for _, cap := range captures {
		capType := tc.bindingType(cap.symID)
		if tc.isReferenceType(capType) {
			tc.report(diag.SemaBlockingBorrowCapture, cap.span,
				"blocking captures must be by value; cannot capture reference %s", tc.typeLabel(capType))
			continue
		}
		// `blocking` ships its state to a worker thread while this one keeps
		// running, so a captured arbitrary-precision value would leave both
		// threads pointing at one counted block. The count is deliberately not
		// atomic, so that is a race, not just a sharing question. A Copy
		// capture cannot be made exclusive either — the caller keeps its
		// binding by definition. Refuse until the boundary deep-copies.
		if tc.result != nil && tc.result.ContainsRefCountedScalar(capType) && tc.isCopyType(capType) {
			tc.report(diag.SemaCrossNotShardMovable, cap.span,
				"`%s` cannot be captured into `blocking` yet: it carries an arbitrary-precision "+
					"value, which is a reference into a counted heap block, and the count is not "+
					"safe to share with the worker thread. Use a fixed-width type (`float64`) for the "+
					"captured value",
				tc.typeLabel(capType))
			continue
		}
		tc.checkSpawnSendability(cap.symID, cap.span)
		tc.observeMove(cap.exprID, cap.span)
	}
	return resultType
}
