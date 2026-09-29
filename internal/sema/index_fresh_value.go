package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// indexYieldsFreshValue reports whether `x[i]` is a call whose selected
// `__index` answers a value rather than a reference into x.
//
// resolvePlace reads every index as an element of its target, which is right
// for the borrow table and for a built-in element (`__index` returns `&T`
// there). A user `__index(self: &Arr, window: Range<int>) -> uint64[]` hands back
// a value it built: taking that value takes nothing out of `a`, so the move
// gates must not report it as an element leaving its container.
func (tc *typeChecker) indexYieldsFreshValue(expr ast.ExprID) bool {
	if tc.builder == nil || tc.types == nil || tc.result == nil {
		return false
	}
	expr = tc.unwrapGroups(expr)
	if node := tc.builder.Exprs.Get(expr); node == nil || node.Kind != ast.ExprIndex {
		return false
	}
	selected, ok := tc.result.IndexSymbols[expr]
	if !ok || !selected.IsValid() {
		return false
	}
	result := tc.result.ExprTypes[expr]
	if result == types.NoTypeID {
		return false
	}
	typ, ok := tc.types.Lookup(tc.resolveAlias(result))
	return ok && typ.Kind != types.KindReference
}
