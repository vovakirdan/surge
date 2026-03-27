package hir

import "surge/internal/types"

func (l *lowerer) rewriteLegacyBlockTailRet(block *Block, ty types.TypeID) {
	if l == nil || block == nil || len(block.Stmts) == 0 || l.isNothingType(ty) {
		return
	}
	last := block.Stmts[len(block.Stmts)-1]
	if last.Kind != StmtExpr {
		return
	}
	data, ok := last.Data.(ExprStmtData)
	if !ok || data.Expr == nil || l.isNothingType(data.Expr.Type) {
		return
	}
	block.Stmts[len(block.Stmts)-1] = Stmt{
		Kind: StmtRet,
		Span: last.Span,
		Data: RetData{Value: data.Expr},
	}
}
