package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/types"
)

// A deferred method call has no selected symbol, so the use of the implementation it
// resolved to has no original typed request to re-read. The finalized outcome is that
// request once it agrees with the implementation's own declaration under the use's
// template arguments, and that declaration accepts the receiver and arguments the
// method certificate already tied to the edge.
func (a *returnOriginAnalyzer) deferredImplementationUse(fn, caller *returnOriginFunction, expression ast.ExprID, use ConcreteInstantiationUse) (*returnOriginSignature, string, bool) {
	useID := caller.unit.Sema.DeferredCallableUses[DeferredUseRef{Expr: expression, Kind: DeferredMethodCall}]
	if useID == "" {
		return nil, "", false
	}
	authority := caller.unit.authority
	closure := authority.InstantiationClosure
	var edge *DeferredCallableEdge
	for _, candidate := range authority.InstantiationGraph.DeferredCallables() {
		if candidate.Kind == DeferredMethodCall && candidate.UseID == useID {
			if edge != nil {
				return nil, "deferred implementation use lacks its unique original edge", true
			}
			copy := candidate
			edge = &copy
		}
	}
	if edge == nil || closure == nil || use.Caller == (InstanceKey{}) || edge.Caller != use.CallerTemplate ||
		edge.Witness.Site != use.Site || edge.Witness.SourceKey != use.SourceKey {
		return nil, "deferred implementation use lacks its unique original edge", true
	}
	var found *ResolvedDeferredCall
	for i := range closure.ResolvedDeferredCalls {
		if call := &closure.ResolvedDeferredCalls[i]; call.Kind == DeferredMethodCall && call.Caller == use.Caller && call.UseID == useID {
			if found != nil {
				return nil, "deferred implementation use has duplicate finalized outcomes", true
			}
			found = call
		}
	}
	if found == nil {
		return nil, "deferred implementation use lacks its finalized outcome", true
	}
	in, params, args := fn.unit.Sema.TypeInterner, fn.candidate.TemplateParams, use.TemplateArgs
	const disagrees = "deferred implementation use disagrees with its finalized outcome"
	const mismatched = "deferred implementation use disagrees with its finalized receiver or arguments"
	if found.Outcome != DeferredCallableResolved || found.Callee != use.CalleeTemplate || len(found.CalleeTemplateArgs) == 0 ||
		!slices.Equal(found.CalleeTemplateArgs, args) || len(found.CalleeParamTypes) != len(fn.info.Params) {
		return nil, disagrees, true
	}
	for i, formal := range fn.info.Params {
		if matchReturnOriginSourceType(in, formal, found.CalleeParamTypes[i], params, args) != "" {
			return nil, disagrees, true
		}
	}
	if matchReturnOriginSourceType(in, fn.info.Result, found.CalleeResultType, params, args) != "" {
		return nil, disagrees, true
	}
	// A static edge has no receiver formal; P1p's certificate pins its Receiver against the edge.
	off := 0
	if fn.candidate.HasSelf {
		off = 1
		if !returnOriginFormalAccepts(in, fn.info.Params[0], found.Receiver, params, args) {
			return nil, mismatched, true
		}
	}
	if len(found.Args)+off != len(fn.info.Params) {
		return nil, mismatched, true
	}
	for i, actual := range found.Args {
		if !returnOriginFormalAccepts(in, fn.info.Params[i+off], actual, params, args) {
			return nil, mismatched, true
		}
	}
	binding := returnOriginBoundView(fn, nil, args)
	return &returnOriginSignature{params: slices.Clone(found.CalleeParamTypes), effects: slices.Clone(found.CalleeParamTypes),
		result: found.CalleeResultType, binding: &binding}, "", true
}

// The resolver's own tier walk re-wraps and therefore interns (wrapDeferredReceiver,
// deferred_callable_match.go:314-327), which this analysis must never do. Strip the same
// wrappers from both sides instead, and compare the cores the resolver would dispatch on.
func returnOriginFormalAccepts(in *types.Interner, formal, actual types.TypeID, params, args []types.TypeID) bool {
	if matchReturnOriginSourceType(in, formal, actual, params, args) == "" {
		return true
	}
	wantCore, wantWrappers := splitDeferredReceiverWrappers(in, formal)
	gotCore, gotWrappers := splitDeferredReceiverWrappers(in, actual)
	if wantCore == types.NoTypeID || gotCore == types.NoTypeID || !returnOriginWrappersAccept(wantWrappers, gotWrappers) {
		return false
	}
	owner := terminalDeferredReceiverAlias(in, gotCore)
	tiers := []types.TypeID{gotCore, owner, deferredReceiverFamily(in, gotCore)}
	if base, ok := in.StructBase(owner); ok {
		tiers = append(tiers, base)
	}
	for _, tier := range tiers {
		if tier != types.NoTypeID && matchReturnOriginSourceType(in, wantCore, tier, params, args) == "" {
			return true
		}
	}
	return false
}

// Identical wrappers, or the one leading shared reference the resolver may add for a
// reference formal (matchCallableType's implicitBorrow, deferred_callable_match.go:44).
func returnOriginWrappersAccept(want, got []types.Type) bool {
	if len(want) == len(got)+1 && want[0].Kind == types.KindReference && !want[0].Mutable {
		want = want[1:]
	}
	return slices.EqualFunc(want, got, func(a, b types.Type) bool { return a.Kind == b.Kind && a.Mutable == b.Mutable })
}
