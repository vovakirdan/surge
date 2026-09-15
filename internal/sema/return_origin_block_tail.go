package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// A brace block that starts with a statement keyword is not normalized by the
// parser, so its last `expr;` stays a statement. Sema still types it as the
// block's value and HIR rewrites it into `ret`, unless the block or that value
// is nothing; this names the statement that ends the block with that value.
func (b *returnOriginBody) legacyExprTail(id ast.ExprID, stmts []ast.StmtID) ast.StmtID {
	u := b.function.unit
	if len(stmts) == 0 {
		return ast.NoStmtID
	}
	last := stmts[len(stmts)-1]
	if node := u.Builder.Stmts.Get(last); node == nil || node.Kind != ast.StmtExpr {
		return ast.NoStmtID
	}
	data := u.Builder.Stmts.Expr(last)
	if data == nil || !data.Expr.IsValid() || returnOriginNothing(u.Sema.TypeInterner, u.Sema.ExprTypes[id]) ||
		returnOriginNothing(u.Sema.TypeInterner, u.Sema.ExprTypes[data.Expr]) {
		return ast.NoStmtID
	}
	return last
}

// The same test HIR applies before its rewrite: a plain lookup, no alias walk.
func returnOriginNothing(in *types.Interner, id types.TypeID) bool {
	if id == types.NoTypeID || in == nil {
		return false
	}
	typ, ok := in.Lookup(id)
	return ok && typ.Kind == types.KindNothing
}
