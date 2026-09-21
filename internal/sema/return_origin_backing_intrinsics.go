package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

type returnOriginArrayOp uint8

const (
	returnOriginArrayDefault returnOriginArrayOp = iota + 1
	returnOriginArrayLen
	returnOriginArrayReserve
	returnOriginArrayPush
	returnOriginArrayRange
	returnOriginArrayNext
	returnOriginArrayPop
	returnOriginArrayGetMut
)

// coreArrayIntrinsic certifies one retained core array declaration by its
// original body-less builtin declaration, its template arity and its exact
// structural descriptors. A name alone selects nothing.
// Only get_mut carries a declared promise, and only its container slot.
func (a *returnOriginAnalyzer) coreArrayIntrinsic(fn *returnOriginFunction) (returnOriginArrayOp, bool) {
	if fn == nil || fn.info == nil || fn.item == nil || fn.candidate == nil {
		return 0, false
	}
	c, u := fn.candidate, fn.unit
	in := u.Sema.TypeInterner
	if !c.Builtin || !c.Intrinsic || c.HasBody || c.Async || fn.item.Body.IsValid() || c.Name != fn.name ||
		c.SourceKey != "builtin" || c.ModulePath != "core/intrinsics" || u.SourceKey != "core/intrinsics.sg" ||
		len(c.TemplateParams) == 0 || len(c.Defaults) != len(fn.info.Params) || len(c.Variadic) != len(fn.info.Params) ||
		slices.Contains(c.Defaults, true) || slices.Contains(c.Variadic, true) || !(fn.info.ReturnSources().IsAllInputs() || fn.name == "rt_array_get_mut") {
		return 0, false
	}
	if identity, err := u.owningCallableIdentity(fn.item, fn.symbol, fn.info); err != nil || identity.BodyKey != fn.key || identity.SourceKey != fn.canonicalSourceKey {
		return 0, false
	}
	params, result, elem := fn.info.Params, fn.info.Result, c.TemplateParams[0]
	container := func(id types.TypeID, mutable bool) bool {
		ct, ok := returnOriginContainer(in, id)
		m, reference := returnOriginBackingDescriptor(in, id)
		if !ok || !reference || m != mutable || ct.element != elem {
			return false
		}
		info, _ := in.StructInfo(ct.container)
		if ct.family == in.ArrayFixedNominalType() {
			return len(c.TemplateParams) == 2 && len(info.TypeArgs) == 2 && info.TypeArgs[1] == c.TemplateParams[1]
		}
		return len(c.TemplateParams) == 1
	}
	_, lent, resolved := returnOriginIndexResolve(in, result)
	free := !c.HasSelf && c.ReceiverTemplateArity == 0 && len(c.TemplateParams) == 1
	receiver := c.HasSelf && c.ReceiverTemplateArity == len(c.TemplateParams) && len(params) == 1
	switch {
	case fn.name == "default" && free && len(params) == 0 && result == elem:
		return returnOriginArrayDefault, true
	case fn.name == "rt_array_reserve" && free && len(params) == 2 && container(params[0], true) &&
		params[1] == in.Builtins().Uint && result == in.Builtins().Nothing:
		return returnOriginArrayReserve, true
	case fn.name == "rt_array_push" && free && len(params) == 2 && container(params[0], true) &&
		params[1] == elem && result == in.Builtins().Nothing:
		return returnOriginArrayPush, true
	case fn.name == "__len" && receiver && container(params[0], false) && result == in.Builtins().Uint:
		return returnOriginArrayLen, true
	case fn.name == "__range" && receiver && container(params[0], false) && a.rangeInstance(result, elem):
		return returnOriginArrayRange, true
	case fn.name == "next" && receiver && len(c.TemplateParams) == 1 && a.rangeReference(params[0], elem) && returnOriginOptionOf(in, result, elem):
		return returnOriginArrayNext, true
	case fn.name == "rt_array_pop" && free && len(params) == 1 && container(params[0], true) && returnOriginOptionOf(in, result, elem):
		return returnOriginArrayPop, true
	case fn.name == "rt_array_get_mut" && !c.HasSelf && c.ReceiverTemplateArity == 0 && len(params) == 2 && container(params[0], true) &&
		params[1] == in.Builtins().Int && resolved && lent.Kind == types.KindReference && lent.Mutable && lent.Elem == elem &&
		slices.Equal(fn.info.ReturnSources().Slots(), []uint32{0}):
		return returnOriginArrayGetMut, true
	}
	return 0, false
}

