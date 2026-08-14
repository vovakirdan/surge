package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A slice of a FIXED array points into the frame that declares it.
//
// The two array kinds differ in the one way that matters here. A dynamic array
// has a heap header, and `array_register_view` links a view to it so the base's
// reclamation waits for every view to go. A fixed array has no header at all —
// its elements ARE the caller's frame slot, handed to the runtime as a raw
// pointer — so there is nothing to register against and nothing to defer. The
// slice is a bare pointer into storage the frame is about to reclaim.
//
// Letting one out is not a leak, it is a dangling read: the native lane returns
// whatever the slot now holds and exits 0, while the VM catches it as a stale
// reference. Refusing it is the owner's ruling on RV2-DEBT-206, and it follows
// the shape `checkBorrowEscapeOnReturn` already set for a returned borrow.

// recordFixedViewProvenance remembers which FIXED array a slice expression
// points into. Provenance has to be carried rather than re-derived, because a
// view's surface type is `T[]` whatever it was sliced from: by the time the
// return is checked, the type no longer says the storage was a frame slot.
func (tc *typeChecker) recordFixedViewProvenance(viewExpr, targetExpr ast.ExprID, containerType types.TypeID) {
	if !viewExpr.IsValid() || tc.fixedViewExprBase == nil {
		return
	}
	if _, _, ok := tc.arrayFixedInfo(tc.valueType(containerType)); !ok {
		// A dynamic array's view keeps its base alive through the registry.
		return
	}
	base := tc.fixedViewBaseOfExpr(targetExpr)
	if !base.IsValid() {
		return
	}
	tc.fixedViewExprBase[viewExpr] = base
}

// fixedViewBaseOfExpr answers "which binding's storage does this expression
// stand for", following one chain: a slice of a slice inherits the base of the
// slice it came from, because a view's own type has already forgotten.
func (tc *typeChecker) fixedViewBaseOfExpr(expr ast.ExprID) symbols.SymbolID {
	if !expr.IsValid() {
		return symbols.NoSymbolID
	}
	expr = tc.unwrapArrayViewExpr(expr)
	if base, ok := tc.fixedViewExprBase[expr]; ok {
		return base
	}
	desc, ok := tc.resolvePlace(expr)
	if !ok || !desc.Base.IsValid() {
		return symbols.NoSymbolID
	}
	if base, ok := tc.fixedViewBindingBase[desc.Base]; ok {
		return base
	}
	return desc.Base
}

// bindArrayView records what a `let` bound to an array-derived value
// establishes: that the binding IS a view, which array a fixed view points
// into, and which array a `__range()` cursor walks. One call because they are
// one event — a binding that is a view or a cursor without provenance is a
// binding the escape rules cannot reason about.
func (tc *typeChecker) bindArrayView(symID symbols.SymbolID, valueExpr ast.ExprID) {
	tc.markArrayViewBinding(symID, tc.isArrayViewExpr(valueExpr))
	tc.noteFixedViewBinding(symID, valueExpr)
	tc.bindRangeCursor(symID, valueExpr)
}

// noteFixedViewBinding carries provenance from a slice expression to the
// binding that holds it, so `let v = xs[[1..3]]; return v` is refused for the
// same reason `return xs[[1..3]]` is.
func (tc *typeChecker) noteFixedViewBinding(symID symbols.SymbolID, valueExpr ast.ExprID) {
	if !symID.IsValid() || tc.fixedViewBindingBase == nil {
		return
	}
	if base, ok := tc.fixedViewExprBase[tc.unwrapArrayViewExpr(valueExpr)]; ok {
		tc.fixedViewBindingBase[symID] = base
		return
	}
	delete(tc.fixedViewBindingBase, symID)
}

// checkFixedArrayViewEscapeOnReturn refuses returning a view whose backing
// provably dies with this frame.
//
// The storage question is asked exactly as `checkBorrowEscapeOnReturn` asks it,
// and reuses its answer: a local `let` and a by-value parameter die here, while
// a REFERENCE parameter's target lives in the caller. That last case is not a
// technicality — `fn f(xs: &int[4]) -> int[] { return xs[[1..3]] }` is correct
// on both lanes today and must stay allowed.
//
// A view whose provenance is unknown — one a callee returned and this function
// passes on — stays allowed, exactly as an unknown-provenance borrow does. The
// rule refuses what it can prove, not what it cannot rule out.
func (tc *typeChecker) checkFixedArrayViewEscapeOnReturn(expr ast.ExprID, span source.Span) {
	if tc.fixedViewExprBase == nil {
		return
	}
	base := tc.fixedViewEscapeBase(expr)
	if !base.IsValid() {
		return
	}
	sym := tc.symbolFromID(base)
	if sym == nil {
		return
	}
	storage, ok := tc.frameLocalStorageLabel(base)
	if !ok {
		return
	}
	name := tc.lookupName(sym.Name)
	headline := fmt.Sprintf(
		"cannot return a slice of %s '%s': it is a fixed array, so the slice points at this call frame",
		storage, name)
	if tc.reporter == nil {
		tc.report(diag.SemaFixedArrayViewEscapes, span, "%s", headline)
		return
	}
	b := diag.ReportError(tc.reporter, diag.SemaFixedArrayViewEscapes, span, headline)
	if b == nil {
		return
	}
	if sym.Span != (source.Span{}) {
		b.WithNote(sym.Span, fmt.Sprintf(
			"'%s' is a fixed array: its elements ARE this frame's storage, and a slice of it "+
				"carries no header that could keep them alive", name))
	}
	b.WithNote(span,
		"slicing a dynamic array may be returned - the runtime registers that view against its "+
			"base and defers the base's reclamation - but a fixed array has no base header to register against")
	b.WithNote(span,
		"hint: build an owned array first: "+
			"`let mut out: T[] = []; for i: int in 0..(xs.__len() to int) { out.push(xs[i]); }` and return `out`")
	b.Emit()
}

// fixedViewEscapeBase resolves the returned expression to the fixed array it
// points into, or NoSymbolID when the expression is not a fixed view at all.
func (tc *typeChecker) fixedViewEscapeBase(expr ast.ExprID) symbols.SymbolID {
	inner := tc.unwrapArrayViewExpr(tc.unwrapGroupExpr(expr))
	if !inner.IsValid() {
		return symbols.NoSymbolID
	}
	if base, ok := tc.fixedViewExprBase[inner]; ok {
		return base
	}
	symID := tc.symbolForExpr(inner)
	if !symID.IsValid() {
		return symbols.NoSymbolID
	}
	if base, ok := tc.fixedViewBindingBase[symID]; ok {
		return base
	}
	return symbols.NoSymbolID
}
