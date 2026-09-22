package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
)

// The block-end edge for the scopes a statement walk never pushes (RV2-DEBT-365, R-f).
//
// A `let` is freed where the scope that declares it ends, so a task that still holds it at that
// point is left reading storage nobody owns -- and "joined later" is no answer: a handle pushed into
// an outer container inside the scope and drained after it releases a pin whose place is already
// gone. refusePinsOfEndedScope is that edge for a statement block. A block EXPRESSION -- a value
// block, a `compare` arm's body -- and a `compare` arm's own pattern scope are scopes too: the
// resolver gives each one (ScopeOwnerExpr, indexed by buildScopeIndex into exprScopes), but the
// checker never pushes them, so their bindings were no edge at all.
//
// A task handed out as the block's value is not dropped here, and the pins it holds on places that
// outlive the block stay: only a pin on a binding this scope declares is refused, because that
// binding dies here whoever holds the task.

// refusePinsOfEndedBlockExpr is the edge at the end of a block expression.
func (tc *typeChecker) refusePinsOfEndedBlockExpr(id ast.ExprID) {
	for _, scope := range tc.exprScopes[id] {
		tc.refusePinsOfEndedScope(scope)
	}
}

// refusePinsOfEndedArm is the edge at the end of one `compare` arm, for the bindings its pattern
// introduced. A binding the arm takes out of a subject it only BORROWS names storage the owner
// keeps, which outlives the compare, and is spared -- the discriminant the arm's reference rule
// asks (armFreesPayloadBinding), without its drop test: a binding with nothing to drop still dies
// with the arm.
func (tc *typeChecker) refusePinsOfEndedArm(id ast.ExprID, arm int, subjectBorrowed, tupleElementsBorrowed bool) {
	scopes := tc.exprScopes[id]
	if arm < len(scopes) && !tupleElementsBorrowed {
		tc.refusePinsOfEndedScopeExcept(scopes[arm], func(symID symbols.SymbolID) bool {
			return subjectBorrowed && !tc.payloadTakesItsOwnReference(symID)
		})
	}
}
