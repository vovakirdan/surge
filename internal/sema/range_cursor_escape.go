package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A `Range` produced by `xs.__range()` is a CURSOR into the array's elements,
// not a copy of them. Letting one outlive the frame that owns the array is the
// same mistake as returning a borrow of a local, read through a third
// representation.
//
// It is not a quiet one. `fn build() -> Range<Item>` that pushes into a local
// `Item[]` and returns `out.__range()` prints an arbitrary-precision integer
// hundreds of digits long and then SEGFAULTS on the native lane, while the VM
// prints the right answer — so it is a lane disagreement as well as a crash.
// Valgrind attributes it exactly: the element buffer is reclaimed by `build`
// itself, on the way out, while the returned cursor still points into it.
//
// REGISTERING the cursor instead was measured, not assumed, and turned down:
// the escaping SLICE — the form that IS registered against the base header —
// prints the right answer and still reads the freed base during teardown
// (RV2-DEBT-218). Registration would have moved this from "segfault" to "right
// answer over a silent read of freed storage", which is not a fix.
//
// The allowed set was measured before the rule was written and this code has to
// keep it: a cursor that STAYS in the frame owning the array is correct and
// valgrind-clean, and a cursor over a `&Item[]` REFERENCE PARAMETER, returned,
// is correct too — its referent is the caller's. Both fall out of
// frameLocalStorageLabel rather than being special-cased here.

// recordRangeCursorProvenance remembers which array a `__range()` call cursors.
// Provenance has to be carried rather than re-derived for the same reason the
// fixed-array view carries its own: the result type says `Range<T>`, which no
// longer says whose storage the cursor walks.
func (tc *typeChecker) recordRangeCursorProvenance(callID, receiverExpr ast.ExprID, receiverType types.TypeID) {
	if !callID.IsValid() || tc.rangeCursorExprBase == nil {
		return
	}
	if !tc.isArrayOrFixedType(receiverType) {
		return
	}
	base := tc.rangeCursorBaseOfExpr(receiverExpr)
	if !base.IsValid() {
		return
	}
	tc.rangeCursorExprBase[callID] = base
}

// rangeCursorBaseOfExpr answers "whose storage does this receiver stand for".
//
// A receiver that is itself a VIEW is where the rule stops rather than guesses:
// a view binding's own provenance is only tracked for fixed arrays, so for a
// dynamic one there is nothing to follow and the storage could just as well be
// the caller's. Staying silent there keeps this rule refusing what it can
// prove, which is the same line `checkFixedArrayViewEscapeOnReturn` draws for a
// view of unknown provenance.
func (tc *typeChecker) rangeCursorBaseOfExpr(expr ast.ExprID) symbols.SymbolID {
	if !expr.IsValid() {
		return symbols.NoSymbolID
	}
	inner := tc.unwrapGroupExpr(expr)
	desc, ok := tc.resolvePlace(inner)
	if !ok || !desc.Base.IsValid() {
		return symbols.NoSymbolID
	}
	if base, known := tc.fixedViewBindingBase[desc.Base]; known {
		return base
	}
	if _, isView := tc.arrayViewBindings[desc.Base]; isView {
		return symbols.NoSymbolID
	}
	return desc.Base
}

// bindRangeCursor carries a cursor's provenance to the binding that holds it,
// so `let c = out.__range(); return c;` is refused for the reason
// `return out.__range()` is. A binding bound to anything else drops whatever it
// carried before, because the name now stands for different storage.
func (tc *typeChecker) bindRangeCursor(symID symbols.SymbolID, valueExpr ast.ExprID) {
	if !symID.IsValid() || tc.rangeCursorBindingBase == nil {
		return
	}
	if base, ok := tc.rangeCursorExprBase[tc.unwrapGroupExpr(valueExpr)]; ok {
		tc.rangeCursorBindingBase[symID] = base
		return
	}
	delete(tc.rangeCursorBindingBase, symID)
}

// checkRangeCursorEscapeOnReturn refuses returning a cursor whose array dies
// with this frame.
func (tc *typeChecker) checkRangeCursorEscapeOnReturn(expr ast.ExprID, span source.Span) {
	if tc.rangeCursorExprBase == nil {
		return
	}
	base := tc.rangeCursorEscapeBase(expr)
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
		"cannot return a range over %s '%s': the cursor walks storage this call frame frees",
		storage, name)
	if tc.reporter == nil {
		tc.report(diag.SemaRangeCursorEscapes, span, "%s", headline)
		return
	}
	b := diag.ReportError(tc.reporter, diag.SemaRangeCursorEscapes, span, headline)
	if b == nil {
		return
	}
	if sym.Span != (source.Span{}) {
		b.WithNote(sym.Span, fmt.Sprintf(
			"'%s' lives only inside this function, and `__range()` cursors its elements rather than copying them",
			name))
	}
	b.WithNote(span,
		"a range over a reference parameter may be returned - that array is the caller's - but one over storage this frame owns cannot")
	b.WithNote(span, fmt.Sprintf(
		"hint: return the array itself and let the caller iterate it - `return %s;` with an array result type",
		name))
	b.Emit()
}

// rangeCursorEscapeBase resolves a returned expression to the array its cursor
// walks, or NoSymbolID when the expression is not a cursor this rule tracked.
func (tc *typeChecker) rangeCursorEscapeBase(expr ast.ExprID) symbols.SymbolID {
	inner := tc.unwrapGroupExpr(expr)
	if !inner.IsValid() {
		return symbols.NoSymbolID
	}
	if base, ok := tc.rangeCursorExprBase[inner]; ok {
		return base
	}
	symID := tc.symbolForExpr(inner)
	if !symID.IsValid() {
		return symbols.NoSymbolID
	}
	if base, ok := tc.rangeCursorBindingBase[symID]; ok {
		return base
	}
	return symbols.NoSymbolID
}
