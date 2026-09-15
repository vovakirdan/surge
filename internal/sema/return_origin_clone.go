package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// The local use records the typed primitive operation. Its target identifier
// need not have a function type or name the selected operation in ExprSymbols.
func (b *returnOriginBody) deferredClone(id ast.ExprID, call *ast.ExprCallData, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, bool, error) {
	fn, u := b.function, b.function.unit
	span := u.Builder.Exprs.Get(id).Span
	use := u.Sema.DeferredCallableUses[DeferredUseRef{Expr: id, Kind: DeferredCloneCall}]
	var edges []DeferredCallableEdge
	for _, edge := range u.authority.InstantiationGraph.DeferredCallables() {
		if edge.Kind == DeferredCloneCall && ((use != "" && edge.UseID == use) ||
			(fn.candidate != nil && edge.Caller == fn.candidate.Symbol && edge.Witness.SourceKey == u.SourceKey && edge.Witness.Site == span)) {
			edges = append(edges, edge)
		}
	}
	if use == "" && len(edges) == 0 {
		return b.selectedClone(id, call, env, targets)
	}
	reason := "deferred clone lacks its unique original typed edge"
	if len(edges) == 1 {
		var original ast.ExprID
		original, reason = fn.originalClone(&edges[0])
		if reason == "" && original != id {
			reason = "deferred clone disagrees with its original typed call"
		}
	}
	flow := returnOriginFlow{normal: env}
	var argument returnOriginValue
	for _, arg := range call.Args {
		var err error
		flow, err = flow.then(func(next returnOriginEnv) (returnOriginFlow, error) {
			out, err := b.expr(arg.Value, next, targets)
			argument = out.value.clone()
			if _, implicit := u.Sema.ImplicitConversions[arg.Value]; implicit && reason == "" {
				reason = "deferred clone argument needs its resolved conversion transfer"
			}
			return out.flow, err
		})
		if err != nil {
			return returnOriginExprResult{}, true, err
		}
	}
	if !flow.normal.reachable {
		return returnOriginExprResult{flow: flow}, true, nil
	}
	if reason == "" {
		reason = b.analyzer.cloneInstances(fn, &edges[0])
	}
	value := returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	if reason == "" {
		// The typed call has exactly one argument once its original edge is proven.
		if contents, loaded := b.cloneElementContents(call.Args[0].Value, argument, flow.normal); loaded {
			value = contents
		} else {
			value, reason = b.cloneBindingContents(argument, flow.normal, edges[0].Receiver)
		}
	}
	if reason != "" {
		b.pending(span, reason)
	}
	b.checkExpired(value, span)
	return returnOriginExprResult{flow: flow, value: value}, true, nil
}

