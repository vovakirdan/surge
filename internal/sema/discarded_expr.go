package sema

import "surge/internal/ast"

func (tc *typeChecker) pushDiscardedExpr(expr ast.ExprID) {
	if tc == nil || !expr.IsValid() {
		return
	}
	tc.discardedExprs = append(tc.discardedExprs, tc.unwrapGroupExpr(expr))
}

func (tc *typeChecker) popDiscardedExpr() {
	if tc == nil || len(tc.discardedExprs) == 0 {
		return
	}
	tc.discardedExprs = tc.discardedExprs[:len(tc.discardedExprs)-1]
}

func (tc *typeChecker) isExprDiscarded(expr ast.ExprID) bool {
	if tc == nil || !expr.IsValid() || len(tc.discardedExprs) == 0 {
		return false
	}
	expr = tc.unwrapGroupExpr(expr)
	for i := len(tc.discardedExprs) - 1; i >= 0; i-- {
		if tc.discardedExprs[i] == expr {
			return true
		}
	}
	return false
}
