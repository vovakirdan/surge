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
func (tc *typeChecker) typeBlockExpr(id ast.ExprID, block *ast.ExprBlockData) types.TypeID {
	if block == nil || len(block.Stmts) == 0 {
		// Empty block has type nothing
		return tc.types.Builtins().Nothing
	}

	// Collect return types (like async blocks do)
	var returns []collectedResult
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
			returns = append(returns, collectedResult{typ: tailType, span: tailSpan, expr: tailExpr})
		}
	}
	tc.recordBlockResultExprs(id, returns)
	sawNonNothing := false
	for _, result := range returns {
		rt := result.typ
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
	for _, result := range returns {
		rt := result.typ
		if rt == types.NoTypeID {
			continue
		}
		if payload == nothing {
			payload = rt
			continue
		}
		switch {
		case tc.typesAssignable(payload, rt, true):
		case tc.typesAssignable(rt, payload, true):
			payload = rt
		default:
			tc.report(diag.SemaTypeMismatch, result.span, "block result type mismatch: expected %s, got %s", tc.typeLabel(payload), tc.typeLabel(rt))
			return types.NoTypeID
		}
	}

	if payload != nothing && !tc.blockExprProducesValueOnAllPaths(block, tailExpr, hasLegacyTail) {
		tc.report(diag.SemaTypeMismatch, tc.exprSpan(id), "block result type mismatch: expected %s, got nothing", tc.typeLabel(payload))
		return types.NoTypeID
	}

	tc.warnLegacyImplicitBlockValue(id, tailSpan, hasLegacyTail, payload)

	return payload
}

func (tc *typeChecker) blockExprProducesValueOnAllPaths(block *ast.ExprBlockData, tailExpr ast.ExprID, hasLegacyTail bool) bool {
	if tc == nil || block == nil {
		return false
	}
	if tc.blockReturnStatus(block.Stmts) == returnClosed {
		return true
	}
	if !hasLegacyTail || !tailExpr.IsValid() || tc.types == nil {
		return false
	}
	tailType := tc.result.ExprTypes[tailExpr]
	if tailType == types.NoTypeID {
		tailType = tc.typeExpr(tailExpr)
	}
	return tailType != types.NoTypeID && tailType != tc.types.Builtins().Nothing
}

func (tc *typeChecker) warnLegacyImplicitBlockValue(id ast.ExprID, tailSpan source.Span, hasLegacyTail bool, payload types.TypeID) {
	if tc == nil || tc.reporter == nil || tc.types == nil || tc.isExprDiscarded(id) {
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
