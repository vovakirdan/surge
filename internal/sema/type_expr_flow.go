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
	movedBefore := tc.snapshotMovedBindings()
	movedArms := make([]map[symbols.SymbolID]source.Span, len(cmp.Arms))
	armClosed := make([]bool, len(cmp.Arms))
	valueType := tc.typeExpr(cmp.Value)
	movedAfterValue := tc.snapshotMovedBindings()
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
		tc.restoreMovedBindings(movedAfterValue)
		armSubject := valueType
		if narrowed := tc.narrowCompareSubjectType(valueType, remainingMembers); narrowed != types.NoTypeID {
			armSubject = narrowed
		}
		tc.inferComparePatternTypes(arm.Pattern, armSubject)
		if arm.Guard.IsValid() {
			tc.ensureBoolContext(arm.Guard, tc.exprSpan(arm.Guard))
		}
		if compareDiscarded {
			tc.pushDiscardedExpr(arm.Result)
		}
		armResult := tc.typeExprWithExpected(arm.Result, expectedCompare)
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
		movedArms[i] = tc.snapshotMovedBindings()
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

	var mergedMoves map[symbols.SymbolID]source.Span
	for i := range cmp.Arms {
		if armClosed[i] {
			continue
		}
		if mergedMoves == nil {
			mergedMoves = movedArms[i]
			continue
		}
		mergedMoves = mergeMovedBindings(mergedMoves, movedArms[i])
	}
	if mergedMoves == nil {
		tc.movedBindings = movedBefore
	} else {
		tc.movedBindings = mergedMoves
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
	resultType := tc.taskBlockPayload(span, blockingData.Body, false)
	captures := tc.collectBlockingCaptures(blockingData.Body)
	tc.recordBlockingCaptures(id, captures)
	for _, cap := range captures {
		capType := tc.bindingType(cap.symID)
		if tc.isReferenceType(capType) {
			tc.report(diag.SemaBlockingBorrowCapture, cap.span,
				"blocking captures must be by value; cannot capture reference %s", tc.typeLabel(capType))
			continue
		}
		tc.checkSpawnSendability(cap.symID, cap.span)
		tc.observeMove(cap.exprID, cap.span)
	}
	return resultType
}
