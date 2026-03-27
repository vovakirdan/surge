package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/types"
)

type legacyBlockTailKind uint8

const (
	legacyBlockTailNone legacyBlockTailKind = iota
	legacyBlockTailExprStmt
	legacyBlockTailSyntheticReturn
)

// typeBlockExpr processes a block expression and returns its type.
// During the migration, block values can still come from legacy implicit tail
// returns, but explicit `ret` statements also contribute block-local result types.
func (tc *typeChecker) typeBlockExpr(block *ast.ExprBlockData) types.TypeID {
	if block == nil || len(block.Stmts) == 0 {
		// Empty block has type nothing
		return tc.types.Builtins().Nothing
	}

	// Collect return types (like async blocks do)
	var returns []types.TypeID
	var bareRetSpans []source.Span
	tc.pushReturnContext(returnCtxBlockExpr, types.NoTypeID, source.Span{}, &returns, &bareRetSpans)

	// Walk all statements in the block
	for _, stmtID := range block.Stmts {
		tc.walkStmt(stmtID)
	}

	tc.popReturnContext()

	nothing := tc.types.Builtins().Nothing
	tailExpr, tailSpan, tailKind, hasLegacyTail := tc.legacyImplicitBlockTailExpr(block)
	if hasLegacyTail && tailKind == legacyBlockTailExprStmt {
		tailType := tc.result.ExprTypes[tailExpr]
		if tailType == types.NoTypeID {
			tailType = tc.typeExpr(tailExpr)
		}
		if tailType != types.NoTypeID && tailType != nothing {
			returns = append(returns, tailType)
		}
	}
	sawNonNothing := false
	for _, rt := range returns {
		if rt != types.NoTypeID && rt != nothing {
			sawNonNothing = true
			break
		}
	}
	if sawNonNothing && len(bareRetSpans) > 0 {
		for _, span := range bareRetSpans {
			tc.report(diag.SemaTypeMismatch, span, "bare 'ret;' can only be used in blocks whose result type is nothing; use 'ret value;' or 'ret nothing;'")
		}
		return nothing
	}

	// Determine block type from collected returns
	if len(returns) == 0 {
		return nothing
	}

	// Unify all return types
	payload := nothing
	for _, rt := range returns {
		if rt == types.NoTypeID {
			continue
		}
		if payload == tc.types.Builtins().Nothing {
			payload = rt
			continue
		}
		// Check if types are compatible
		if !tc.typesAssignable(payload, rt, true) && !tc.typesAssignable(rt, payload, true) {
			payload = types.NoTypeID
		}
	}

	if payload == types.NoTypeID {
		return tc.types.Builtins().Nothing
	}
	tc.warnLegacyImplicitBlockValue(tailSpan, hasLegacyTail, payload)

	return payload
}

func (tc *typeChecker) warnLegacyImplicitBlockValue(tailSpan source.Span, hasLegacyTail bool, payload types.TypeID) {
	if tc == nil || tc.reporter == nil || tc.types == nil || tc.discardDepth > 0 {
		return
	}
	nothing := tc.types.Builtins().Nothing
	if payload == types.NoTypeID || payload == nothing {
		return
	}
	if !hasLegacyTail || tailSpan == (source.Span{}) {
		return
	}
	if b := diag.ReportWarning(tc.reporter, diag.SemaImplicitBlockValue, tailSpan,
		"legacy implicit block value should use 'ret'; insert 'ret' for an explicit block exit"); b != nil {
		b.WithFixSuggestion(fix.InsertText("insert 'ret '", tailSpan.ZeroideToStart(), "ret ", "", fix.Preferred()))
		b.Emit()
	}
}

func (tc *typeChecker) legacyImplicitBlockTailExpr(block *ast.ExprBlockData) (ast.ExprID, source.Span, legacyBlockTailKind, bool) {
	if tc == nil || tc.builder == nil || block == nil || len(block.Stmts) == 0 {
		return ast.NoExprID, source.Span{}, legacyBlockTailNone, false
	}
	stmtID := block.Stmts[len(block.Stmts)-1]
	stmt := tc.builder.Stmts.Get(stmtID)
	if stmt == nil {
		return ast.NoExprID, source.Span{}, legacyBlockTailNone, false
	}
	switch stmt.Kind {
	case ast.StmtExpr:
		exprStmt := tc.builder.Stmts.Expr(stmtID)
		if exprStmt == nil || !exprStmt.Expr.IsValid() {
			return ast.NoExprID, source.Span{}, legacyBlockTailNone, false
		}
		expr := tc.builder.Exprs.Get(exprStmt.Expr)
		if expr == nil {
			return ast.NoExprID, source.Span{}, legacyBlockTailNone, false
		}
		return exprStmt.Expr, expr.Span, legacyBlockTailExprStmt, true
	case ast.StmtReturn:
	default:
		return ast.NoExprID, source.Span{}, legacyBlockTailNone, false
	}
	ret := tc.builder.Stmts.Return(stmtID)
	if ret == nil || !ret.Expr.IsValid() {
		return ast.NoExprID, source.Span{}, legacyBlockTailNone, false
	}
	retExpr := tc.builder.Exprs.Get(ret.Expr)
	if retExpr == nil {
		return ast.NoExprID, source.Span{}, legacyBlockTailNone, false
	}
	// Synthetic parser returns reuse the expression span (or start at it),
	// while explicit `return` starts earlier at the keyword.
	if stmt.Span.File != retExpr.Span.File || stmt.Span.Start < retExpr.Span.Start {
		return ast.NoExprID, source.Span{}, legacyBlockTailNone, false
	}
	return ret.Expr, retExpr.Span, legacyBlockTailSyntheticReturn, true
}
