package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
)

// indexStore is a native or selected `x[i] = v` into a canonical container. The
// evaluated place's owner proves its backing targets (G1); the write is weak.
// Anything unproved leaves the store on its existing refusal.
func (b *returnOriginBody) indexStore(lhs ast.ExprID, owner, rhs returnOriginValue, rhsExpr ast.ExprID, env returnOriginEnv, span source.Span) (returnOriginEnv, bool) {
	if !env.reachable || !rhs.normal {
		return env, false
	}
	container, reason := b.analyzer.indexStoreOperation(b.function, lhs)
	if reason != "" {
		return env, false
	}
	targets, proven := b.backingTargets(container, owner, env, true)
	if !proven {
		return env, false
	}
	return b.storeBackingContents(env, container, targets, rhs, []ast.ExprID{rhsExpr}, span), true
}

// indexStoreOperation certifies the store's typed operands and, when SEMA
// recorded one, its selected `__index_set`. An invalid present entry never falls
// back to the native store, and a place that also records a read is refused.
func (a *returnOriginAnalyzer) indexStoreOperation(caller *returnOriginFunction, lhs ast.ExprID) (returnOriginIndexType, string) {
	u := caller.unit
	in := u.Sema.TypeInterner
	data, ok := u.Builder.Exprs.Index(lhs)
	if !ok || data == nil {
		return returnOriginIndexType{}, "index store lacks its original operands"
	}
	if _, read := u.Sema.IndexSymbols[lhs]; read {
		return returnOriginIndexType{}, "index store also records a selected index read"
	}
	for _, expr := range [...]ast.ExprID{lhs, data.Target, data.Index} {
		if _, converted := u.Sema.ImplicitConversions[expr]; converted {
			return returnOriginIndexType{}, "index store requires its implicit conversion effect transfer"
		}
	}
	container, canonical := returnOriginContainer(in, u.Sema.ExprTypes[data.Target])
	if !canonical || u.Sema.ExprTypes[data.Index] != in.Builtins().Int {
		return returnOriginIndexType{}, "index store requires a scalar index into its canonical container"
	}
	selected, present := u.Sema.IndexSetSymbols[lhs]
	if !present {
		return container, ""
	}
	if !selected.IsValid() {
		return container, "selected index store lacks a valid original symbol"
	}
	fn, reason := a.selectedCallableFunction(u, selected)
	if reason != "" {
		return container, reason
	}
	return container, a.indexSetDeclaration(fn, container)
}

func (a *returnOriginAnalyzer) indexSetDeclaration(fn *returnOriginFunction, actual returnOriginIndexType) string {
	c, u := fn.candidate, fn.unit
	in := u.Sema.TypeInterner
	if c == nil || !c.Builtin || !c.Intrinsic || !c.HasSelf || c.HasBody || c.Async || fn.item.Body.IsValid() ||
		c.SourceKey != "builtin" || c.ModulePath != "core/intrinsics" || u.SourceKey != "core/intrinsics.sg" ||
		c.Name != "__index_set" || len(fn.info.Params) != 3 || len(c.Defaults) != 3 || len(c.Variadic) != 3 ||
		slices.Contains(c.Defaults, true) || slices.Contains(c.Variadic, true) || len(c.TemplateParams) == 0 {
		return "selected index store requires its nonprimitive effect transfer"
	}
	if identity, err := u.owningCallableIdentity(fn.item, fn.symbol, fn.info); err != nil || identity.BodyKey != fn.key || identity.SourceKey != fn.canonicalSourceKey {
		return "selected index store lost its original builtin declaration certificate"
	}
	original, family := returnOriginContainer(in, fn.info.Params[0])
	mutable, reference := returnOriginBackingDescriptor(in, fn.info.Params[0])
	arity := 1
	if actual.family == in.ArrayFixedNominalType() {
		arity = 2
	}
	if !family || !reference || !mutable || original.family != actual.family || c.ReceiverType != original.container ||
		len(c.TemplateParams) != arity || c.ReceiverTemplateArity != arity || original.element != c.TemplateParams[0] ||
		fn.info.Params[1] != in.Builtins().Int || fn.info.Params[2] != c.TemplateParams[0] || fn.info.Result != in.Builtins().Nothing {
		return "selected index store disagrees with its original signature"
	}
	return ""
}

// checkIndexStoreUse answers a finalized `__index_set` use at a store place: the
// same certificate, and element and length arguments bound from its caller.
func (a *returnOriginAnalyzer) checkIndexStoreUse(fn, caller *returnOriginFunction, id ast.ExprID, use ConcreteInstantiationUse) string {
	u := caller.unit
	in := u.Sema.TypeInterner
	container, reason := a.indexStoreOperation(caller, id)
	if reason != "" {
		return reason
	}
	selected, present := u.Sema.IndexSetSymbols[id]
	if chosen, _ := a.selectedCallableFunction(u, selected); !present || chosen != fn || use.CalleeTemplate != fn.candidate.Symbol {
		return "generic index store differs from its current selected declaration"
	}
	return returnOriginContainerUseArgs(in, caller, fn, container, use)
}

// arrayRangeView is G4 for `x[r]` over payload-free elements: a fixed base's
// view borrows its storage, a dynamic one carries the base value's loans.
func (b *returnOriginBody) arrayRangeView(id ast.ExprID, target returnOriginExprResult, env returnOriginEnv, span source.Span) (returnOriginValue, bool) {
	container, reason := b.analyzer.arrayRangeIndex(b.function, id)
	if reason != "" || !b.elementsFreeAt(container, span) {
		return returnOriginValue{}, false
	}
	owner := target.storage
	if container.reference {
		owner = target.value
	}
	if !owner.normal || len(owner.roots) == 0 {
		return returnOriginValue{}, false
	}
	b.checkExpired(owner, span)
	if container.family == b.function.unit.Sema.TypeInterner.ArrayFixedNominalType() {
		return owner.clone(), true
	}
	return b.containerLoans(owner, env, span), true
}

