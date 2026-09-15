package sema

import "surge/internal/ast"

// expr evaluates an expression and then the implicit conversion SEMA recorded on
// that exact node, which HIR lowers around it: every consumer sees the value the
// program actually passes on, never the source's own.
func (b *returnOriginBody) expr(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	out, err := b.exprCore(id, env, targets)
	u := b.function.unit
	conversion, converted := u.Sema.ImplicitConversions[id]
	if err != nil || !converted || !out.flow.normal.reachable {
		return out, err
	}
	out.storage = returnOriginValue{} // the converted value is a new temporary
	switch conversion.Kind {
	case ImplicitConversionSome, ImplicitConversionSuccess, ImplicitConversionTagUnion:
		// A wrapper holds the payload itself, with every loan it carries.
	case ImplicitConversionTo:
		if b.selectedOperation(u.Sema.ToSymbols, id, "__to", 2) {
			// As for a certified operator: no container formal and no borrowed
			// state in the result, so no operand origin or loan can leave.
			out.value = returnOriginValueOf()
		} else {
			b.pending(u.Builder.Exprs.Get(id).Span, "implicit conversion needs its selected __to origin contract")
			out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		}
	default:
		b.pending(u.Builder.Exprs.Get(id).Span, "implicit conversion kind needs an origin transfer")
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	return out, nil
}
