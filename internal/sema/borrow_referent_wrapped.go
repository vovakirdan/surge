package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/symbols"
)

// A shared reference reached through an existing reference -- `k.get_ref(&kk)`
// with `k: &mut Map<K, V>`, `first(r)` -- takes a shared loan on the referent
// for the binding that holds it (referentLoansForBinding). Handed to a
// constructor first -- `Some(k.get_ref(&kk))`, `Some::<&string>(first(r))` --
// the walk stopped at the by-value argument, the binding took no loan, and a
// grow of the map through `k` was accepted while the payload still read the
// entry it moved (valgrind: invalid read). The walk now enters a by-value
// argument that carries a shared reference.

// passesCarriedSharedReference: an argument handed by value that carries a
// shared reference, which the call's result may then carry too (a tag
// constructor's payload, a generic identity).
func (tc *typeChecker) passesCarriedSharedReference(param symbols.TypeKey, arg ast.ExprID) bool {
	if strings.HasPrefix(strings.TrimSpace(string(param)), "&") {
		return false
	}
	carried, carries := tc.carriedReferenceType(tc.result.ExprTypes[arg])
	return carries && !tc.isMutRefType(tc.resolveAlias(carried))
}
