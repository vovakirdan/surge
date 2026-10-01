package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

// A Map index is not a primitive: SEMA selects the core source bodies
// `Map<K, V>.__index(self: &Map<K, V>, key: &K) -> &V` for a read and
// `Map<K, V>.__index_set(self: &mut Map<K, V>, key: K, value: V) -> nothing` for a
// store (core/map.sg), and HIR lowers `m[k]` to a call of the selected one with
// (m, k) as its actuals. Both bodies are ordinary Map operations the analysis
// already certifies (returnOriginMapIntrinsic): the read is get_ref, which hands
// back a slot address in the map's value storage (rt_map.c get_ref), and the store
// is insert, which moves the old value out before the new one in.
//
// The certificate is the checked callee, not a name: the selected declaration is
// the core body by identity and exact signature, its concrete use binds the
// actual K and V, and its checked summary is what the index transfers:
//
//   - read: a result that names only the map formal (slot 0), so the element is a
//     borrow of the indexed map's storage, the owner an array element has; the key
//     is only read for the call and reaches nothing;
//   - store: a result that names nothing, and the weak store insert makes, into
//     proven backing targets of the map operand.
//
// Only a Map whose K and V hold no borrow and keep no storage loan is answered:
// its value storage has no contents to transfer, so neither the loaded element nor
// the stored value can carry an origin the transfer would have to track. Any other
// Map index keeps a refusal by name.

// returnOriginMapIndexBottom is a body with no normal return yet (a provisional
// fixpoint step) or ever: the index does not complete, as a call's bottom summary.
const returnOriginMapIndexBottom = "map index body has no normal return"

const returnOriginMapIndexElement = "map index requires a key and value that hold no borrow and keep no storage loan"

// returnOriginMapIndexDeclaration certifies the core Map `__index` (store false)
// or `__index_set` (store true) body by its original declaration.
func returnOriginMapIndexDeclaration(fn *returnOriginFunction, store bool) bool {
	if fn == nil || fn.info == nil || fn.item == nil || fn.candidate == nil || !returnOriginCoreDeclaration(fn) {
		return false
	}
	c, u := fn.candidate, fn.unit
	in := u.Sema.TypeInterner
	name, arity := "__index", 2
	if store {
		name, arity = "__index_set", 3
	}
	if c.Name != name || fn.name != name || !c.HasSelf || !c.HasBody || !fn.item.Body.IsValid() || c.Async || c.Intrinsic ||
		u.SourceKey != "core/map.sg" || fn.canonicalSourceKey != "core/map.sg" || len(fn.info.Params) != arity ||
		len(c.Defaults) != arity || len(c.Variadic) != arity || slices.Contains(c.Defaults, true) || slices.Contains(c.Variadic, true) ||
		len(c.TemplateParams) != 2 || c.ReceiverTemplateArity != 2 {
		return false
	}
	if identity, err := u.owningCallableIdentity(fn.item, fn.symbol, fn.info); err != nil || identity.BodyKey != fn.key || identity.SourceKey != fn.canonicalSourceKey {
		return false
	}
	key, value := c.TemplateParams[0], c.TemplateParams[1]
	receiver, received := returnOriginMapContainer(in, c.ReceiverType)
	self, typed := returnOriginMapContainer(in, fn.info.Params[0])
	mutable, reference := returnOriginBackingDescriptor(in, fn.info.Params[0])
	if !received || receiver.reference || !typed || !reference || mutable != store || self.container != receiver.container ||
		returnOriginMapKey(in, self) != key || self.element != value {
		return false
	}
	ref := func(id, elem types.TypeID) bool {
		_, typ, ok := returnOriginIndexResolve(in, id)
		return ok && typ.Kind == types.KindReference && !typ.Mutable && typ.Elem == elem
	}
	if store {
		return fn.info.Params[1] == key && fn.info.Params[2] == value && fn.info.Result == in.Builtins().Nothing
	}
	return ref(fn.info.Params[1], key) && ref(fn.info.Result, value)
}

