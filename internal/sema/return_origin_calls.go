package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (b *returnOriginBody) call(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	call, _ := u.Builder.Exprs.Call(id)
	span := u.Builder.Exprs.Get(id).Span
	symID := u.Symbols.ExprSymbols[id]
	sym := u.Symbols.Table.Symbols.Get(symID)
	callee := u.functions[symID]
	var receiver ast.ExprID
	if sym != nil && sym.Signature != nil && sym.Signature.HasSelf {
		if member, ok := u.Builder.Exprs.Member(call.Target); ok && member != nil {
			receiver = member.Target
		}
	}
	flow := returnOriginFlow{normal: env}
	values := make(map[ast.ExprID]returnOriginValue, len(call.Args)+1)
	evaluate := func(expr ast.ExprID) error {
		var err error
		flow, err = flow.then(func(next returnOriginEnv) (returnOriginFlow, error) {
			out, evalErr := b.expr(expr, next, targets)
			values[expr] = out.value.clone()
			if _, implicit := u.Sema.ImplicitConversions[expr]; implicit {
				b.pending(span, "implicit argument conversion needs its resolved callable origin contract")
				values[expr] = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
			}
			return out.flow, evalErr
		})
		return err
	}
	if receiver.IsValid() {
		if err := evaluate(receiver); err != nil {
			return returnOriginExprResult{}, err
		}
	} else if sym == nil || sym.Kind != symbols.SymbolFunction {
		if err := evaluate(call.Target); err != nil {
			return returnOriginExprResult{}, err
		}
	}
	for _, arg := range call.Args {
		if err := evaluate(arg.Value); err != nil {
			return returnOriginExprResult{}, err
		}
	}
	if !flow.normal.reachable {
		return returnOriginExprResult{flow: flow}, nil
	}
	if callee == nil || sym == nil || sym.Signature == nil {
		b.pending(span, "call needs an exact body, canonical core contract, or opaque declaration promise")
		return returnOriginExprResult{flow: flow, value: returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})}, nil
	}
	slots, err := mapReturnOriginArguments(sym.Signature, call, receiver)
	if err != nil {
		return returnOriginExprResult{}, err
	}
	actuals := make([]returnOriginValue, len(slots))
	params := callee.unit.Builder.Items.GetFnParamIDs(callee.item)
	for i, slot := range slots {
		actuals[i] = returnOriginValueOf()
		if slot.defaulted {
			param := callee.unit.Builder.Items.FnParam(params[i])
			// Literal defaults have no hidden evaluation or borrowed content.
			// Other defaults must be evaluated in their original owner unit.
			if literal, ok := callee.unit.Builder.Exprs.Literal(param.Default); !ok || literal == nil {
				b.pending(span, "nonliteral default argument needs its owning expression transfer")
				actuals[i] = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
			}
		}
		for _, expr := range slot.exprs {
			actuals[i] = actuals[i].join(values[expr])
		}
		if typ, ok := u.Sema.TypeInterner.Lookup(callee.info.Params[i]); ok && typ.Kind == types.KindReference && typ.Mutable {
			if returnOriginTypeShape(u.Sema.TypeInterner, typ.Elem, nil) != returnOriginRefFree {
				b.pending(span, "mutable argument may replace reference-bearing contents")
			}
		}
	}
	summary := b.analyzer.summaries[callee.key]
	if !summary.normal {
		flow.normal = returnOriginEnv{}
		return returnOriginExprResult{flow: flow}, nil
	}
	value := returnOriginValueOf()
	for _, root := range summary.roots {
		if root.kind != returnOriginParam || int(root.param) >= len(actuals) {
			b.pending(span, "callee returned an unproved source")
			value = value.join(returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}))
			continue
		}
		value = value.join(actuals[root.param])
	}
	return returnOriginExprResult{flow: flow, value: value}, nil
}