// The Range family is the one the certified int range constructor returns.
func (a *returnOriginAnalyzer) rangeFamily(id types.TypeID) bool {
	rangeType, reason := a.intRangeType()
	if reason != "" {
		return false
	}
	in := a.units[0].authority.TypeInterner
	family, found := in.StructInfo(rangeType)
	info, typed := in.StructInfo(returnOriginResolveAlias(in, id))
	return found && typed && family != nil && info != nil && info.Name == family.Name && info.Decl == family.Decl
}

func (a *returnOriginAnalyzer) rangeInstance(id, elem types.TypeID) bool {
	in := a.units[0].authority.TypeInterner
	info, _ := in.StructInfo(returnOriginResolveAlias(in, id))
	return a.rangeFamily(id) && info != nil && slices.Equal(info.TypeArgs, []types.TypeID{elem})
}

func (a *returnOriginAnalyzer) rangeReference(id, elem types.TypeID) bool {
	in := a.units[0].authority.TypeInterner
	_, typ, ok := returnOriginIndexResolve(in, id)
	return ok && typ.Kind == types.KindReference && typ.Mutable && a.rangeInstance(typ.Elem, elem)
}

func returnOriginOptionOf(in *types.Interner, id, elem types.TypeID) bool {
	info, found := in.UnionInfo(returnOriginResolveAlias(in, id))
	return found && info != nil && slices.Equal(info.TypeArgs, []types.TypeID{elem})
}

// applyCoreArrayIntrinsic answers a certified array intrinsic from one frozen
// pre-call environment. An operation it cannot prove is left unhandled, so the
// ordinary opaque-call obligations stay.
func (b *returnOriginBody) applyCoreArrayIntrinsic(op returnOriginArrayOp, id ast.ExprID, slots []returnOriginArgument, actuals []returnOriginValue,
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
	switch op {
	case returnOriginArrayDefault:
		result := u.Sema.ExprTypes[id]
		view := returnOriginView(b.function)
		// A canonical container keeps its existing arm; any other T is answered only
		// when its Defaultable requirement holds, so this never replaces a refusal.
		if c, canonical := returnOriginContainer(in, result); (!canonical || c.reference) && view.requirement(returnOriginDefaultable, result).failed() {
			return returnOriginValue{}, pre, false
		}
		return b.requireDefaultable(view, result, span), pre, true
	case returnOriginArrayLen, returnOriginArrayReserve:
		expr, ok := argument(0)
		if _, canonical := returnOriginContainer(in, u.Sema.ExprTypes[expr]); !ok || !canonical {
			return returnOriginValue{}, pre, false
		}
		return returnOriginValueOf(), pre, true
	case returnOriginArrayPush:
		expr, ok := argument(0)
		stored, storedOK := argument(1)
		c, canonical := returnOriginContainer(in, u.Sema.ExprTypes[expr])
		if !ok || !storedOK || !canonical {
			return returnOriginValue{}, pre, false
		}
		targets, proven := b.backingTargets(c, actuals[0], pre, true)
		if !proven {
			// The store is skipped, so its value keeps the G6 guard calls.go:147 left to it.
			b.storeBackingContents(pre, c, returnOriginBackingTargets{}, actuals[1], []ast.ExprID{stored}, span)
			return returnOriginValue{}, pre, false
		}
		return returnOriginValueOf(), b.storeBackingContents(pre, c, targets, actuals[1], []ast.ExprID{stored}, span), true
	case returnOriginArrayPop, returnOriginArrayGetMut:
		// pop moves one element out (a weak read, never a kill); get_mut lends a slot of the storage.
		expr, ok := argument(0)
		c, canonical := returnOriginContainer(in, u.Sema.ExprTypes[expr])
		targets, proven := b.backingTargets(c, actuals[0], pre, true)
		switch {
		case !ok || !canonical || !proven && (op == returnOriginArrayGetMut || !b.loanElement(c)):
			return returnOriginValue{}, pre, false
		case !proven: // a loan element without a target set has no loans to load (as legacyBackingValue)
			b.pending(span, returnOriginCursorLoanElement)
			return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}), pre, true
		case op == returnOriginArrayGetMut:
			return actuals[0].clone(), pre, true
		case b.loanElement(c): // I1: a moved-out array or cursor keeps the loans its container's value records
			loans := returnOriginTargetLoans(pre, targets)
			if b.erasedType(u.Sema.ExprTypes[id]) && localLoan(loans) { // a formal's L(slot) is guarded at each caller
				b.pending(span, "storage loan would be discarded by a payload-free value")
			}
			return loans, pre, true
		}
		return b.loadBackingContents(pre, c, targets, span), pre, true
	case returnOriginArrayRange:
		// A cursor over reference-bearing elements stays on its existing refusal.
		expr, ok := argument(0)
		c, canonical := returnOriginContainer(in, u.Sema.ExprTypes[expr])
		if !ok || !canonical || !actuals[0].normal || len(actuals[0].roots) == 0 || !b.elementsFreeAt(c, span) {
			return returnOriginValue{}, pre, false
		}
		if c.family == in.ArrayFixedNominalType() {
			return actuals[0].clone(), pre, true
		}
		// A native dynamic cursor does not retain the base header, so it keeps both.
		return actuals[0].join(b.containerLoans(actuals[0], pre, span)), pre, true
	case returnOriginArrayNext:
		// A step copies the walked element out of its base and never borrows the cursor variable.
		elem, typed := returnOriginOptionElement(in, u.Sema.ExprTypes[id])
		if _, ok := argument(0); !ok || !typed {
			return returnOriginValue{}, pre, false
		}
		switch {
		case b.analyzer.loanCarrier(elem): // a copied array or cursor handle keeps its storage loans
			b.pending(span, returnOriginCursorLoanElement)
			return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}), pre, true
		case returnOriginView(b.function).shape(elem) == returnOriginRefFree, b.freeTemplateElement(elem, span):
			return returnOriginValueOf(), pre, true
		}
		return returnOriginValue{}, pre, false
	}
	return returnOriginValue{}, pre, false
}

