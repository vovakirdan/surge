package sema

import (
	"slices"

	"surge/internal/types"
)

// A universal body transfer is conditional on every current clone instance.
// BuiltinCopy creates a resolved outcome but no callee UseSite of its own.
func (a *returnOriginAnalyzer) cloneInstances(fn *returnOriginFunction, edge *DeferredCallableEdge) string {
	closure := fn.unit.authority.InstantiationClosure
	if closure == nil || fn.unit.authority.InstantiationIdentity == nil {
		return "deferred clone lacks finalized instance authority"
	}
	for _, instance := range closure.Instances {
		if instance.Template == edge.Caller {
			if reason := a.cloneOutcome(fn, edge, &instance); reason != "" {
				return reason
			}
		}
	}
	return ""
}

func (a *returnOriginAnalyzer) cloneOutcome(fn *returnOriginFunction, edge *DeferredCallableEdge, instance *InstantiationInstance) string {
	if reason := a.genericInstance(instance.Key, instance.Template, instance.TemplateArgs); reason != "" {
		return "deferred clone caller: " + reason
	}
	if instance.Template != edge.Caller || len(instance.TemplateArgs) != int(edge.CallerTemplateArity) {
		return "deferred clone outcome disagrees with its caller binding"
	}
	var receiver types.TypeID
	for _, binding := range edge.CallerBindings {
		if binding.Param == edge.Receiver && int(binding.ArgIndex) < len(instance.TemplateArgs) {
			receiver = instance.TemplateArgs[binding.ArgIndex]
		}
	}
	in := fn.unit.Sema.TypeInterner
	if _, ok := in.Lookup(receiver); !ok || receiver == types.NoTypeID || types.ContainsGenericParam(in, receiver) {
		return "deferred clone needs its exact concrete receiver binding"
	}
	var found *ResolvedDeferredCall
	for _, call := range fn.unit.authority.InstantiationClosure.ResolvedDeferredCalls {
		if call.Caller == instance.Key && call.UseID == edge.UseID {
			if found != nil {
				return "deferred clone has duplicate finalized outcomes"
			}
			copy := call
			found = &copy
		}
	}
	if found == nil {
		return "deferred clone lacks its finalized outcome"
	}
	if found.CallerTemplate != edge.Caller || !slices.Equal(found.CallerTemplateArgs, instance.TemplateArgs) || found.Kind != DeferredCloneCall ||
		found.SourceKey != edge.Witness.SourceKey || found.Site != edge.Witness.Site || found.Receiver != receiver || found.ExpectedResult != receiver ||
		found.StaticReceiver || len(found.Args) != 0 {
		return "deferred clone outcome disagrees with its caller binding"
	}
	switch found.Outcome {
	case DeferredCallableBuiltinCopy:
		if !in.IsCopy(receiver) || found.CalleeKey != "builtin/copy" || found.Callee.IsValid() || len(found.CalleeTemplateArgs) != 0 ||
			len(found.CalleeParamTypes) != 0 || found.CalleeResultType != types.NoTypeID {
			return "deferred clone has an invalid builtin Copy outcome"
		}
		if returnOriginFnInfo(in, receiver) != nil || returnOriginTypeShape(in, receiver, nil) == returnOriginShapeUnknown {
			return "deferred clone concrete contents need their callable or payload origin transfer"
		}
		return ""
	case DeferredCallableResolved:
		return a.resolvedCloneOutcome(found, receiver)
	default:
		return "deferred clone needs its selected non-Copy body and effect transfer"
	}
}

