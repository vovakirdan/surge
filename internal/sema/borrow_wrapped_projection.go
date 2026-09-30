package sema

import (
	"surge/internal/ast"
)

// A `&mut` a call returns into what its argument points at is held by the
// binding it is bound to (projectedMutReborrow): `let e = firstm(k); app(k);`
// is refused. Bound INSIDE a value that carries it -- `Some::<&mut int>(
// firstm(k))`, that inside a tuple or a record field, a choice among such, or
// a `let e: Option<&mut int> = firstm(k)` that wraps it implicitly -- the
// binding is not a reference, so no one took those loans: the value's loans
// were collected from the operands' own loans, and a reference parameter `k`
// stands on none in this frame. The grow was accepted and the payload then
// read freed memory.
//
// wrappedProjectionLoans takes, for such a value, the loans the bare binding
// would take, and the binding holds them as it holds any loan its value
// carries (holdViewLoansForBinding).

// wrappedProjectionLoans reports the projection loans of a `&mut` call result
// that is not itself bound as a reference. ok is false outside a holder's walk
// (holdWrappedProjections), when expr is not such a projection, or when its
// loans were already taken where it is bound. A refused loan is reported by
// the walk, and the value then holds none.
//
// The loans are taken only for a holder -- a binding, a compare arm's
// bindings -- never for a value merely read, such as a `ret` handing its loans
// out of a loop body: taken there, the next pass over the body found them
// still live. Within one walk the loans accumulate in one list, so the values
// of a choice, which are alternatives, hold one loan on a place they share
// rather than two that conflict, as a bare binding's choice does.
func (tc *typeChecker) wrappedProjectionLoans(expr ast.ExprID) (loans []BorrowID, ok bool) {
	expr = tc.unwrapGroupExpr(expr)
	if tc.heldProjections == nil || tc.borrow == nil || tc.result == nil || !expr.IsValid() ||
		tc.borrow.ExprBorrow(expr) != NoBorrowID {
		return nil, false
	}
	if !tc.isMutRefType(tc.resolveAlias(tc.result.ExprTypes[expr])) {
		return nil, false
	}
	scope := tc.currentScope()
	if !scope.IsValid() {
		return nil, false
	}
	held := tc.heldProjections
	from := len(*held)
	if tc.projectionLoans(expr, scope, held) {
		return nil, true
	}
	return append([]BorrowID(nil), (*held)[from:]...), len(*held) > from
}

// holdWrappedProjections runs walk as a holder's value walk: the projection
// loans it meets are taken (wrappedProjectionLoans).
func (tc *typeChecker) holdWrappedProjections(walk func() []BorrowID) []BorrowID {
	if tc.heldProjections != nil {
		return walk()
	}
	var held []BorrowID
	tc.heldProjections = &held
	defer func() { tc.heldProjections = nil }()
	return walk()
}