// returnOriginContainerUseArgs binds a container operation's element and length
// arguments from its caller and requires them concrete.
func returnOriginContainerUseArgs(in *types.Interner, caller, fn *returnOriginFunction, container returnOriginIndexType, use ConcreteInstantiationUse) string {
	info, _ := in.StructInfo(container.container)
	bound := func(arg types.TypeID) types.TypeID {
		if use.Caller == (InstanceKey{}) {
			return arg
		}
		return returnOriginBoundType(arg, caller.candidate.TemplateParams, use.CallerTemplateArgs)
	}
	if info == nil || len(use.TemplateArgs) != len(fn.candidate.TemplateParams) || len(info.TypeArgs) < len(use.TemplateArgs) ||
		use.TemplateArgs[0] != bound(container.element) {
		return "generic container operation differs from its concrete element arguments"
	}
	for i, arg := range use.TemplateArgs {
		if _, ok := in.Lookup(arg); !ok || types.ContainsGenericParam(in, arg) || (i > 0 && arg != bound(info.TypeArgs[i])) {
			return "generic container operation requires its concrete element or length"
		}
	}
	return ""
}

// requireDefaultable records a Defaultable condition for a canonical container
// default; a failure at the flow site keeps its value Unknown.
func (b *returnOriginBody) requireDefaultable(view returnOriginTypeView, result types.TypeID, span source.Span) returnOriginValue {
	condition := returnOriginCondition{body: b.function.key, site: span, kind: returnOriginDefaultable, subject: result, view: view.clone()}
	b.conditions = append(b.conditions, condition)
	required := view.requirement(condition.kind, result)
	b.required = b.required.join(required)
	if required.failed() {
		b.pending(span, "default result is not proven Defaultable")
		return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	return returnOriginValueOf()
}

// checkBackingIntrinsicUse answers a finalized use of a certified intrinsic by
// its identity; only a container default checks its concrete Defaultable.
func (a *returnOriginAnalyzer) checkBackingIntrinsicUse(fn *returnOriginFunction, use ConcreteInstantiationUse) (handled bool, reason string) {
	op, certified := a.coreArrayIntrinsic(fn)
	if !certified {
		return false, ""
	}
	in := fn.unit.Sema.TypeInterner
	switch op {
	case returnOriginArrayDefault:
		if len(use.TemplateArgs) != 1 {
			return false, ""
		}
		// `coreArrayIntrinsic` pins `result == elem` (:56), so the requirement over the
		// declared result and the container test over the argument name one type.
		required := returnOriginBoundView(fn, nil, use.TemplateArgs).requirement(returnOriginDefaultable, fn.info.Result)
		if c, canonical := returnOriginContainer(in, use.TemplateArgs[0]); (!canonical || c.reference) && required.failed() {
			return false, ""
		}
		if required.failed() {
			return true, "default result is not proven Defaultable"
		}
		return true, ""
	case returnOriginArrayLen, returnOriginArrayReserve, returnOriginArrayPush, returnOriginArrayPop, returnOriginArrayGetMut:
		return true, ""
	case returnOriginArrayRange, returnOriginArrayNext:
		if len(use.TemplateArgs) == 0 || returnOriginTypeShape(in, use.TemplateArgs[0], nil) != returnOriginRefFree {
			return false, ""
		}
		if op == returnOriginArrayNext && a.loanCarrier(use.TemplateArgs[0]) {
			return true, returnOriginCursorLoanElement
		}
		return true, ""
	}
	return false, ""
}