// mapIndexOperation certifies a read (store false) or a pure store place (store
// true) over a canonical Map. handled is false when the target is not a Map.
func (a *returnOriginAnalyzer) mapIndexOperation(caller *returnOriginFunction, id ast.ExprID, store bool) (c returnOriginIndexType, fn *returnOriginFunction, handled bool, reason string) {
	u := caller.unit
	in := u.Sema.TypeInterner
	data, ok := u.Builder.Exprs.Index(id)
	if !ok || data == nil {
		return c, nil, false, ""
	}
	c, handled = returnOriginMapContainer(in, u.Sema.ExprTypes[data.Target])
	if !handled {
		return c, nil, false, ""
	}
	for _, expr := range [...]ast.ExprID{id, data.Target, data.Index} {
		if _, converted := u.Sema.ImplicitConversions[expr]; converted {
			return c, nil, true, "map index requires its implicit conversion effect transfer"
		}
	}
	key := returnOriginMapKey(in, c)
	read, reads := u.Sema.IndexSymbols[id]
	set, sets := u.Sema.IndexSetSymbols[id]
	selected := read
	if store {
		selected = set
	}
	if reads == store || sets != store || !selected.IsValid() || u.Sema.ExprTypes[data.Index] != key {
		return c, nil, true, "map index disagrees with its selected operation"
	}
	fn, reason = a.selectedCallableFunction(u, selected)
	if reason != "" {
		return c, nil, true, reason
	}
	if !returnOriginMapIndexDeclaration(fn, store) {
		return c, nil, true, "map index lacks its original core declaration certificate"
	}
	if !store {
		_, result, typed := returnOriginIndexResolve(in, u.Sema.ExprTypes[id])
		if !typed || result.Kind != types.KindReference || result.Mutable || result.Elem != c.element {
			return c, nil, true, "map index has an inconsistent borrowed element type"
		}
	}
	span := u.Builder.Exprs.Get(id).Span
	use, reason := a.indexUse(fn, caller, id, span)
	if reason != "" {
		return c, nil, true, reason
	}
	if reason = a.checkMapIndexUse(fn, caller, c, &use); reason != "" {
		return c, nil, true, reason
	}
	// The read names the map formal V(0) and its element contents E(0); for a V
	// that holds no borrow and keeps no loan, E(0) is empty at every map.
	summary := a.summaries[fn.key].value
	lends := func(root returnOrigin) bool {
		return root.kind == returnOriginParam && root.param == 0 && root.selector == returnOriginInputValue
	}
	named := func(root returnOrigin) bool {
		return !root.expired && (lends(root) || root.kind == returnOriginParam && root.param == 0 && root.selector == returnOriginInputElements)
	}
	switch {
	case !summary.normal && len(summary.roots) == 0 && len(summary.callables) == 0:
		return c, fn, true, returnOriginMapIndexBottom
	case !summary.normal || len(summary.callables) != 0,
		store && len(summary.roots) != 0,
		!store && (!slices.ContainsFunc(summary.roots, lends) || !slices.ContainsFunc(summary.roots, named) ||
			slices.ContainsFunc(summary.roots, func(root returnOrigin) bool { return !named(root) })):
		return c, nil, true, "map index body lost its checked origin summary"
	}
	return c, fn, true, ""
}

// checkMapIndexUse binds the use's arguments to the actual K and V, concretely,
// and admits only a K and V that hold no borrow and keep no storage loan.
func (a *returnOriginAnalyzer) checkMapIndexUse(fn, caller *returnOriginFunction, c returnOriginIndexType, use *ConcreteInstantiationUse) string {
	in := caller.unit.Sema.TypeInterner
	args := []types.TypeID{returnOriginMapKey(in, c), c.element}
	if use.Caller != (InstanceKey{}) {
		for i := range args {
			args[i] = returnOriginBoundType(args[i], caller.candidate.TemplateParams, use.CallerTemplateArgs)
		}
	}
	if !caller.unit.templateNames(use.CalleeTemplate, fn.candidate.Symbol) || !slices.Equal(use.TemplateArgs, args) || !returnOriginConcreteArgs(in, args) {
		return "generic map index differs from its concrete key and value arguments"
	}
	view := returnOriginBoundView(fn, nil, args)
	for i, arg := range args {
		required := view.requirement(returnOriginNoBorrowedState, fn.candidate.TemplateParams[i])
		if returnOriginTypeShape(in, arg, nil) != returnOriginRefFree || a.loanCarrier(arg) || required.failed() || len(required.atoms) != 0 {
			return returnOriginMapIndexElement
		}
	}
	return ""
}

