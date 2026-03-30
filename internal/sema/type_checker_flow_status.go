package sema

import "surge/internal/ast"

type flowStatusMode uint8

const (
	flowStatusBlockReachability flowStatusMode = iota
	flowStatusAbruptExit
)

func (tc *typeChecker) returnStatus(stmtID ast.StmtID) returnStatus {
	return tc.flowStatus(stmtID, flowStatusBlockReachability)
}

func (tc *typeChecker) blockReturnStatus(stmts []ast.StmtID) returnStatus {
	return tc.blockFlowStatus(stmts, flowStatusBlockReachability)
}

func (tc *typeChecker) blockAbruptStatus(stmts []ast.StmtID) returnStatus {
	return tc.blockFlowStatus(stmts, flowStatusAbruptExit)
}

func (tc *typeChecker) blockFlowStatus(stmts []ast.StmtID, mode flowStatusMode) returnStatus {
	for _, child := range stmts {
		if tc.flowStatus(child, mode) == returnClosed {
			return returnClosed
		}
	}
	return returnOpen
}

func (tc *typeChecker) flowStatus(stmtID ast.StmtID, mode flowStatusMode) returnStatus {
	if !stmtID.IsValid() || tc.builder == nil {
		return returnOpen
	}
	stmt := tc.builder.Stmts.Get(stmtID)
	if stmt == nil {
		return returnOpen
	}
	switch stmt.Kind {
	case ast.StmtReturn:
		if mode == flowStatusBlockReachability {
			return returnClosed
		}
		if tc.isExplicitReturnStmt(stmtID) {
			return returnClosed
		}
		ret := tc.builder.Stmts.Return(stmtID)
		if ret != nil && ret.Expr.IsValid() && tc.exprAbruptExit(ret.Expr) {
			return returnClosed
		}
		return returnOpen
	case ast.StmtRet:
		if mode == flowStatusBlockReachability {
			return returnClosed
		}
		return returnOpen
	case ast.StmtBlock:
		if block := tc.builder.Stmts.Block(stmtID); block != nil {
			return tc.blockFlowStatus(block.Stmts, mode)
		}
		return returnOpen
	case ast.StmtLet:
		if letStmt := tc.builder.Stmts.Let(stmtID); letStmt != nil && letStmt.Value.IsValid() && tc.exprAbruptExit(letStmt.Value) {
			return returnClosed
		}
		return returnOpen
	case ast.StmtConst:
		if constStmt := tc.builder.Stmts.Const(stmtID); constStmt != nil && constStmt.Value.IsValid() && tc.exprAbruptExit(constStmt.Value) {
			return returnClosed
		}
		return returnOpen
	case ast.StmtExpr:
		if exprStmt := tc.builder.Stmts.Expr(stmtID); exprStmt != nil && tc.exprAbruptExit(exprStmt.Expr) {
			return returnClosed
		}
		return returnOpen
	case ast.StmtIf:
		ifStmt := tc.builder.Stmts.If(stmtID)
		if ifStmt == nil {
			return returnOpen
		}
		if tc.exprAbruptExit(ifStmt.Cond) {
			return returnClosed
		}
		thenStatus := tc.flowStatus(ifStmt.Then, mode)
		if tc.isBoolLiteralTrue(ifStmt.Cond) {
			return thenStatus
		}
		if !ifStmt.Else.IsValid() {
			return returnOpen
		}
		elseStatus := tc.flowStatus(ifStmt.Else, mode)
		if tc.isBoolLiteralFalse(ifStmt.Cond) {
			return elseStatus
		}
		if thenStatus == returnClosed && elseStatus == returnClosed {
			return returnClosed
		}
		return returnOpen
	case ast.StmtWhile:
		whileStmt := tc.builder.Stmts.While(stmtID)
		if whileStmt == nil {
			return returnOpen
		}
		if tc.exprAbruptExit(whileStmt.Cond) {
			return returnClosed
		}
		if tc.isBoolLiteralTrue(whileStmt.Cond) && tc.flowStatus(whileStmt.Body, mode) == returnClosed {
			return returnClosed
		}
		return returnOpen
	case ast.StmtForClassic:
		forStmt := tc.builder.Stmts.ForClassic(stmtID)
		if forStmt == nil {
			return returnOpen
		}
		if forStmt.Init.IsValid() && tc.flowStatus(forStmt.Init, mode) == returnClosed {
			return returnClosed
		}
		if forStmt.Cond.IsValid() && tc.exprAbruptExit(forStmt.Cond) {
			return returnClosed
		}
		infinite := !forStmt.Cond.IsValid() || tc.isBoolLiteralTrue(forStmt.Cond)
		if infinite && tc.flowStatus(forStmt.Body, mode) == returnClosed {
			return returnClosed
		}
		return returnOpen
	case ast.StmtForIn:
		if forInStmt := tc.builder.Stmts.ForIn(stmtID); forInStmt != nil && tc.exprAbruptExit(forInStmt.Iterable) {
			return returnClosed
		}
		return returnOpen
	default:
		return returnOpen
	}
}

func (tc *typeChecker) isExplicitReturnStmt(stmtID ast.StmtID) bool {
	if !stmtID.IsValid() || tc.builder == nil {
		return false
	}
	stmt := tc.builder.Stmts.Get(stmtID)
	if stmt == nil || stmt.Kind != ast.StmtReturn {
		return false
	}
	ret := tc.builder.Stmts.Return(stmtID)
	if ret == nil {
		return false
	}
	if !ret.Expr.IsValid() {
		return !stmt.Span.Empty()
	}
	retExpr := tc.builder.Exprs.Get(ret.Expr)
	if retExpr == nil {
		return true
	}
	return stmt.Span.File == retExpr.Span.File && stmt.Span.Start < retExpr.Span.Start
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
