package sema

import (
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// pushCompareGuardBindings marks bound as belonging to the compare arm
// whose guard is about to be type-checked: a move that resolves to one
// of these symbols anywhere inside the guard is rejected (see
// observeMove's check and reportCompareGuardMove). Mirrors
// discarded_expr.go's stack shape so nested compares (a guard that
// itself contains a compare) stack correctly.
func (tc *typeChecker) pushCompareGuardBindings(bound []symbols.SymbolID) {
	if tc == nil || len(bound) == 0 {
		return
	}
	tc.compareGuardBindings = append(tc.compareGuardBindings, bound)
}

// popCompareGuardBindings must be called exactly once for every push,
// even when push itself was a no-op (empty bound), so callers can
// unconditionally pair push/pop around guard type-checking.
func (tc *typeChecker) popCompareGuardBindings(bound []symbols.SymbolID) {
	if tc == nil || len(bound) == 0 {
		return
	}
	if n := len(tc.compareGuardBindings); n > 0 {
		tc.compareGuardBindings = tc.compareGuardBindings[:n-1]
	}
}

// isCompareGuardBinding reports whether symID is one of the CURRENTLY
// guarded arm(s)' own pattern bindings — checked across every active
// frame, not just the innermost, so a nested compare's guard can't move
// an enclosing arm's binding either.
func (tc *typeChecker) isCompareGuardBinding(symID symbols.SymbolID) bool {
	if tc == nil || !symID.IsValid() {
		return false
	}
	for _, frame := range tc.compareGuardBindings {
		for _, s := range frame {
			if s == symID {
				return true
			}
		}
	}
	return false
}

// reportCompareGuardMove names the real cause at the earliest point sema
// can see it: the guard tried to move symID, one of ITS OWN arm's
// pattern bindings, out by value. Rust rejects the same shape ("cannot
// move out of value because it is borrowed" during a match guard) for
// the identical reason — a failed guard must leave the scrutinee (and
// this arm's already-extracted payload) intact for whatever runs next.
func (tc *typeChecker) reportCompareGuardMove(symID symbols.SymbolID, span source.Span) {
	name := tc.bindingName(symID)
	tc.report(diag.SemaCompareGuardMovesBinding, span,
		"guard cannot move `%s` out by value: a failed guard falls through to the next arm with the compared value still expected to be intact — borrow it instead (e.g. `len(&%s)` or `&%s`)",
		name, name, name)
}
