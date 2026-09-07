package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
)

// The crossing capture gate asks a different question about a view than every
// other rule in this package, and the difference is who catches a wrong answer.
//
// A resize rule, a temporary's reclaim decision and the range-cursor escape rule
// all act on their answer with nothing behind them: say "view" where there is
// none and a correct program stops compiling or a live block gets freed twice;
// say "not a view" where there is one and a cursor walks freed storage. They
// read the narrow fact -- what the LAST binding of the name established -- and
// it is withdrawn by an assignment (type_array_view.go).
//
// The gate is the one rule that can afford to be wrong in one direction,
// because every crossing of a dynamic array hands its header to
// `rt_array_unshare_walk`, which asks the view registry and refuses a view -- or
// a base some live view still reads -- by name. So the gate asks MAY: does any
// path reaching this capture bind a window onto another array's buffer? A yes
// it cannot justify costs a compile error where the runtime would have let the
// program run; a no costs nothing, because the runtime still refuses.
//
// Hence a second map, joined and never withdrawn, that only the gate reads.

// markArrayViewMayBinding adds to the never-withdrawn fact.
//
// Withdrawing it would clear a live view recorded on a branch that never ran:
// `let mut v: int[] = base[[1..3]]; if flag { v = [9, 9]; }` with `flag` false
// crossed into an `on` body and the destination shard wrote 777 and 888 into
// `base`, measured at 2 and at 8 shards. So a rebind that IS a view adds the
// fact and a rebind that is not leaves it alone: the join of the branches,
// computed by never taking the meet.
func (tc *typeChecker) markArrayViewMayBinding(symID symbols.SymbolID, mayBeView bool) {
	if !symID.IsValid() || !mayBeView || tc.arrayViewMayBindings == nil {
		return
	}
	tc.arrayViewMayBindings[symID] = struct{}{}
}

// isArrayViewBinding answers about the BINDING rather than an expression: may
// this symbol name a window onto another array's buffer?
//
// The crossing gate needs the binding form because a capture is a name, not the
// expression that produced the value: the body mentions `v`, and what made `v` a
// view happened at its `let`, statements earlier.
//
// It is an honest yes and a doubtful no. A view laundered through a call's
// return value, a struct field or a parameter never gets the marker, and neither
// does one written into a holder by `xs.push(v)`, `xs[0] = v` or `pair.0 = v` --
// so a false here means "not one this checker can see", never "not a view".
// What stands behind that doubt is the runtime.
func (tc *typeChecker) isArrayViewBinding(symID symbols.SymbolID) bool {
	if !symID.IsValid() || tc.arrayViewMayBindings == nil {
		return false
	}
	_, ok := tc.arrayViewMayBindings[symID]
	return ok
}

// mayBeArrayView is the widest honest reading of one expression, and the only
// thing that feeds the gate's map.
//
// Beyond what `isArrayViewExpr` will say it adds the two shapes whose yes is a
// guess: an element read back out of a value that holds a view, where which slot
// a subscript names is not a question this checker answers, and a choice whose
// head lies about its arms -- `compare flag { true => base[[1..3]]; false =>
// base[[0..2]]; }` is a `compare` node every arm of which windows `base`, and
// reading only the head let that value cross into an `on` body and the
// destination shard write through into `base`, measured at 2 and at 8 shards.
func (tc *typeChecker) mayBeArrayView(expr ast.ExprID) bool {
	if !expr.IsValid() {
		return false
	}
	if tc.isArrayViewExpr(expr) {
		return true
	}
	expr = tc.unwrapArrayViewExpr(expr)
	if symID := tc.symbolForExpr(expr); tc.isArrayViewBinding(symID) {
		return true
	}
	if tc.arrayViewReadOutOfAHolder(expr) {
		return true
	}
	for _, arm := range tc.arrayViewChoiceArms(expr) {
		if tc.mayBeArrayView(arm) {
			return true
		}
	}
	return false
}
