package sema

import (
	"surge/internal/ast"
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

	// Walk all statements in the block
	for _, stmtID := range block.Stmts {
		tc.walkStmt(stmtID)
	}

	// Find the return type by looking for return statements
	returnType, hasReturn := tc.findBlockReturnType(block.Stmts)

	if hasReturn {
		return returnType
	}

	// No return found - block has type nothing
	// We'll check in type assignment if this is valid (nothing blocks don't require return)
	return tc.types.Builtins().Nothing
}

// findBlockReturnType searches for return statements in the block
// and returns the type of the return expression.
// It returns (type, true) if a return was found, (NoTypeID, false) otherwise.
func (tc *typeChecker) findBlockReturnType(stmts []ast.StmtID) (types.TypeID, bool) {
	for _, stmtID := range stmts {
		stmt := tc.builder.Stmts.Get(stmtID)
		if stmt == nil {
			continue
		}

		switch stmt.Kind {
		case ast.StmtReturn:
			ret := tc.builder.Stmts.Return(stmtID)
			if ret == nil {
				continue
			}
			if ret.Expr.IsValid() {
				return tc.typeExpr(ret.Expr), true
			}
			// return; with no expression
			return tc.types.Builtins().Nothing, true

		case ast.StmtBlock:
			// Nested block - check recursively
			block := tc.builder.Stmts.Block(stmtID)
			if block != nil {
				if ty, found := tc.findBlockReturnType(block.Stmts); found {
					return ty, true
				}
			}

		case ast.StmtIf:
			// If statement - check both branches
			ifStmt := tc.builder.Stmts.If(stmtID)
			if ifStmt != nil {
				// Check then branch
				thenBlock := tc.builder.Stmts.Block(ifStmt.Then)
				if thenBlock != nil {
					if ty, found := tc.findBlockReturnType(thenBlock.Stmts); found {
						// Also check else branch if present
						if ifStmt.Else.IsValid() {
							elseBlock := tc.builder.Stmts.Block(ifStmt.Else)
							if elseBlock != nil {
								if elseType, elseFound := tc.findBlockReturnType(elseBlock.Stmts); elseFound {
									// Both branches return - types should match
									// For now just return the then type
									_ = elseType
									return ty, true
								}
							}
						}
						// Only then branch returns - that's still a return path
						return ty, true
					}
				}
				// Check else branch
				if ifStmt.Else.IsValid() {
					elseBlock := tc.builder.Stmts.Block(ifStmt.Else)
					if elseBlock != nil {
						if ty, found := tc.findBlockReturnType(elseBlock.Stmts); found {
							return ty, true
						}
					}
				}
			}
		}
	}
	return types.NoTypeID, false
}
