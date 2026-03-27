package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

func (tc *typeChecker) recordBlockResultExprs(blockExpr ast.ExprID, results []collectedResult) {
	if tc == nil || !blockExpr.IsValid() {
		return
	}
	if tc.blockResultExprs == nil {
		tc.blockResultExprs = make(map[ast.ExprID][]ast.ExprID)
	}
	blockExpr = tc.unwrapGroupExpr(blockExpr)
	delete(tc.blockResultExprs, blockExpr)
	if len(results) == 0 {
		return
	}
	seen := make(map[ast.ExprID]struct{}, len(results))
	for _, result := range results {
		if !result.expr.IsValid() {
			continue
		}
		expr := tc.unwrapGroupExpr(result.expr)
		if !expr.IsValid() {
			continue
		}
		if _, ok := seen[expr]; ok {
			continue
		}
		seen[expr] = struct{}{}
		tc.blockResultExprs[blockExpr] = append(tc.blockResultExprs[blockExpr], expr)
	}
}

func (tc *typeChecker) forEachReturnLikeExpr(expr ast.ExprID, visit func(ast.ExprID)) {
	if tc == nil || !expr.IsValid() || visit == nil {
		return
	}
	seen := make(map[ast.ExprID]struct{})
	var walk func(ast.ExprID)
	walk = func(current ast.ExprID) {
		current = tc.unwrapGroupExpr(current)
		if !current.IsValid() {
			return
		}
		if _, ok := seen[current]; ok {
			return
		}
		seen[current] = struct{}{}
		if nested := tc.blockResultExprs[current]; len(nested) > 0 {
			for _, inner := range nested {
				walk(inner)
			}
			return
		}
		visit(current)
	}
	walk(expr)
}

func (tc *typeChecker) applyReturnPathChecks(expr ast.ExprID) {
	if tc == nil || !expr.IsValid() {
		return
	}
	tc.forEachReturnLikeExpr(expr, func(candidate ast.ExprID) {
		candidateType := tc.result.ExprTypes[candidate]
		if candidateType == types.NoTypeID {
			candidateType = tc.typeExpr(candidate)
		}
		candidateSpan := tc.exprSpan(candidate)
		if candidateSpan == (source.Span{}) {
			candidateSpan = tc.exprSpan(expr)
		}
		if tc.isLocalTaskExpr(candidate) {
			tc.report(diag.SemaLocalTaskNotSendable, candidateSpan,
				"local task handle cannot be returned from function")
		}
		tc.checkTaskContainerEscape(candidate, candidateType, candidateSpan)
		tc.trackTaskReturn(candidate)
		tc.checkTrivialReturnRecursion(candidate)
	})
}
