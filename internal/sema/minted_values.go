package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// An index expression either READS something a container owns or MINTS a value
// of its own, and two separate sites need that answer to agree.
//
// `projectionReadAliasesItsSource` asks it about a BINDING: `let t = s[[1..3]]`
// is an owner if the slice minted, an alias if it only read. `temp_drops.go`
// asks it about a TEMPORARY nobody names: `s[[3..5]].__len()` needs reclaiming
// if the slice minted, and would be a double free if it did not. When the two
// disagreed about an ARRAY slice the bound form leaked while the temporary form
// did not (RV2-DEBT-206); they agreed about a STRING slice and both leaked
// (RV2-DEBT-219). One predicate is what stops that class of disagreement.

// mintsOwnedValue reports whether this index expression produced a fresh value
// whose only owner is whoever receives it.
//
// Both slice kinds do, for the same reason spelled differently: an array slice
// allocates a view header (`array_alloc_view`), and a string slice allocates a
// whole new string (`rt_string_from_bytes`, called from `rt_string_slice`).
// Element reads do not - `xs[0]` and `s[0]` hand back something the container
// keeps owning, and a character is not even a heap value.
func (tc *typeChecker) mintsOwnedValue(expr ast.ExprID) bool {
	return tc.isArrayViewExpr(expr) || tc.isStringSliceExpr(expr)
}

// isStringSliceExpr reports whether this expression slices a STRING with a
// range, as opposed to indexing one with an integer.
//
// The distinction is the whole point: `s[i]` yields a character and owns
// nothing, while `s[a..b]` allocates. The same shape is tested inside
// `observeMove`, which skips move tracking for it because slicing does not take
// the source away; that site answers a different question about the same
// expression and is left where it is.
func (tc *typeChecker) isStringSliceExpr(expr ast.ExprID) bool {
	if !expr.IsValid() || tc.builder == nil || tc.types == nil || tc.result == nil {
		return false
	}
	idx, ok := tc.builder.Exprs.Index(expr)
	if !ok || idx == nil {
		return false
	}
	container := tc.result.ExprTypes[idx.Target]
	indexType := tc.result.ExprTypes[idx.Index]
	if container == types.NoTypeID || indexType == types.NoTypeID {
		return false
	}
	if tc.valueType(container) != tc.types.Builtins().String {
		return false
	}
	payload, isRange := tc.rangePayload(indexType)
	if !isRange {
		return false
	}
	intType := tc.types.Builtins().Int
	return intType != types.NoTypeID && tc.sameType(payload, intType)
}
