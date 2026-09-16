package sema

import (
	"fmt"
	"slices"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// A call, an operator, an index or a conversion can hand back a window into what a
// REFERENCE formal points at, and its result type says only `T[]`. Nothing in the
// caller spells the borrow: the operand of `a + b` is borrowed implicitly, and the
// result is typed like any owned array.
//
// The rule is in three parts, and the third is what makes it a language rule rather
// than a coincidence of file order. A function that returns a slice of its own
// reference parameter records that fact about ITSELF -- the escape check already
// computes it and throws it away. A call-like expression keeps which actual fills
// which formal, resolved when the call is typed, so a later rebind cannot change the
// answer. And the two are joined AFTER every body has been walked, by one monotone
// step, so a caller written above its callee is answered exactly as one written
// below it.
//
// What stays allowed is what the direct-slice rule already allows: a window over a
// reference parameter is the caller's storage and outlives this frame. What is
// refused is the same window handed to a frame that owns the array.

// fixedViewFormal is a callee's formal as the refusal quotes it back.
type fixedViewFormal struct {
	name source.StringID
	span source.Span
}

// fixedViewCall is one call-like expression: the declaration it selected, and the
// storage each formal's actual stands for, resolved at the moment it was typed.
type fixedViewCall struct {
	callee symbols.SymbolID
	bases  map[int]symbols.SymbolID
	span   source.Span
}

// fixedViewReturn is a returned call-like expression whose answer is owed until the
// facts are closed. It carries its own function's formals, because the parameter
// stack is gone by the time the answer is read.
type fixedViewReturn struct {
	fn     symbols.SymbolID
	params []symbols.SymbolID
	site   source.Span
	call   fixedViewCall
}

// fixedViewThrough is the call route's half of the refusal: which declaration handed
// the window back, and through which formal.
type fixedViewThrough struct {
	callee     string
	formal     string
	formalSpan source.Span
	call       source.Span
}

// recordFixedViewReturnParam remembers what this function's own return proves about
// it: that it gives back a slice of what formal k points at. The escape check has
// just resolved the base and found it NOT frame-local; a reference parameter is the
// one case where that answer is a fact some caller needs.
func (tc *typeChecker) recordFixedViewReturnParam(base symbols.SymbolID) {
	if tc.fixedViewReturnParams == nil || !base.IsValid() {
		return
	}
	sym := tc.symbolFromID(base)
	if sym == nil || sym.Kind != symbols.SymbolParam || !tc.isReferenceType(tc.bindingType(base)) {
		return
	}
	fnSym := tc.currentFnSym()
	if !fnSym.IsValid() {
		return
	}
	for slot, param := range tc.currentFnParams() {
		if param == base {
			tc.seedFixedViewFormal(fnSym, slot, sym)
			return
		}
	}
}

// seedFixedViewFormal adds one formal to a function's fact, and answers whether it
// was new -- which is what makes the closure below terminate.
func (tc *typeChecker) seedFixedViewFormal(fnSym symbols.SymbolID, slot int, sym *symbols.Symbol) bool {
	formals := tc.fixedViewReturnParams[fnSym]
	if formals == nil {
		formals = make(map[int]fixedViewFormal)
		tc.fixedViewReturnParams[fnSym] = formals
	}
	if _, known := formals[slot]; known {
		return false
	}
	formals[slot] = fixedViewFormal{name: sym.Name, span: sym.Span}
	return true
}

// noteFixedViewReturnCall records a returned call-like expression, directly or
// through the binding that holds its result. The answer is not taken here: taking it
// during the walk would make a program's diagnostics depend on where its functions
// are written.
func (tc *typeChecker) noteFixedViewReturnCall(expr ast.ExprID, span source.Span) {
	if tc.fixedViewReturnParams == nil {
		return
	}
	fnSym := tc.currentFnSym()
	if !fnSym.IsValid() {
		return
	}
	call, ok := tc.fixedViewCallOf(expr)
	if !ok && tc.fixedViewBindingCall != nil {
		if symID := tc.symbolForExpr(tc.unwrapArrayViewExpr(tc.unwrapGroupExpr(expr))); symID.IsValid() {
			call, ok = tc.fixedViewBindingCall[symID]
		}
	}
	if !ok || !call.callee.IsValid() || len(call.bases) == 0 {
		return
	}
	tc.fixedViewReturnCalls = append(tc.fixedViewReturnCalls, fixedViewReturn{
		fn:     fnSym,
		params: append([]symbols.SymbolID(nil), tc.currentFnParams()...),
		site:   span,
		call:   call,
	})
}

// noteFixedViewCallBinding carries a call's provenance to the binding that holds its
// result, so `let v = a + b; return v;` is refused for the reason `return a + b` is.
// A binding bound to anything else drops what it carried, exactly as the slice
// provenance beside it does.
func (tc *typeChecker) noteFixedViewCallBinding(symID symbols.SymbolID, valueExpr ast.ExprID) {
	if tc.fixedViewBindingCall == nil || !symID.IsValid() {
		return
	}
	if call, ok := tc.fixedViewCallOf(valueExpr); ok {
		tc.fixedViewBindingCall[symID] = call
		return
	}
	delete(tc.fixedViewBindingCall, symID)
}

// fixedViewCallOf classifies an expression as a call-like node and maps every formal
// slot to the actual that fills it.
//
// An implicit conversion is asked about FIRST, because recording one on a node
// overwrites that node's own `__to` selection: the conversion is what the program
// runs, so it is the callee this rule must follow.
func (tc *typeChecker) fixedViewCallOf(expr ast.ExprID) (fixedViewCall, bool) {
	if tc.builder == nil || tc.builder.Exprs == nil || tc.result == nil {
		return fixedViewCall{}, false
	}
	expr = tc.unwrapGroupExpr(expr)
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return fixedViewCall{}, false
	}
	span := tc.exprSpan(expr)
	if conv, converted := tc.result.ImplicitConversions[expr]; converted {
		if conv.Kind != ImplicitConversionTo {
			return fixedViewCall{}, false
		}
		return tc.fixedViewCallSlots(tc.result.ToSymbols[expr], span, expr)
	}
	switch node.Kind {
	case ast.ExprBinary:
		data, ok := tc.builder.Exprs.Binary(expr)
		if !ok || data == nil || data.Op == ast.ExprBinaryAssign {
			return fixedViewCall{}, false
		}
		return tc.fixedViewCallSlots(tc.result.MagicBinarySymbols[expr], span, data.Left, data.Right)
	case ast.ExprUnary:
		data, ok := tc.builder.Exprs.Unary(expr)
		if !ok || data == nil {
			return fixedViewCall{}, false
		}
		switch data.Op {
		case ast.ExprUnaryRef, ast.ExprUnaryRefMut, ast.ExprUnaryDeref, ast.ExprUnaryOwn, ast.ExprUnaryAwait:
			return fixedViewCall{}, false
		}
		return tc.fixedViewCallSlots(tc.result.MagicUnarySymbols[expr], span, data.Operand)
	case ast.ExprIndex:
		data, ok := tc.builder.Exprs.Index(expr)
		if !ok || data == nil {
			return fixedViewCall{}, false
		}
		return tc.fixedViewCallSlots(tc.result.IndexSymbols[expr], span, data.Target, data.Index)
	case ast.ExprCast:
		data, ok := tc.builder.Exprs.Cast(expr)
		if !ok || data == nil {
			return fixedViewCall{}, false
		}
		return tc.fixedViewCallSlots(tc.result.ToSymbols[expr], span, data.Value)
	case ast.ExprCall:
		return tc.fixedViewCallArguments(expr, span)
	default:
		return fixedViewCall{}, false
	}
}

