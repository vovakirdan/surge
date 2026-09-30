package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// A reference carried inside a value handed by value -- `bad(Some::<&mut
// string>(firstsm(k)), k)`, `bads(Some::<&string>(firsts(k)), k)` -- is a
// reference argument like the bare one: `bad(firstsm(k), k)` is refused
// because the callee would hold an element of k's buffer and k itself, and
// could grow one under the other. Handed by value it was only moved, so the
// callee read the element after growing the buffer (valgrind: invalid read).
// The wrapper is now noted with the call's other reference arguments
// (noteRefArg), and the argument-order check treats it, and a bare `&mut`
// element, as an earlier argument a later one must not free
// (refuseLaterArgumentOverEarlierElement): `bad2(firstsm(k), appsr(k))`.

// carriesAnyReference: the value's type holds a reference as a union's payload
// (a tuple, record or array cannot: SEM3138), and whether it is `&mut`.
func (tc *typeChecker) carriesAnyReference(t types.TypeID) (carries, mutable bool) {
	if t == types.NoTypeID || tc.types == nil || tc.isReferenceType(t) {
		return false, false
	}
	if carried, ok := tc.carriedReferenceType(t); ok {
		return true, tc.isMutRefType(tc.resolveAlias(carried))
	}
	return false, false
}

// noteWrappedRefArg notes a by-value argument that carries a reference with
// the call's reference arguments.
func (tc *typeChecker) noteWrappedRefArg(expr ast.ExprID, exprType types.TypeID) {
	if tc.mutArgs == nil {
		return
	}
	if carries, mutable := tc.carriesAnyReference(exprType); carries {
		tc.noteRefArg(tc.unwrapGroupExpr(expr), mutable, tc.exprSpan(expr))
	}
}
