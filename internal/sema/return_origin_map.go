package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

// A Map is a backing kind. Its backing is the value storage of a Map formal or an
// owning local, read and written with the array rules at element = V; keys are never
// tracked, and every key that flows into storage or a result records NoBorrowedState(K).
// Runtime: rt_map.c get_ref/get_mut :302–328 hand back a slot address and transfer
// nothing; insert :337–370 moves the old value out before the new one in; remove
// :375–400 moves the value out and relocates the last entry, destroying nothing;
// keys :414–475 builds fresh owning copies. VM intrinsic_map.go :117–263, :265–392,
// :394–507, :509–560. A changed declaration fails the certificate; re-read the
// runtime before widening.

type returnOriginMapOp uint8

const (
	returnOriginMapNew returnOriginMapOp = iota + 1
	returnOriginMapLen
	returnOriginMapContains
	returnOriginMapGetRef
	returnOriginMapGetMut
	returnOriginMapInsert
	returnOriginMapRemove
	returnOriginMapKeys
)

// returnOriginMapContainer answers the canonical core Map after alias resolution,
// reached directly or through one outer reference; its element is the value type V.
func returnOriginMapContainer(in *types.Interner, id types.TypeID) (returnOriginIndexType, bool) {
	var out returnOriginIndexType
	id, typ, ok := returnOriginIndexResolve(in, id)
	if ok && typ.Kind == types.KindReference {
		out.reference = true
		id, _, ok = returnOriginIndexResolve(in, typ.Elem)
	}
	info, found := in.StructInfo(id)
	base, based := in.StructInfo(in.MapNominalType())
	if !ok || !found || !based || info == nil || base == nil || info.Name != base.Name || info.Decl != base.Decl || len(info.TypeArgs) != 2 {
		return returnOriginIndexType{}, false
	}
	out.container, out.family, out.element = id, in.MapNominalType(), info.TypeArgs[1]
	_, present := in.Lookup(out.element)
	return out, present
}

// returnOriginBackingContainer is a canonical Array or ArrayFixed, or a canonical Map.
// Only the roster, G1, the checked call and the call-site container read it; views,
// cursors, the index store and loanCarrier keep returnOriginContainer.
func returnOriginBackingContainer(in *types.Interner, id types.TypeID) (returnOriginIndexType, bool) {
	if c, canonical := returnOriginContainer(in, id); canonical {
		return c, true
	}
	return returnOriginMapContainer(in, id)
}

// returnOriginMapKey is a Map container's key type K, in the vocabulary of the actual.
func returnOriginMapKey(in *types.Interner, c returnOriginIndexType) types.TypeID {
	if info, found := in.StructInfo(c.container); found && info != nil && len(info.TypeArgs) == 2 {
		return info.TypeArgs[0]
	}
	return types.NoTypeID
}

// returnOriginMapIntrinsic certifies one retained core Map declaration by its original
// body-less declaration and its exact structural descriptors. Only get_ref and
// get_mut carry a declared promise, and only their map slot.
func returnOriginMapIntrinsic(fn *returnOriginFunction) (returnOriginMapOp, bool) {
	if !returnOriginCoreIntrinsicDeclaration(fn, 2, 0) || fn.candidate.HasSelf || fn.candidate.ReceiverType != types.NoTypeID {
		return 0, false
	}
	in := fn.unit.Sema.TypeInterner
	p, result, key, value := fn.info.Params, fn.info.Result, fn.candidate.TemplateParams[0], fn.candidate.TemplateParams[1]
	all, lent := fn.info.ReturnSources().IsAllInputs(), slices.Equal(fn.info.ReturnSources().Slots(), []uint32{0})
	owned := func(id types.TypeID, reference, mutable bool) bool {
		c, ok := returnOriginMapContainer(in, id)
		_, outer, _ := returnOriginIndexResolve(in, id)
		info, _ := in.StructInfo(c.container)
		return ok && c.reference == reference && (!reference || outer.Mutable == mutable) && info != nil && slices.Equal(info.TypeArgs, []types.TypeID{key, value})
	}
	ref := func(id, elem types.TypeID, mutable bool) bool {
		_, typ, ok := returnOriginIndexResolve(in, id)
		return ok && typ.Kind == types.KindReference && typ.Mutable == mutable && typ.Elem == elem
	}
	lends := func(mutable bool) bool {
		elem, typed := returnOriginOptionElement(in, result)
		return typed && ref(elem, value, mutable)
	}
	keys, keyed := returnOriginContainer(in, result)
	switch {
	case fn.name == "rt_map_new" && all && len(p) == 0 && owned(result, false, false):
		return returnOriginMapNew, true
	case fn.name == "rt_map_len" && all && len(p) == 1 && owned(p[0], true, false) && result == in.Builtins().Uint:
		return returnOriginMapLen, true
	case fn.name == "rt_map_contains" && all && len(p) == 2 && owned(p[0], true, false) && ref(p[1], key, false) && result == in.Builtins().Bool:
		return returnOriginMapContains, true
	case fn.name == "rt_map_get_ref" && lent && len(p) == 2 && owned(p[0], true, false) && ref(p[1], key, false) && lends(false):
		return returnOriginMapGetRef, true
	case fn.name == "rt_map_get_mut" && lent && len(p) == 2 && owned(p[0], true, true) && ref(p[1], key, false) && lends(true):
		return returnOriginMapGetMut, true
	case fn.name == "rt_map_insert" && all && len(p) == 3 && owned(p[0], true, true) && p[1] == key && p[2] == value && returnOriginOptionOf(in, result, value):
		return returnOriginMapInsert, true
	case fn.name == "rt_map_remove" && all && len(p) == 2 && owned(p[0], true, true) && ref(p[1], key, false) && returnOriginOptionOf(in, result, value):
		return returnOriginMapRemove, true
	case fn.name == "rt_map_keys" && all && len(p) == 1 && owned(p[0], true, false) && keyed && !keys.reference &&
		keys.family == in.ArrayNominalType() && keys.element == key:
		return returnOriginMapKeys, true
	}
	return 0, false
}

