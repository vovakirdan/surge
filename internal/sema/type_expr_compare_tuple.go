package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// compareTupleElementsAreBorrowed separates tuple DECOMPOSITION from the
// existing whole-value rule for non-unions. A whole non-union pattern still
// receives the subject by move; only fields projected by a tuple pattern stay
// aliases when the subject is read through a borrow.
func (tc *typeChecker) compareTupleElementsAreBorrowed(value, pattern ast.ExprID, subject types.TypeID) bool {
	if tc.builder == nil || tc.types == nil || !tc.subjectReadsThroughBorrow(value) {
		return false
	}
	// Dereferencing a Copy value-composite clones it in a consuming position.
	// Its tuple fields belong to that clone, just as a copied union's payloads
	// belong to the cloned envelope.
	if tc.types.IsCopy(subject) {
		return false
	}
	patternExpr := tc.builder.Exprs.Get(pattern)
	if patternExpr == nil || patternExpr.Kind != ast.ExprTuple {
		return false
	}
	subject = tc.resolveAlias(tc.stripOwnType(subject))
	_, ok := tc.types.TupleInfo(subject)
	return ok
}
