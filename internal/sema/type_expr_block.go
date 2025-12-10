package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

// typeBlockExpr processes a block expression and returns its type.
// A block expression contains statements and must end with a return statement
// (unless the expected type is 'nothing').
// The type of the block is the type of the return expression.
func (tc *typeChecker) typeBlockExpr(block *ast.ExprBlockData) types.TypeID {
	if block == nil || len(block.Stmts) == 0 {
		// Empty block has type nothing
		return tc.types.Builtins().Nothing
	}

	// Collect return types (like async blocks do)
	var returns []types.TypeID
	tc.pushReturnContext(types.NoTypeID, source.Span{}, &returns)

	// Walk all statements in the block
	for _, stmtID := range block.Stmts {
		tc.walkStmt(stmtID)
	}

	tc.popReturnContext()

	// Determine block type from collected returns
	if len(returns) == 0 {
		return tc.types.Builtins().Nothing
	}

	// Unify all return types
	payload := tc.types.Builtins().Nothing
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