// fixedViewCallArguments maps an ordinary call: the receiver fills formal 0 when the
// declaration takes `self`, and the arguments fill the rest. A call with NAMED
// arguments records nothing -- without an exact slot map this rule proves nothing,
// which is what it did before this file existed.
func (tc *typeChecker) fixedViewCallArguments(expr ast.ExprID, span source.Span) (fixedViewCall, bool) {
	call, ok := tc.builder.Exprs.Call(expr)
	if !ok || call == nil || call.HasNamedArgs() {
		return fixedViewCall{}, false
	}
	callee := tc.symbolForExpr(expr)
	sym := tc.symbolFromID(callee)
	if sym == nil || sym.Kind != symbols.SymbolFunction || sym.Signature == nil {
		return fixedViewCall{}, false
	}
	actuals := make([]ast.ExprID, 0, len(call.Args)+1)
	if sym.Signature.HasSelf {
		member, memberOK := tc.builder.Exprs.Member(call.Target)
		if !memberOK || member == nil {
			return fixedViewCall{}, false
		}
		actuals = append(actuals, member.Target)
	}
	for _, arg := range call.Args {
		actuals = append(actuals, arg.Value)
	}
	if len(actuals) != len(sym.Signature.Params) {
		return fixedViewCall{}, false
	}
	return tc.fixedViewCallSlots(callee, span, actuals...)
}

// fixedViewCallSlots resolves each actual to the storage it stands for, now rather
// than later: a rebind after this point must not change what the call was given.
func (tc *typeChecker) fixedViewCallSlots(callee symbols.SymbolID, span source.Span, actuals ...ast.ExprID) (fixedViewCall, bool) {
	if !callee.IsValid() {
		return fixedViewCall{}, false
	}
	call := fixedViewCall{callee: callee, bases: make(map[int]symbols.SymbolID, len(actuals)), span: span}
	for slot, actual := range actuals {
		if base := tc.fixedViewActualBase(actual); base.IsValid() {
			call.bases[slot] = base
		}
	}
	if len(call.bases) == 0 {
		return fixedViewCall{}, false
	}
	return call, true
}

