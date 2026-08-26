package sema

import "surge/internal/types"

// convertedBorrowMatches answers whether a value the parameter converts on
// the way in (`print(1)` against `@allow_to s: &string`) can be borrowed:
// the conversion produces a fresh string, and that temporary is what the
// borrow reads, released with the statement -- the same materialization a
// concatenation or a call result already gets. The argument itself has no
// place of its own, which is why the caller asks this before it asks for one.
func (tc *typeChecker) convertedBorrowMatches(elem, innerActual types.TypeID, mutable, allowImplicitTo bool) bool {
	if mutable || !allowImplicitTo || !tc.isStringType(elem) {
		return false
	}
	if tc.isReferenceType(innerActual) || tc.resolveAlias(innerActual) == tc.types.Builtins().String {
		return false
	}
	_, found, _ := tc.tryImplicitConversion(innerActual, elem)
	return found
}

func (tc *typeChecker) collectArgTypes(args []callArg) []types.TypeID {
	if len(args) == 0 {
		return nil
	}
	out := make([]types.TypeID, 0, len(args))
	for _, arg := range args {
		out = append(out, arg.ty)
	}
	return out
}