// applyCoreMapIntrinsic answers a certified Map intrinsic from one frozen pre-call
// environment with the array rules at element = V; like push, argument 0 may be an
// implicitly borrowed owned map, whose actual is its storage. An operation it cannot
// prove is left unhandled, so the ordinary opaque-call obligations stay.
func (b *returnOriginBody) applyCoreMapIntrinsic(op returnOriginMapOp, id ast.ExprID, slots []returnOriginArgument, actuals []returnOriginValue,
	pre returnOriginEnv, span source.Span,
) (value returnOriginValue, env returnOriginEnv, handled bool) {
	u := b.function.unit
	in := u.Sema.TypeInterner
	argument := func(i int) (ast.ExprID, bool) {
		if i >= len(slots) || slots[i].defaulted || len(slots[i].exprs) != 1 {
			return ast.NoExprID, false
		}
		_, converted := u.Sema.ImplicitConversions[slots[i].exprs[0]]
		return slots[i].exprs[0], !converted
	}
	if op == returnOriginMapNew {
		return returnOriginValueOf(), pre, true // an empty fresh map
	}
	expr, ok := argument(0)
	c, isMap := returnOriginMapContainer(in, u.Sema.ExprTypes[expr])
	if !ok || !isMap {
		return returnOriginValue{}, pre, false
	}
	key, view, unknown := returnOriginMapKey(in, c), returnOriginView(b.function), returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	switch op {
	case returnOriginMapLen, returnOriginMapContains:
		return returnOriginValueOf(), pre, true
	case returnOriginMapKeys:
		if b.analyzer.loanCarrier(key) { // A1: a copied key array or cursor keeps its storage loans
			b.pending(span, returnOriginCursorLoanElement)
			return unknown, pre, true
		}
		return b.requireOpaqueState(view, key, span), pre, true // fresh owning key copies
	}
	stored, storedOK := argument(2)
	targets, proven := b.backingTargets(c, actuals[0], pre, op != returnOriginMapGetRef)
	if op == returnOriginMapInsert && storedOK && !proven {
		// The store is skipped, so its value keeps the G6 guard calls.go leaves to it.
		b.storeBackingContents(pre, c, returnOriginBackingTargets{}, actuals[2], []ast.ExprID{stored}, span)
	}
	switch {
	case op == returnOriginMapInsert && !storedOK, !proven && (op == returnOriginMapGetRef || op == returnOriginMapGetMut || !b.loanElement(c)):
		return returnOriginValue{}, pre, false
	case !proven: // a loan element without a target set has no loans to load
		b.pending(span, returnOriginCursorLoanElement)
		return unknown, pre, true
	}
	read := b.loadBackingContents(pre, c, targets, span)
	if b.loanElement(c) { // I1: a moved-out array or cursor keeps the loans its map's value records
		read = returnOriginTargetLoans(pre, targets)
		if b.erasedType(u.Sema.ExprTypes[id]) && localLoan(read) { // a formal's L(slot) is guarded at each caller
			b.pending(span, "storage loan would be discarded by a payload-free value")
		}
	}
	switch op {
	case returnOriginMapGetRef, returnOriginMapGetMut:
		return actuals[0].join(read), pre, true
	case returnOriginMapRemove:
		return read, pre, true // moved out, never killed
	}
	if b.analyzer.loanCarrier(key) { // A1
		b.pending(span, returnOriginCursorLoanElement)
		read = unknown
	}
	// insert: the old value from PRE, a weak store of the new one, and NoBorrowedState(K).
	return read.join(b.requireOpaqueState(view, key, span)), b.storeBackingContents(pre, c, targets, actuals[2], []ast.ExprID{stored}, span), true
}

// checkMapIntrinsicUse answers a finalized use of a certified Map intrinsic by its
// identity. A use has no targets, so a value or key that is a loan carrier Pends
// first, as defence in depth: every route that stores a loan into a Map value is
// refused where it enters. A stored or copied key must then be NoBorrowedState.
func (a *returnOriginAnalyzer) checkMapIntrinsicUse(fn *returnOriginFunction, use ConcreteInstantiationUse) (handled bool, reason string) {
	op, certified := returnOriginMapIntrinsic(fn)
	if !certified || len(use.TemplateArgs) != 2 {
		return false, ""
	}
	keyed := op == returnOriginMapInsert || op == returnOriginMapKeys
	switch {
	case (op == returnOriginMapInsert || op == returnOriginMapRemove) && a.loanCarrier(use.TemplateArgs[1]),
		keyed && a.loanCarrier(use.TemplateArgs[0]):
		return true, returnOriginCursorLoanElement
	case keyed && returnOriginBoundView(fn, nil, use.TemplateArgs).requirement(returnOriginNoBorrowedState, fn.candidate.TemplateParams[0]).failed():
		return true, "opaque result type may carry borrowed state"
	}
	return true, ""
}