// A non-Copy clone is allowed only through the exact implementation selected by
// the finalized closure. The generic caller still reads the original binding's
// contents, which is a safe upper bound for a clone whose body returns either a
// fresh value or data reached through its sole shared receiver. Any other source
// remains Pending in the selected body's own summary and is rejected here too.
func (a *returnOriginAnalyzer) resolvedCloneOutcome(found *ResolvedDeferredCall, receiver types.TypeID) string {
	const unresolved = "deferred clone needs its selected non-Copy body and effect transfer"
	if found == nil || !found.Callee.IsValid() || found.CalleeKey == "" || len(found.CalleeTemplateArgs) != 0 ||
		len(found.CalleeParamTypes) != 1 || found.CalleeResultType != receiver {
		return unresolved
	}
	callee := a.functionForTemplate(found.Callee)
	if callee == nil || callee.candidate == nil || callee.info == nil || callee.key != found.CalleeKey ||
		callee.candidate.Symbol != found.Callee || callee.candidate.Name != "__clone" || !callee.candidate.HasSelf ||
		callee.candidate.Async || len(callee.candidate.TemplateParams) != 0 || len(callee.info.Params) != 1 ||
		!slices.Equal(callee.info.Params, found.CalleeParamTypes) || callee.info.Result != found.CalleeResultType {
		return unresolved
	}
	self, ok := callee.unit.Sema.TypeInterner.Lookup(callee.info.Params[0])
	if !ok || self.Kind != types.KindReference || self.Mutable || self.Elem != receiver ||
		callee.candidate.ReceiverType != receiver || callee.info.Result != receiver {
		return unresolved
	}
	if callee.item.Body.IsValid() {
		if !callee.candidate.HasBody {
			return unresolved
		}
		fact, present := a.summaries[callee.key]
		if !present || len(fact.value.callables) != 0 || fact.required.failed() || len(fact.required.atoms) != 0 ||
			len(fact.postCells) != 0 || len(fact.postBackings) != 0 {
			return unresolved
		}
		for _, root := range fact.value.roots {
			if root.kind != returnOriginParam || root.param != 0 || root.expired {
				return unresolved
			}
		}
		return ""
	}
	if callee.candidate.HasBody || !callee.candidate.Intrinsic || returnOriginCallHasUnprovedEffects(callee.unit.Sema.TypeInterner, callee.info.Params) {
		return unresolved
	}
	required := returnOriginView(callee).requirement(returnOriginNoBorrowedState, callee.info.Result)
	if required.failed() || len(required.atoms) != 0 {
		return unresolved
	}
	return ""
}

// Flow can stop before a retained clone. Enumerate original operations and all
// concrete callers independently, then reject orphan or contradictory outcomes.
func (a *returnOriginAnalyzer) checkDeferredClones() error {
	authority := a.units[0].authority
	pending := func(edge *DeferredCallableEdge, reason string) {
		item := ReturnOriginPending{SourceKey: edge.Witness.SourceKey, Span: edge.Witness.Site, Reason: reason}
		if reason != "" && !slices.Contains(a.report.Pending, item) {
			a.report.Pending = append(a.report.Pending, item)
		}
	}
	edges := authority.InstantiationGraph.DeferredCallables()
	// A retained local use must not disappear with a missing graph edge, even
	// when its original body has no current instance or stops before the call.
	for _, unit := range a.units {
		for ref, use := range unit.Sema.DeferredCallableUses {
			if ref.Kind != DeferredCloneCall {
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
				if node != nil && edge.Kind == DeferredCloneCall && edge.UseID == use && edge.Witness.SourceKey == unit.SourceKey && edge.Witness.Site == span {
					matches++
				}
			}
			if matches != 1 {
				local := DeferredCallableEdge{Witness: InstantiationWitness{SourceKey: unit.SourceKey, Site: span}}
				pending(&local, "deferred clone lacks its unique original typed edge")
			}
		}
	}
	for i := range edges {
		edge := &edges[i]
		if edge.Kind != DeferredCloneCall {
			continue
		}
		if err := a.ctx.Err(); err != nil {
			return err
		}
		fn := a.functionForTemplate(edge.Caller)
		if fn == nil {
			pending(edge, "deferred clone lacks its original owning caller")
			continue
		}
		_, reason := fn.originalClone(edge)
		if reason != "" {
			pending(edge, reason)
			continue
		}
		if authority.InstantiationClosure == nil || authority.InstantiationIdentity == nil {
			pending(edge, "deferred clone lacks finalized instance authority")
			continue
		}
		for _, instance := range authority.InstantiationClosure.Instances {
			if instance.Template == edge.Caller {
				pending(edge, a.cloneOutcome(fn, edge, &instance))
			}
		}
	}
	closure := authority.InstantiationClosure
	if closure == nil {
		return nil
	}
	for _, call := range closure.ResolvedDeferredCalls {
		if call.Kind != DeferredCloneCall {
			continue
		}
		if err := a.ctx.Err(); err != nil {
			return err
		}
		var edge *DeferredCallableEdge
		matches := 0
		for i := range edges {
			if edges[i].Kind == DeferredCloneCall && edges[i].UseID == call.UseID {
				edge = &edges[i]
				matches++
			}
		}
		if matches != 1 {
			orphan := DeferredCallableEdge{Witness: InstantiationWitness{SourceKey: call.SourceKey, Site: call.Site}}
			pending(&orphan, "deferred clone outcome lacks its unique original edge")
			continue
		}
		fn := a.functionForTemplate(edge.Caller)
		instance, ok := closure.Lookup(call.Caller)
		if fn == nil || !ok || instance.Template != edge.Caller || call.CallerTemplate != edge.Caller {
			pending(edge, "deferred clone outcome lacks its exact original caller instance")
			continue
		}
		_, reason := fn.originalClone(edge)
		if reason == "" {
			reason = a.cloneOutcome(fn, edge, &instance)
		}
		pending(edge, reason)
	}
	return nil
}
