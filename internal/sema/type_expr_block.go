package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
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

	return payload
}
