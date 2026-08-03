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
	if tc.compareTupleSubjectIsCopy(subject) {
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

// compareTupleSubjectIsCopy preserves a nominal @copy marker while walking
// aliases. typeChecker.isCopyType resolves the complete alias chain first, so
// it cannot see a marker attached to an intermediate nominal alias.
func (tc *typeChecker) compareTupleSubjectIsCopy(subject types.TypeID) bool {
	const maxDepth = 32
	for range maxDepth {
		if tc.types.IsCopy(subject) {
			return true
		}
		tt, ok := tc.types.Lookup(subject)
		if !ok {
			return tc.isCopyType(subject)
		}
		switch tt.Kind {
		case types.KindAlias:
			target, ok := tc.types.AliasTarget(subject)
			if !ok || target == types.NoTypeID || target == subject {
				return tc.isCopyType(subject)
			}
			subject = target
		case types.KindOwn:
			subject = tt.Elem
		default:
			return tc.isCopyType(subject)
		}
	}
	return false
}
