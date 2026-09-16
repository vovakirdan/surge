package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

// A deferred contract method is chosen per caller instance, and no contract
// promise is believed: implementations are not checked against one. A
// reference-free type is not a borrow-free value (an array view carries a
// storage loan), so every admitted result gets the ordinary opaque-result
// classification. Each actual is a reference, whose writes the instance
// certificate proves absent, or cannot carry a loan past the call.
func (b *returnOriginBody) deferredMethodCall(id ast.ExprID, local *DeferredCallableEdge, call *ast.ExprCallData,
	receiver ast.ExprID, values map[ast.ExprID]returnOriginCallValue, span source.Span) (returnOriginValue, bool) {
	fn := b.function
	u := fn.unit
	in := u.Sema.TypeInterner
	// The unit's edge names its own symbols; only the published edge names the canonical caller.
	var edge *DeferredCallableEdge
	for _, published := range u.authority.InstantiationGraph.DeferredCallables() {
		if local != nil && published.Kind == DeferredMethodCall && published.UseID == local.UseID {
			if edge != nil {
				return returnOriginValue{}, false
			}
			copy := published
			edge = &copy
		}
	}
	if edge == nil || call.HasNamedArgs() || len(edge.ExplicitTypeArgs) != 0 || !fn.originalDeferredMethod(edge) ||
		int(edge.CallerTemplateArity) != len(fn.candidate.TemplateParams) || edge.Requirement.Name != edge.Method ||
		len(edge.Requirement.Contracts) == 0 || edge.Requirement.Async || edge.Requirement.Result != edge.ExpectedResult {
		return returnOriginValue{}, false
	}
	result := u.Sema.ExprTypes[id]
	if b.shape(id) != returnOriginRefFree && !fn.directTemplateParam(result) {
		return returnOriginValue{}, false
	}
	actuals := make([]ast.ExprID, 0, len(call.Args)+1)
	if receiver.IsValid() {
		actuals = append(actuals, receiver)
	}
	for _, arg := range call.Args {
		actuals = append(actuals, arg.Value)
	}
	for _, expr := range actuals {
		typ := u.Sema.ExprTypes[expr]
		if !returnOriginIsReference(in, typ) && returnOriginTypeShape(in, typ, nil) != returnOriginRefFree && !fn.directTemplateParam(typ) {
			return returnOriginValue{}, false
		}
	}
	for _, expr := range actuals {
		switch typ := u.Sema.ExprTypes[expr]; {
		case returnOriginIsReference(in, typ):
		case fn.directTemplateParam(typ):
			b.requireOpaqueState(returnOriginView(fn), typ, span)
		default:
			b.discardLoans(values[expr].value, span)
		}
	}
	return b.requireOpaqueState(returnOriginView(fn), result, span), true
}

// A generic callee's by-value actual whose type is exactly the caller's own
// template parameter holds contents only a caller instance can name. When every
// other effect is proven, each such parameter leaves the body-level test and
// every use of this body must prove it NoBorrowedState instead; otherwise the
// test is unchanged and nothing is recorded.
func (b *returnOriginBody) opaqueCallEffects(signature *returnOriginSignature, params, effects []types.TypeID, span source.Span) []types.TypeID {
	if signature == nil {
		return effects
	}
	fn := b.function
	in := fn.unit.Sema.TypeInterner
	var kept, moved []types.TypeID
	for i, effect := range effects {
		if i < len(params) && !returnOriginIsReference(in, params[i]) && fn.directTemplateParam(effect) {
			moved = append(moved, effect)
			continue
		}
		kept = append(kept, effect)
	}
	if len(moved) == 0 || returnOriginCallHasUnprovedEffects(in, kept) {
		return effects
	}
	for _, effect := range moved {
		b.requireOpaqueState(returnOriginView(fn), effect, span)
	}
	return kept
}

// A current use rebinds each effect that is exactly its caller's template
// parameter to that instance's argument. Nested mentions stay symbolic and
// keep their refusal.
func returnOriginConcreteEffects(in *types.Interner, view *returnOriginSignature, params, args []types.TypeID) string {
	for i, effect := range view.effects {
		bound := returnOriginBoundType(effect, params, args)
		if bound == effect {
			continue
		}
		if _, ok := in.Lookup(bound); !ok || types.ContainsGenericParam(in, bound) {
			return "generic use lacks its concrete argument effects"
		}
		view.effects[i] = bound
	}
	return ""
}
