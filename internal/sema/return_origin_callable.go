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
	slots    []uint32
	promise  source.Span
	contract *returnOriginCallableType
	view     returnOriginTypeView
}

func cloneReturnOriginCallables(values []returnOriginCallable) []returnOriginCallable {
	out := slices.Clone(values)
	for i := range out {
		out[i].slots = slices.Clone(out[i].slots)
		out[i].view = out[i].view.clone()
	}
	return out
}

func compareReturnOriginCallables(a, b returnOriginCallable) int {
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
	return returnOriginCallable{typ: typ, slots: slices.Clone(contract.slots), promise: contract.promise, contract: contract, view: view}, true
}

func (b *returnOriginBody) callableIdent(id ast.ExprID, env returnOriginEnv) returnOriginExprResult {
	u := b.function.unit
	node := u.Builder.Exprs.Get(id)
	symID := u.Symbols.ExprSymbols[id]
	sym := u.Symbols.Table.Symbols.Get(symID)
	if sym == nil || b.callableType(u.Sema.ExprTypes[id], node.Span) == nil {
		return b.unknownExpr(env, node.Span, "callable identifier lacks concrete source facts")
	}
	value := env.value(symID)
	if sym.Kind == symbols.SymbolFunction {
		fn, reason := b.resolveCallDeclaration(symID)
		if fn == nil {
			return b.unknownExpr(env, node.Span, reason)
		}
		value = returnOriginValueOf()
		value.callables = []returnOriginCallable{{bodyKey: fn.key, typ: sym.Type}}
	} else if !b.within(sym.Scope, b.function.scope) {
		return b.unknownExpr(env, node.Span, "captured callable binding needs its owning content proof")
	} else if len(value.callables) == 0 {
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
		value.callables = []returnOriginCallable{declared}
		env = env.assign(symID, sym.Scope, value)
	}
	b.checkExpired(value, node.Span)
	out := originExprValue(env, value)
	if sym.Kind != symbols.SymbolFunction {
		out.storage = returnOriginValueOf(returnOrigin{kind: returnOriginLocal, binding: symID, scope: sym.Scope})
	}
	return out
}

func (b *returnOriginBody) callableFunction(value returnOriginCallable) *returnOriginFunction {
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

func (b *returnOriginBody) callableSources(value returnOriginCallable, span source.Span, invoke bool) returnOriginValue {
	view := value.view
	if view.owner == nil {
		view = returnOriginView(b.function)
	}
	info := b.callableType(value.typ, span, view)
	if info == nil {
		return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	if value.bodyKey == "" {
		if !invoke {
			out := returnOriginValueOf()
			for _, slot := range value.slots {
				out = out.join(returnOriginValueOf(returnOrigin{kind: returnOriginParam, param: slot}))
			}
			return out
		}
		return b.opaqueReturnSources(info, value.slots, true, span, &returnOriginSignature{params: info.Params, result: info.Result, binding: &view})
	}
	fn := b.callableFunction(value)
	if fn == nil {
		b.pending(span, "function value lost its exact original declaration/body")
		return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	if fn.item.Body.IsValid() {
		summary := b.analyzer.summaries[fn.key].value
		if invoke && b.inheritRequirements(fn, view, span).failed() {
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
	return b.opaqueReturnSources(info, slots, valid, span)
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