// arrayRangeIndex certifies `x[r]` with an int Range over a canonical container,
// natively or through its selected builtin `__index(Range)`.
func (a *returnOriginAnalyzer) arrayRangeIndex(caller *returnOriginFunction, id ast.ExprID) (returnOriginIndexType, string) {
	u := caller.unit
	in := u.Sema.TypeInterner
	data, ok := u.Builder.Exprs.Index(id)
	rangeType, reason := a.intRangeType()
	if !ok || data == nil || reason != "" {
		return returnOriginIndexType{}, "index requires a non-scalar index transfer"
	}
	container, canonical := returnOriginContainer(in, u.Sema.ExprTypes[data.Target])
	view, dynamic := returnOriginContainer(in, u.Sema.ExprTypes[id])
	if !canonical || u.Sema.ExprTypes[data.Index] != rangeType || !dynamic || view.reference || view.family != in.ArrayNominalType() || view.element != container.element {
		return container, "index requires a non-scalar index transfer"
	}
	for _, expr := range [...]ast.ExprID{id, data.Target, data.Index} {
		if _, converted := u.Sema.ImplicitConversions[expr]; converted {
			return container, "array range index requires its implicit conversion effect transfer"
		}
	}
	if _, selected := u.Sema.IndexSymbols[id]; !selected {
		return container, ""
	}
	fn, reason := a.selectedIndexFunction(u, id)
	if reason != "" {
		return container, reason
	}
	c := fn.candidate
	original, family := returnOriginContainer(in, fn.info.Params[0])
	mutable, reference := returnOriginBackingDescriptor(in, fn.info.Params[0])
	result, typed := returnOriginContainer(in, fn.info.Result)
	arity := 1
	if container.family == in.ArrayFixedNominalType() {
		arity = 2
	}
	if !c.Builtin || !c.Intrinsic || c.HasBody || c.Async || fn.item.Body.IsValid() || c.SourceKey != "builtin" || c.ModulePath != "core/intrinsics" ||
		fn.unit.SourceKey != "core/intrinsics.sg" || c.Name != "__index" || len(fn.info.Params) != 2 || slices.Contains(c.Defaults, true) ||
		!family || !reference || mutable || original.family != container.family || c.ReceiverType != original.container ||
		len(c.TemplateParams) != arity || c.ReceiverTemplateArity != arity || original.element != c.TemplateParams[0] || fn.info.Params[1] != rangeType ||
		!typed || result.reference || result.family != in.ArrayNominalType() || result.element != c.TemplateParams[0] {
		return container, "selected array range index disagrees with its original signature"
	}
	if identity, err := fn.unit.owningCallableIdentity(fn.item, fn.symbol, fn.info); err != nil || identity.BodyKey != fn.key || identity.SourceKey != fn.canonicalSourceKey {
		return container, "selected array range index lost its original builtin declaration certificate"
	}
	return container, ""
}

// checkArrayRangeIndexUse answers a finalized `__index(Range)` use over
// payload-free elements; any other use keeps its scalar index reason.
func (a *returnOriginAnalyzer) checkArrayRangeIndexUse(fn, caller *returnOriginFunction, id ast.ExprID, use ConcreteInstantiationUse) (handled bool, reason string) {
	u := caller.unit
	in := u.Sema.TypeInterner
	container, reason := a.arrayRangeIndex(caller, id)
	if reason == "index requires a non-scalar index transfer" || len(use.TemplateArgs) == 0 || returnOriginTypeShape(in, use.TemplateArgs[0], nil) != returnOriginRefFree {
		return false, ""
	}
	if reason != "" {
		return true, reason
	}
	if chosen, _ := a.selectedCallableFunction(u, u.Sema.IndexSymbols[id]); chosen != fn || use.CalleeTemplate != fn.candidate.Symbol {
		return true, "generic array range index differs from its current selected declaration"
	}
	return true, returnOriginContainerUseArgs(in, caller, fn, container, use)
}

// cloneElementContents is G3: clone(x[i]) over a certified scalar index loads
// the container's contents; a payload-free element clones to nothing borrowed.
func (b *returnOriginBody) cloneElementContents(arg ast.ExprID, argument returnOriginValue, env returnOriginEnv) (returnOriginValue, bool) {
	u := b.function.unit
	for {
		group, ok := u.Builder.Exprs.Group(arg)
		if !ok || group == nil {
			break
		}
		arg = group.Inner
	}
	data, ok := u.Builder.Exprs.Index(arg)
	if !ok || data == nil {
		return returnOriginValue{}, false
	}
	primitive, reason := b.analyzer.indexOperation(b.function, arg)
	container, canonical := returnOriginContainer(u.Sema.TypeInterner, u.Sema.ExprTypes[data.Target])
	if reason != "" || !canonical || primitive.family != container.family {
		return returnOriginValue{}, false
	}
	if b.elementsFree(container) {
		return returnOriginValueOf(), true
	}
	targets, proven := b.backingTargets(container, argument, env, false)
	if !proven {
		return returnOriginValue{}, false
	}
	return b.loadBackingContents(env, container, targets, u.Builder.Exprs.Get(arg).Span), true
}
