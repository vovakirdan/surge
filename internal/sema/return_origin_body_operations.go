package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// bodyOperation answers an operator or conversion SEMA resolved to an exact,
// synchronous source body. HIR lowers it as a call (internal/hir/lower_expr.go:27-32;
// lower_expr_helpers.go:66-77); its result type is erased, its formals hold no
// container and no unproved effect, and its checked summary names no origin, so no
// operand origin or loan can reach the value. A body still at bottom leaves the
// operation unreachable, exactly as a call does (return_origin_calls.go:232-235).
//
// The summary read is a fixpoint-phase fact: solveBodies iterates to a fixed point
// and only then sets collect, re-running every body against the final summaries
// (return_origin_summary.go:18-60), and pending is a no-op until then
// (return_origin_check.go:126-129).
func (b *returnOriginBody) genericOperationView(fn *returnOriginFunction, id ast.ExprID) (returnOriginTypeView, bool) {
	if len(fn.candidate.TemplateParams) == 0 {
		return returnOriginView(fn), true
	}
	authority := b.function.unit.authority
	if authority.InstantiationClosure == nil || authority.InstantiationIdentity == nil {
		return returnOriginTypeView{}, false
	}
	if len(b.function.candidate.TemplateParams) != 0 {
		return returnOriginTypeView{}, false
	}
	span := b.function.unit.Builder.Exprs.Get(id).Span
	var found *ConcreteInstantiationUse
	for i := range authority.InstantiationClosure.UseSites {
		use := &authority.InstantiationClosure.UseSites[i]
		if use.Kind != InstantiationFunction || use.CalleeTemplate != fn.candidate.Symbol {
			continue
		}
		if use.Caller != (InstanceKey{}) || use.CallerTemplate != b.function.symbol {
			continue
		}
		if use.SourceKey != b.function.unit.SourceKey || use.Site != span {
			continue
		}
		if found != nil {
			return returnOriginTypeView{}, false
		}
		found = use
	}
	if found == nil || !returnOriginConcreteArgs(authority.TypeInterner, found.TemplateArgs) {
		return returnOriginTypeView{}, false
	}
	if b.analyzer.genericInstance(found.Callee, found.CalleeTemplate, found.TemplateArgs) != "" {
		return returnOriginTypeView{}, false
	}
	if b.analyzer.useWitnessReason(*found) != "call" {
		return returnOriginTypeView{}, false
	}
	return returnOriginBoundView(fn, b.function, found.TemplateArgs), true
}

func returnOriginOperationEffectsReadOnly(in *types.Interner, view returnOriginTypeView, params []types.TypeID) bool {
	for _, id := range params {
		typ, present := in.Lookup(returnOriginResolveAlias(in, id))
		if present && typ.Kind == types.KindReference && !typ.Mutable {
			continue
		}
		if view.shape(id) != returnOriginRefFree {
			return false
		}
	}
	return true
}

func (b *returnOriginBody) bodyOperation(out *returnOriginExprResult, selections map[ast.ExprID]symbols.SymbolID, id ast.ExprID,
	result types.TypeID, name string, arity int, values []returnOriginCallValue, operands ...ast.ExprID,
) bool {
	u := b.function.unit
	selected, present := selections[id]
	// An erased result carries no origin. A concrete dynamic array is also
	// admitted when its checked body returns no source and its element can hold
	// neither a borrow nor a storage loan: that proves fresh owning storage.
	resultFree := b.erasedType(result)
	if !resultFree {
		container, canonical := returnOriginContainer(u.Sema.TypeInterner, result)
		resultFree = canonical && !container.reference && container.family == u.Sema.TypeInterner.ArrayNominalType() &&
			returnOriginTypeShape(u.Sema.TypeInterner, container.element, nil) == returnOriginRefFree && !b.holdsLoan(container.element)
	}
	if !present || !selected.IsValid() || len(values) != len(operands) {
		return false
	}
	for _, operand := range operands {
		if conversion, converted := u.Sema.ImplicitConversions[operand]; converted &&
			!(operand == id && conversion.Kind == ImplicitConversionTo) {
			return false
		}
	}
	fn, reason := b.analyzer.selectedCallableFunction(u, selected)
	if reason != "" {
		return false
	}
	c, in := fn.candidate, u.Sema.TypeInterner
	view, exactUse := b.genericOperationView(fn, id)
	effectParams := fn.info.Params
	if name == "__to" && arity == 2 && len(operands) == 1 && len(effectParams) == 2 && effectParams[1] == result {
		// A cast's second `__to` formal is its type marker, not an evaluated
		// source operand. It may have the same container type as the result.
		effectParams = effectParams[:1]
	}
	if c.Name != name || len(fn.info.Params) != arity || !c.HasBody || !fn.item.Body.IsValid() || c.Async || !exactUse ||
		!returnOriginOperationEffectsReadOnly(in, view, effectParams) {
		return false
	}
	summary := b.analyzer.summaries[fn.key].value
	span := u.Builder.Exprs.Get(id).Span
	if b.inheritRequirements(fn, view, span).failed() {
		return false
	}
	out.storage = returnOriginValue{}
	if !summary.normal {
		out.value, out.flow.normal = returnOriginValue{}, returnOriginEnv{}
		return true
	}
	if len(summary.roots) == 0 && len(summary.callables) == 0 {
		if !resultFree {
			return false
		}
		out.value = returnOriginValueOf()
		return true
	}
	if len(summary.callables) != 0 || (b.shape(id) == returnOriginRefFree && !b.holdsLoan(result)) {
		return false
	}
	value := returnOriginValueOf()
	for _, root := range summary.roots {
		i := int(root.param)
		if root.kind != returnOriginParam || root.expired || root.selector != returnOriginInputValue || i >= len(operands) {
			return false
		}
		value = value.join(b.callArgumentOrigin(operands[i], fn.info.Params[i], values[i], false))
	}
	out.value = value
	return true
}
