package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

// A deferred method's body transfer holds only inside a finalized caller instance. Flow can stop
// before the call, so uses, edges and concrete callers are enumerated independently of it.
func (a *returnOriginAnalyzer) checkDeferredMethods() error {
	authority := a.units[0].authority
	pending := func(sourceKey string, site source.Span, reason string) {
		item := ReturnOriginPending{SourceKey: sourceKey, Span: site, Reason: reason}
		if reason != "" && !slices.Contains(a.report.Pending, item) {
			a.report.Pending = append(a.report.Pending, item)
		}
	}
	edges := authority.InstantiationGraph.DeferredCallables()
	for _, unit := range a.units {
		for ref, use := range unit.Sema.DeferredCallableUses {
			if ref.Kind != DeferredMethodCall {
				continue
			}
			if err := a.ctx.Err(); err != nil {
				return err
			}
			span := unit.Builder.Files.Get(unit.FileID).Span
			node := unit.Builder.Exprs.Get(ref.Expr)
			if node != nil {
				span = node.Span
			}
			matches := 0
			for _, edge := range edges {
				if node != nil && edge.Kind == DeferredMethodCall && edge.UseID == use && edge.Witness.SourceKey == unit.SourceKey && edge.Witness.Site == span {
					matches++
				}
			}
			if matches != 1 {
				pending(unit.SourceKey, span, "deferred method lacks its unique original typed edge")
			}
		}
	}
	closure := authority.InstantiationClosure
	for i := range edges {
		edge := &edges[i]
		if edge.Kind != DeferredMethodCall {
			continue
		}
		if err := a.ctx.Err(); err != nil {
			return err
		}
		fn := a.functionForTemplate(edge.Caller)
		switch {
		case fn == nil:
			pending(edge.Witness.SourceKey, edge.Witness.Site, "deferred method lacks its original owning caller")
		case !fn.originalDeferredMethod(edge):
			pending(edge.Witness.SourceKey, edge.Witness.Site, "deferred method lacks its original typed use")
		case closure == nil || authority.InstantiationIdentity == nil:
			pending(edge.Witness.SourceKey, edge.Witness.Site, "deferred method lacks finalized instance authority")
		default:
			for _, instance := range closure.Instances {
				if instance.Template == edge.Caller {
					pending(edge.Witness.SourceKey, edge.Witness.Site, a.methodOutcome(fn, edge, &instance))
				}
			}
		}
	}
	if closure == nil {
		return nil
	}
	for _, call := range closure.ResolvedDeferredCalls {
		if call.Kind != DeferredMethodCall {
			continue
		}
		if err := a.ctx.Err(); err != nil {
			return err
		}
		var edge *DeferredCallableEdge
		matches := 0
		for i := range edges {
			if edges[i].Kind == DeferredMethodCall && edges[i].UseID == call.UseID {
				edge = &edges[i]
				matches++
			}
		}
		if matches != 1 {
			pending(call.SourceKey, call.Site, "deferred method outcome lacks its unique original edge")
			continue
		}
		var fn *returnOriginFunction
		if edge != nil {
			fn = a.functionForTemplate(edge.Caller)
		}
		instance, ok := closure.Lookup(call.Caller)
		if fn == nil || !ok || instance.Template != edge.Caller || call.CallerTemplate != edge.Caller {
			pending(call.SourceKey, call.Site, "deferred method outcome lacks its exact original caller instance")
			continue
		}
		pending(edge.Witness.SourceKey, edge.Witness.Site, a.methodOutcome(fn, edge, &instance))
	}
	return nil
}

// The typed use is the unique call node at the edge's witness, inside its caller, as for originalClone.
func (fn *returnOriginFunction) originalDeferredMethod(edge *DeferredCallableEdge) bool {
	u := fn.unit
	var id ast.ExprID
	for ref, use := range u.Sema.DeferredCallableUses {
		if ref.Kind == DeferredMethodCall && use == edge.UseID {
			if id.IsValid() {
				return false
			}
			id = ref.Expr
		}
	}
	if !id.IsValid() || fn.candidate == nil || edge.Caller != fn.candidate.Symbol || edge.Witness.SourceKey != u.SourceKey {
		return false
	}
	node := u.Builder.Exprs.Get(id)
	return node != nil && node.Kind == ast.ExprCall && node.Span == edge.Witness.Site && node.Span.File == fn.item.Span.File &&
		node.Span.Start >= fn.item.Span.Start && node.Span.End <= fn.item.Span.End
}

// An instance's outcome is its edge substituted through that instance, resolved to formals with no unproved effect.
func (a *returnOriginAnalyzer) methodOutcome(fn *returnOriginFunction, edge *DeferredCallableEdge, instance *InstantiationInstance) string {
	if reason := a.genericInstance(instance.Key, instance.Template, instance.TemplateArgs); reason != "" {
		return "deferred method caller: " + reason
	}
	const disagrees = "deferred method outcome disagrees with its caller binding"
	params := fn.candidate.TemplateParams
	if instance.Template != edge.Caller || len(instance.TemplateArgs) != len(params) || int(edge.CallerTemplateArity) != len(params) ||
		len(edge.CallerBindings) != len(params) || slices.ContainsFunc(edge.CallerBindings, func(binding InstantiationParamBinding) bool {
		return int(binding.ArgIndex) >= len(params) || params[binding.ArgIndex] != binding.Param
	}) {
		return disagrees
	}
	var found *ResolvedDeferredCall
	for _, call := range fn.unit.authority.InstantiationClosure.ResolvedDeferredCalls {
		if call.Caller == instance.Key && call.UseID == edge.UseID {
			if found != nil {
				return "deferred method has duplicate finalized outcomes"
			}
			copy := call
			found = &copy
		}
	}
	if found == nil {
		return "deferred method lacks its finalized outcome"
	}
	in := fn.unit.Sema.TypeInterner
	matches := func(original, actual types.TypeID) bool {
		return matchReturnOriginSourceType(in, original, actual, params, instance.TemplateArgs) == ""
	}
	if found.CallerTemplate != edge.Caller || !slices.Equal(found.CallerTemplateArgs, instance.TemplateArgs) || found.Kind != DeferredMethodCall ||
		found.SourceKey != edge.Witness.SourceKey || found.Site != edge.Witness.Site || found.StaticReceiver != edge.StaticReceiver ||
		!matches(edge.Receiver, found.Receiver) || !matches(edge.ExpectedResult, found.ExpectedResult) || !slices.EqualFunc(edge.Args, found.Args, matches) {
		return disagrees
	}
	if found.Outcome != DeferredCallableResolved || !found.Callee.IsValid() ||
		(edge.StaticReceiver && len(found.CalleeParamTypes) != len(found.Args)) || (!edge.StaticReceiver && len(found.CalleeParamTypes) != len(found.Args)+1) {
		return "deferred method needs its resolved implementation signature"
	}
	if returnOriginCallHasUnprovedEffects(in, found.CalleeParamTypes) {
		return "deferred method may change reference-bearing or callable contents"
	}
	// A body-less implementation has no body that refuses a loan written through a `&mut` formal.
	if callee := a.functionForTemplate(found.Callee); (callee == nil || !callee.item.Body.IsValid()) &&
		(&returnOriginBody{analyzer: a, function: fn}).loanSinkEffects(found.CalleeParamTypes, found.CalleeParamTypes) {
		return "deferred method may change reference-bearing or callable contents"
	}
	return ""
}
