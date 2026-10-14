package sema

import (
	"cmp"
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A nonempty bodyKey preserves inferred precision. An empty key is a validated
// declared promise, including explicit widening; copying it never restores a
// body. Captured/incoming contents remain in value.roots, not in these call-result
// slots. The alternatives are finite source/type identities and immutable.
type returnOriginCallable struct {
	bodyKey  string
	typ      types.TypeID
	bodied   bool // a promise fixed from values that each name a function with a body (D1)
	slots    []uint32
	promise  source.Span
	contract *returnOriginCallableType
	view     returnOriginTypeView
}

func cloneReturnOriginCallables(values []*returnOriginCallable) []*returnOriginCallable {
	out := make([]*returnOriginCallable, len(values))
	for i, value := range values {
		if value == nil {
			continue
		}
		cloned := *value
		cloned.slots = slices.Clone(value.slots)
		cloned.view = value.view.clone()
		out[i] = &cloned
	}
	return out
}

func compareReturnOriginCallables(a, b *returnOriginCallable) int {
	if a == nil || b == nil {
		if a == b {
			return 0
		}
		if a == nil {
			return -1
		}
		return 1
	}
	if order := cmp.Compare(a.bodyKey, b.bodyKey); order != 0 {
		return order
	}
	if order := cmp.Compare(a.typ, b.typ); order != 0 {
		return order
	}
	if order := slices.Compare(a.slots, b.slots); order != 0 {
		return order
	}
	if order := compareReturnOriginSpans(a.promise, b.promise); order != 0 {
		return order
	}
	if a.bodied != b.bodied {
		if a.bodied {
			return 1
		}
		return -1
	}
	return compareReturnOriginView(a.view, b.view)
}

func (b *returnOriginBody) callableType(typ types.TypeID, span source.Span, views ...returnOriginTypeView) *types.FnInfo {
	in := b.function.unit.Sema.TypeInterner
	info := returnOriginFnInfo(in, typ)
	view := returnOriginView(b.function)
	if len(views) != 0 {
		view = views[0]
	}
	if info == nil || !view.validType(typ) {
		b.pending(span, "callable value needs its concrete original type and alias authority")
		return nil
	}
	if returnOriginFnInfo(in, info.Result) != nil {
		b.pending(span, "callable return values need their destination and capture contracts")
		return nil
	}
	return info
}

func (b *returnOriginBody) declaredCallable(typ types.TypeID, typeExpr ast.TypeID, span source.Span) (returnOriginCallable, bool) {
	view := returnOriginView(b.function)
	if b.callableType(typ, span, view) == nil {
		return returnOriginCallable{}, false
	}
	contract := b.readCallableType(b.function.unit, typ, typeExpr, span, make(map[types.TypeID]bool))
	if contract == nil {
		return returnOriginCallable{}, false
	}
	return returnOriginCallable{
		typ: typ, slots: slices.Clone(contract.slots), promise: contract.promise,
		contract: contract, view: view, bodied: b.callablePromiseHasRuntimeBody(typ),
	}, true
}

// callablePromiseHasRuntimeBody proves that an incoming function value cannot
// name a body-less intrinsic with this exact concrete ABI. Ordinary body-less
// free declarations are rejected by checkBodylessFunctions, and methods are
// not values of a free-function type. This leaves the closed intrinsic catalog
// as the only alternative that could type-check but fail when called through a
// value. A generic intrinsic is relevant only after it has the same concrete
// parameter and result descriptors; its unresolved template descriptors do not
// impersonate this promise.
func (b *returnOriginBody) callablePromiseHasRuntimeBody(typ types.TypeID) bool {
	if b == nil || b.function == nil || b.function.unit == nil || b.analyzer == nil || len(b.analyzer.units) == 0 {
		return false
	}
	in := b.function.unit.Sema.TypeInterner
	info := returnOriginFnInfo(in, typ)
	if info == nil {
		return false
	}
	authority := b.analyzer.units[0].authority
	if authority == nil {
		return false
	}
	for i := range authority.CallableCandidates {
		candidate := &authority.CallableCandidates[i]
		if candidate.HasBody || candidate.HasSelf || !candidate.Intrinsic || len(candidate.TemplateParams) != 0 {
			continue
		}
		if candidate.ResultType == info.Result && slices.Equal(candidate.ParamTypes, info.Params) {
			return false
		}
	}
	return true
}

func (b *returnOriginBody) callableIdent(id ast.ExprID, env returnOriginEnv) returnOriginExprResult {
	u := b.function.unit
	node := u.Builder.Exprs.Get(id)
	symID := u.Symbols.ExprSymbols[id]
	sym := u.Symbols.Table.Symbols.Get(symID)
	if sym == nil {
		return b.unknownExpr(env, node.Span, "callable identifier lacks concrete source facts")
	}
	value := env.value(symID)
	switch {
	case sym.Kind == symbols.SymbolFunction:
		fn, reason := b.resolveCallDeclaration(symID)
		if fn == nil {
			return b.unknownExpr(env, node.Span, reason)
		}
		view := returnOriginView(fn)
		if len(fn.candidate.TemplateParams) != 0 {
			var found bool
			view, found, reason = b.analyzer.genericFunctionValueAt(fn, b.function, id, node.Span)
			if !found || reason != "" {
				return b.unknownExpr(env, node.Span, reason)
			}
		}
		if b.callableType(sym.Type, node.Span, view) == nil {
			return b.unknownExpr(env, node.Span, "callable identifier lacks concrete source facts")
		}
		value = returnOriginValueOf()
		value.callables = []*returnOriginCallable{{bodyKey: fn.key, typ: sym.Type, view: view}}
	case !b.within(sym.Scope, b.function.scope):
		return b.unknownExpr(env, node.Span, "captured callable binding needs its owning content proof")
	case len(value.callables) == 0:
		if b.callableType(u.Sema.ExprTypes[id], node.Span) == nil {
			return b.unknownExpr(env, node.Span, "callable identifier lacks concrete source facts")
		}
		// Unused inputs retain their existing Param-content fact. Materialize
		// the declared callable only when it is actually read, copied or called.
		_, typeExpr, parameter := b.callbackParameter(id)
		if !parameter {
			return b.unknownExpr(env, node.Span, "callable binding has no evaluated source alternative")
		}
		declared, valid := b.declaredCallable(u.Sema.ExprTypes[id], typeExpr, node.Span)
		if !valid {
			return b.unknownExpr(env, node.Span, "incoming callable promise is not finalized")
		}
		value.callables = []*returnOriginCallable{&declared}
		env = env.assign(symID, sym.Scope, value)
	}
	b.checkExpired(value, node.Span)
	out := originExprValue(env, value)
	if sym.Kind != symbols.SymbolFunction {
		out.storage = returnOriginValueOf(returnOrigin{kind: returnOriginLocal, binding: symID, scope: sym.Scope})
	}
	return out
}

func (a *returnOriginAnalyzer) genericFunctionValueAt(fn, caller *returnOriginFunction, id ast.ExprID, span source.Span) (view returnOriginTypeView, handled bool, reason string) {
	if a == nil || fn == nil || caller == nil || fn.candidate == nil || caller.candidate == nil ||
		len(fn.candidate.TemplateParams) == 0 || len(caller.candidate.TemplateParams) != 0 ||
		caller.unit == nil || caller.unit.authority == nil || caller.unit.authority.InstantiationClosure == nil {
		return returnOriginTypeView{}, false, "generic function value lacks finalized owning authority"
	}
	node := caller.unit.Builder.Exprs.Get(id)
	if node == nil || (node.Kind != ast.ExprIdent && node.Kind != ast.ExprMember) || node.Span != span {
		return returnOriginTypeView{}, false, "generic function value lacks its original typed expression"
	}
	var found *ConcreteInstantiationUse
	for i := range caller.unit.authority.InstantiationClosure.UseSites {
		use := &caller.unit.authority.InstantiationClosure.UseSites[i]
		if use.Kind != InstantiationFunction || use.Caller != (InstanceKey{}) || use.SourceKey != caller.unit.SourceKey || use.Site != span ||
			!a.sameTemplateBody(use.CallerTemplate, caller.candidate.Symbol) || !a.sameTemplateBody(use.CalleeTemplate, fn.candidate.Symbol) {
			continue
		}
		if found != nil {
			return returnOriginTypeView{}, true, "generic function value has ambiguous finalized uses"
		}
		found = use
	}
	if found == nil {
		return returnOriginTypeView{}, true, "generic function value lacks its finalized concrete use"
	}
	if reason := a.genericInstance(found.Callee, found.CalleeTemplate, found.TemplateArgs); reason != "" {
		return returnOriginTypeView{}, true, reason
	}
	if len(found.TemplateArgs) != len(fn.candidate.TemplateParams) || !returnOriginConcreteArgs(caller.unit.Sema.TypeInterner, found.TemplateArgs) {
		return returnOriginTypeView{}, true, "generic function value lacks concrete template arguments"
	}
	actual := returnOriginFnInfo(caller.unit.Sema.TypeInterner, caller.unit.Sema.ExprTypes[id])
	if actual == nil || len(actual.Params) != len(fn.info.Params) {
		return returnOriginTypeView{}, true, "generic function value disagrees with its concrete callable type"
	}
	for i, param := range fn.info.Params {
		if returnOriginBoundType(param, fn.candidate.TemplateParams, found.TemplateArgs) != actual.Params[i] {
			return returnOriginTypeView{}, true, "generic function value disagrees with its concrete parameter types"
		}
	}
	if returnOriginBoundType(fn.info.Result, fn.candidate.TemplateParams, found.TemplateArgs) != actual.Result {
		return returnOriginTypeView{}, true, "generic function value disagrees with its concrete result type"
	}
	return returnOriginBoundView(fn, caller, found.TemplateArgs), true, ""
}

func (b *returnOriginBody) callableFunction(value *returnOriginCallable) *returnOriginFunction {
	if value == nil {
		return nil
	}
	fn := b.analyzer.bodies[value.bodyKey]
	if fn == nil {
		fn = b.analyzer.declarations[value.bodyKey]
	}
	if fn == nil {
		return nil
	}
	sym := fn.unit.Symbols.Table.Symbols.Get(fn.symbol)
	if sym == nil || sym.Type != value.typ {
		return nil
	}
	return fn
}

func (b *returnOriginBody) callableSources(value *returnOriginCallable, span source.Span, invoke bool) returnOriginValue {
	if value == nil {
		return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	view := value.view
	if view.owner == nil {
		view = returnOriginView(b.function)
	}
	info := b.callableType(value.typ, span, view)
	if info == nil {
		return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	if invoke && value.bodyKey != "" {
		if fn := b.callableFunction(value); fn != nil && !fn.item.Body.IsValid() &&
			b.analyzer.isCoreTask(b.function.unit.Sema.TypeInterner, info.Result) {
			b.pending(span, "task function value may name a body-less intrinsic")
			return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		}
	}
	if value.bodyKey == "" {
		if !invoke {
			out := returnOriginValueOf()
			for _, slot := range value.slots {
				out = out.join(returnOriginValueOf(returnOrigin{kind: returnOriginParam, param: slot}))
			}
			return out
		}
		if b.analyzer.isCoreTask(b.function.unit.Sema.TypeInterner, info.Result) && !value.bodied {
			b.pending(span, "task function value may name a body-less intrinsic")
			return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		}
		return b.opaqueReturnSources(nil, info, value.slots, true, span, &returnOriginSignature{params: info.Params, result: info.Result, binding: &view})
	}
	fn := b.callableFunction(value)
	if fn == nil {
		b.pending(span, "function value lost its exact original declaration/body")
		return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	if fn.item.Body.IsValid() {
		summary := b.analyzer.summaries[fn.key].value
		if result, _, resolved := view.resolve(fn.info.Result); summary.normal && resolved &&
			returnOriginTypeShape(fn.unit.Sema.TypeInterner, result, nil) == returnOriginRefFree &&
			!b.analyzer.loanCarrier(result) && !b.hidesBorrow(result) {
			summary = returnOriginValueOf()
		}
		if invoke && b.inheritRequirements(fn, view, span).failed() && summary.normal {
			return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		}
		return summary.clone()
	}
	slots, valid := b.declaredFunctionSources(fn, span)
	if !invoke && valid {
		out := returnOriginValueOf()
		for _, slot := range slots {
			out = out.join(returnOriginValueOf(returnOrigin{kind: returnOriginParam, param: slot}))
		}
		return out
	}
	return b.opaqueReturnSources(nil, info, slots, valid, span)
}

func (b *returnOriginBody) callableValueSources(value returnOriginValue, span source.Span, invocation ...bool) returnOriginValue {
	invoke := len(invocation) == 0 || invocation[0]
	out := returnOriginValue{}
	for _, alternative := range value.callables {
		out = out.join(b.callableSources(alternative, span, invoke))
	}
	unknown := len(value.callables) == 0
	for _, root := range value.roots {
		// Incoming opaque contents are allowed, but the call promise still
		// permits references only from explicit arguments, never captures.
		unknown = unknown || root.kind != returnOriginParam || root.expired
	}
	if unknown {
		b.pending(span, "callable contents have unresolved or captured provenance")
		out = out.join(returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}))
	}
	return out
}