// mapIndex answers a Map read in index(); handled is false for any other target.
func (b *returnOriginBody) mapIndex(id ast.ExprID, target, out *returnOriginExprResult, span source.Span) bool {
	_, store := b.function.unit.Sema.IndexSetSymbols[id]
	c, fn, handled, reason := b.analyzer.mapIndexOperation(b.function, id, store)
	if !handled {
		return false
	}
	out.storage = returnOriginValue{}
	if reason == returnOriginMapIndexBottom {
		out.value, out.flow.normal = returnOriginValue{}, returnOriginEnv{}
		return true
	}
	if reason == "" && !store {
		reason = b.mapIndexRequirements(fn, c, span)
	}
	if reason != "" {
		b.pending(span, reason)
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		return true
	}
	owner := target.storage
	if c.reference {
		owner = target.value
	}
	if !owner.normal || len(owner.roots) == 0 {
		b.pending(span, "indexed element lacks its evaluated storage owner")
		owner = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	b.checkExpired(owner, span)
	// A pure store place is not read: its owner is what the store writes through.
	out.value, out.storage = owner.clone(), owner.clone()
	return true
}

// mapIndexRequirements inherits the certified body's conditions at the bound K and V.
func (b *returnOriginBody) mapIndexRequirements(fn *returnOriginFunction, c returnOriginIndexType, span source.Span) string {
	in := b.function.unit.Sema.TypeInterner
	view := returnOriginBoundView(fn, b.function, []types.TypeID{returnOriginMapKey(in, c), c.element})
	if b.inheritRequirements(fn, view, span).failed() {
		return "map index body requirements are not discharged"
	}
	return ""
}

// mapIndexStore is `m[k] = v` through the certified core `__index_set`: the weak
// store insert makes into the map operand's proven backing targets. A refusal is
// reported here, by name, so the store never falls back to the place rule.
func (b *returnOriginBody) mapIndexStore(lhs ast.ExprID, owner, rhs returnOriginValue, rhsExpr ast.ExprID, env returnOriginEnv, span source.Span) (returnOriginEnv, bool) {
	c, fn, handled, reason := b.analyzer.mapIndexOperation(b.function, lhs, true)
	if !handled {
		return env, false
	}
	if reason == returnOriginMapIndexBottom {
		return returnOriginEnv{}, true
	}
	if reason == "" {
		reason = b.mapIndexRequirements(fn, c, span)
	}
	targets, proven := b.backingTargets(c, owner, env, true)
	if reason == "" && !proven {
		reason = "map index store lacks its proven backing targets"
	}
	if reason != "" {
		return b.taintExternalCellEffects(env, span, reason), true
	}
	return b.storeBackingContents(env, c, targets, rhs, []ast.ExprID{rhsExpr}, span), true
}

// checkMapIndexSiteUse answers a finalized use at a Map index site with the same
// certificate the body walk applies; handled is false for any other target.
func (a *returnOriginAnalyzer) checkMapIndexSiteUse(fn, caller *returnOriginFunction, expression ast.ExprID) (handled bool, reason string) {
	_, store := caller.unit.Sema.IndexSetSymbols[expression]
	var selected *returnOriginFunction
	_, selected, handled, reason = a.mapIndexOperation(caller, expression, store)
	if reason == returnOriginMapIndexBottom {
		reason = ""
	}
	if handled && reason == "" && selected != fn {
		reason = "generic map index differs from its current selected declaration"
	}
	return handled, reason
}
