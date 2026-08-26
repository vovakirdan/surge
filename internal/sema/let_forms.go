package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
)

// The two `let` forms that bind no simple name. Both live here rather than in
// the statement walk so that the walk stays the size it was.

// checkTupleLet types `let (x, y) = value` and binds the pattern.
func (tc *typeChecker) checkTupleLet(letStmt *ast.LetStmt, scope symbols.ScopeID) {
	valueType := tc.typeExpr(letStmt.Value)
	tc.observeMove(letStmt.Value, tc.exprSpan(letStmt.Value))
	tc.bindTuplePattern(letStmt.Pattern, valueType, scope)
}

// checkDiscardedLet types `let _ = value`.
//
// `_` names nobody, so nobody receives the value: it is the discarded result
// the statement `value;` is, released at the end of this statement by the
// same temporary machinery, and a PLACE on the right is read, not moved --
// `x` stays with its binding. Binding it instead consumed the temporary on
// behalf of a binding that never dropped, and every owning value discarded
// through `_` leaked.
func (tc *typeChecker) checkDiscardedLet(letStmt *ast.LetStmt, scope symbols.ScopeID) {
	declaredType := tc.resolveTypeExprWithScope(letStmt.Type, scope)
	tc.pushDiscardedExpr(letStmt.Value)
	valueType := tc.typeExprWithExpected(letStmt.Value, declaredType)
	tc.popDiscardedExpr()
	tc.ensureBindingTypeMatch(letStmt.Type, declaredType, valueType, letStmt.Value)
}