// ExprID belongs to this original unit; edge.Caller belongs to the published
// canonical vocabulary. Neither can be recovered from another unit's raw IDs.
func (fn *returnOriginFunction) originalClone(edge *DeferredCallableEdge) (ast.ExprID, string) {
	u := fn.unit
	var id ast.ExprID
	for ref, use := range u.Sema.DeferredCallableUses {
		if ref.Kind == DeferredCloneCall && use == edge.UseID {
			if id.IsValid() {
				return ast.NoExprID, "deferred clone lacks its unique original typed use"
			}
			id = ref.Expr
		}
	}
	if !id.IsValid() {
		return id, "deferred clone lacks its original typed use"
	}
	matches := 0
	for _, original := range u.authority.InstantiationGraph.DeferredCallables() {
		if original.Kind == DeferredCloneCall && original.UseID == edge.UseID {
			matches++
		}
	}
	if matches != 1 {
		return id, "deferred clone lacks its unique original typed edge"
	}
	node := u.Builder.Exprs.Get(id)
	call, ok := u.Builder.Exprs.Call(id)
	if fn.candidate == nil || !fn.item.Body.IsValid() || edge.Caller != fn.candidate.Symbol || edge.Witness.Caller != edge.Caller ||
		edge.Witness.SourceKey != u.SourceKey || edge.Kind != DeferredCloneCall || edge.UseID == "" ||
		node == nil || node.Span != edge.Witness.Site || node.Span.File != fn.item.Span.File || node.Span.Start < fn.item.Span.Start || node.Span.End > fn.item.Span.End ||
		!ok || call == nil || len(call.Args) != 1 || call.HasNamedArgs() || len(edge.Args) != 0 || len(edge.ExplicitTypeArgs) != 0 ||
		edge.StaticReceiver || edge.Method != "__clone" || edge.ExpectedResult != edge.Receiver || u.Sema.ExprTypes[id] != edge.ExpectedResult {
		return id, "deferred clone disagrees with its original typed call"
	}
	arg, ok := u.Sema.TypeInterner.Lookup(u.Sema.ExprTypes[call.Args[0].Value])
	if !ok || arg.Kind != types.KindReference || arg.Mutable || arg.Elem != edge.Receiver {
		return id, "deferred clone requires its direct template shared receiver"
	}
	if int(edge.CallerTemplateArity) != len(fn.candidate.TemplateParams) || validateInstantiationBindings(&InstantiationEdge{
		CallerTemplateArity: edge.CallerTemplateArity, CallerBindings: edge.CallerBindings}) != nil {
		return id, "deferred clone lacks its original caller bindings"
	}
	for _, binding := range edge.CallerBindings {
		info, ok := u.Sema.TypeInterner.TypeParamInfo(binding.Param)
		var owner symbols.SymbolID
		if ok && info != nil {
			owner, ok = u.canonicalParameterOwner(symbols.SymbolID(info.Owner))
		}
		if !ok || info == nil || owner != binding.Owner || info.Index != binding.ParamIndex ||
			fn.candidate.TemplateParams[binding.ArgIndex] != binding.Param {
			return id, "deferred clone disagrees with its original parameter owner"
		}
	}
	if !fn.directTemplateParam(edge.Receiver) {
		return id, "deferred clone requires its direct template shared receiver"
	}
	return id, ""
}

// Parameter descriptors retain local owners while graph bindings are merged.
// Only an empty publication shares that vocabulary; otherwise the reverse
// mapping must identify exactly one canonical owner in this original unit.
func (u *returnOriginUnitIndex) canonicalParameterOwner(local symbols.SymbolID) (symbols.SymbolID, bool) {
	if !local.IsValid() {
		return symbols.NoSymbolID, false
	}
	if len(u.Publication.RootToLocalSymbols) == 0 {
		return local, true
	}
	var canonical symbols.SymbolID
	for root, locals := range u.Publication.RootToLocalSymbols {
		if !slices.Contains(locals, local) {
			continue
		}
		if !root.IsValid() || canonical.IsValid() {
			return symbols.NoSymbolID, false
		}
		canonical = root
	}
	return canonical, canonical.IsValid()
}

// Ordinary & evaluation keeps the address in argument and the binding's
// current contents in env. Read those contents after evaluation, without
// reviving an expired owner or evaluating the operand a second time.
func (b *returnOriginBody) cloneBindingContents(argument returnOriginValue, env returnOriginEnv, receiver types.TypeID) (returnOriginValue, string) {
	unknown := returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	if !argument.normal || len(argument.roots) == 0 || len(argument.callables) != 0 {
		return unknown, "deferred clone lacks its live whole-binding referent"
	}
	value := returnOriginValueOf()
	for _, root := range argument.roots {
		if root.kind != returnOriginLocal || root.expired {
			return argument.join(unknown), "deferred clone requires a live local storage referent"
		}
		owner := b.function.unit.Symbols.Table.Symbols.Get(root.binding)
		binding, found := env.bindings[root.binding]
		if owner == nil || owner.Type != receiver || owner.Scope != root.scope || !found || binding.scope != root.scope || !binding.value.normal {
			return unknown, "deferred clone lacks its live whole-binding contents"
		}
		value = value.join(binding.value)
	}
	if len(value.callables) != 0 || slices.ContainsFunc(value.roots, func(root returnOrigin) bool {
		return root.kind == returnOriginUnknown || root.kind == returnOriginCapture || root.expired
	}) {
		return value.join(unknown), "deferred clone contents retain unproved or expired provenance"
	}
	return value, ""
}