// fixedViewActualBase answers "whose storage does this actual stand for", with the
// same walk a slice target uses -- it strips `&` and `own`, follows a slice of a
// slice and a view binding.
//
// One guard is added that the direct path does not have: a reference LET binding
// names someone ELSE's storage, so its loan root is taken instead, and an unknown
// root drops the slot rather than guessing. Without it `let r: &Arr = p; return r + r;`
// would be refused although the window is the caller's.
func (tc *typeChecker) fixedViewActualBase(actual ast.ExprID) symbols.SymbolID {
	if !actual.IsValid() {
		return symbols.NoSymbolID
	}
	base := tc.fixedViewBaseOfExpr(actual)
	if !base.IsValid() {
		return symbols.NoSymbolID
	}
	sym := tc.symbolFromID(base)
	if sym == nil {
		return symbols.NoSymbolID
	}
	if sym.Kind == symbols.SymbolLet && tc.isReferenceType(tc.bindingType(base)) {
		return tc.loanRootBase(actual)
	}
	return base
}

// resolveFixedViewReturnCalls closes the facts under one monotone step and then
// reports. It runs after `walk_items`, which is what makes the answer independent of
// the order the functions are written in; the step only ever adds a formal, so it
// terminates, and recursion is harmless.
func (tc *typeChecker) resolveFixedViewReturnCalls() {
	if len(tc.fixedViewReturnCalls) == 0 {
		return
	}
	for changed := true; changed; {
		changed = false
		for _, obligation := range tc.fixedViewReturnCalls {
			for slot := range tc.fixedViewReturnParams[obligation.call.callee] {
				base, known := obligation.call.bases[slot]
				if known && tc.propagateFixedViewReturnParam(obligation, base) {
					changed = true
				}
			}
		}
	}
	for _, obligation := range tc.fixedViewReturnCalls {
		tc.reportFixedViewReturnCall(obligation)
	}
}

// propagateFixedViewReturnParam is the transitive step: a function that hands on a
// window into what its OWN reference parameter points at proves about itself exactly
// what its callee proved.
func (tc *typeChecker) propagateFixedViewReturnParam(obligation fixedViewReturn, base symbols.SymbolID) bool {
	sym := tc.symbolFromID(base)
	if sym == nil || sym.Kind != symbols.SymbolParam || !tc.isReferenceType(tc.bindingType(base)) {
		return false
	}
	for slot, param := range obligation.params {
		if param == base {
			return tc.seedFixedViewFormal(obligation.fn, slot, sym)
		}
	}
	return false
}

// reportFixedViewReturnCall refuses one returned call whose callee gives back a
// window into storage this frame owns. Slots are walked in order so the refusal a
// program gets does not depend on map iteration, and one call reports once.
func (tc *typeChecker) reportFixedViewReturnCall(obligation fixedViewReturn) {
	formals := tc.fixedViewReturnParams[obligation.call.callee]
	if len(formals) == 0 {
		return
	}
	slots := make([]int, 0, len(formals))
	for slot := range formals {
		slots = append(slots, slot)
	}
	slices.Sort(slots)
	callee := tc.symbolFromID(obligation.call.callee)
	for _, slot := range slots {
		base, known := obligation.call.bases[slot]
		if !known {
			continue
		}
		storage, frameLocal := tc.frameLocalStorageLabel(base)
		if !frameLocal {
			continue
		}
		sym := tc.symbolFromID(base)
		if sym == nil || callee == nil {
			continue
		}
		through := fixedViewThrough{
			callee:     tc.lookupName(callee.Name),
			formal:     tc.lookupName(formals[slot].name),
			formalSpan: formals[slot].span,
			call:       obligation.call.span,
		}
		tc.reportFixedArrayViewEscape(sym, storage, obligation.site, &through)
		return
	}
}

// reportFixedArrayViewEscape is the one refusal both routes emit: the direct slice
// passes nil, and the call route adds the two notes that say which declaration
// handed the window back and through which formal.
func (tc *typeChecker) reportFixedArrayViewEscape(sym *symbols.Symbol, storage string, span source.Span, through *fixedViewThrough) {
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
	if through != nil {
		b.WithNote(through.call, fmt.Sprintf(
			"'%s' gives back a slice of what its parameter '%s' points at, and here that is '%s'",
			through.callee, through.formal, name))
		if through.formalSpan != (source.Span{}) {
			b.WithNote(through.formalSpan,
				"this parameter is a reference: its referent is the caller's, which is why the slice "+
					"is allowed here and refused there")
		}
	}
	b.WithNote(span,
		"slicing a dynamic array may be returned - the runtime registers that view against its "+
			"base and defers the base's reclamation - but a fixed array has no base header to register against")
	b.WithNote(span,
		"hint: build an owned array first: "+
			"`let mut out: T[] = []; for i: int in 0..(xs.__len() to int) { out.push(xs[i]); }` and return `out`")
	b.Emit()
}
