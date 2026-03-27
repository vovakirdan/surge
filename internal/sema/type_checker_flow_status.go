package sema

import "surge/internal/ast"

func (tc *typeChecker) blockReturnStatus(stmts []ast.StmtID) returnStatus {
	for _, child := range stmts {
		if tc.returnStatus(child) == returnClosed {
			return returnClosed
		}
	}
	return returnOpen
}

func (tc *typeChecker) isBoolLiteralFalse(expr ast.ExprID) bool {
	if !expr.IsValid() || tc.builder == nil {
		return false
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.ExprLit:
		if lit, ok := tc.builder.Exprs.Literal(expr); ok && lit != nil {
			return lit.Kind == ast.ExprLitFalse
		}
	case ast.ExprGroup:
		if grp, ok := tc.builder.Exprs.Group(expr); ok && grp != nil {
			return tc.isBoolLiteralFalse(grp.Inner)
		}
	}
	return false
}
