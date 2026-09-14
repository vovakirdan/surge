package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/types"
)

// The existing argument mapper supplies physical slots, including self and
// named arguments. Read the selected body's original parameter in its owning
// unit; a caller's same-shaped type or same-named declaration is not authority.
func (b *returnOriginBody) convertCallableArgument(callee *returnOriginFunction, index int, slot returnOriginArgument, expr ast.ExprID, formal types.TypeID, actual returnOriginValue) returnOriginValue {
	u := b.function.unit
	span := u.Builder.Exprs.Get(expr).Span
	unknown := actual.join(returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}))
	if callee == nil || callee.candidate == nil || !callee.candidate.HasBody || !callee.item.Body.IsValid() ||
		len(callee.candidate.TemplateParams) != 0 || slot.defaulted || len(slot.exprs) != 1 ||
		index < 0 || index >= len(callee.info.Params) || formal != callee.info.Params[index] {
		b.pending(span, "callable argument conversion needs its selected destination promise")
		return unknown
	}
	if index < len(callee.candidate.Variadic) && callee.candidate.Variadic[index] {
		b.pending(span, "variadic callable arguments need their owning payload transfer")
		return unknown
	}
	if _, converted := u.Sema.ImplicitConversions[expr]; converted {
		b.pending(span, "callable argument conversion needs its selected operation contract")
		return unknown
	}
	if b.callableType(formal, span) == nil {
		return unknown
	}
	params := callee.unit.Builder.Items.GetFnParamIDs(callee.item)
	if index >= len(params) {
		b.pending(span, "callable argument destination lacks its original physical parameter")
		return unknown
	}
	param := callee.unit.Builder.Items.FnParam(params[index])
	if param == nil || !param.Type.IsValid() {
		b.pending(span, "callable argument destination lacks its original type syntax")
		return unknown
	}
	contract := b.readCallableType(callee.unit, formal, param.Type, span, make(map[types.TypeID]bool))
	if contract == nil {
		return unknown
	}
	expected := returnOriginCallable{typ: formal, slots: slices.Clone(contract.slots), promise: contract.promise, contract: contract}
	if !b.checkCallableDestination(actual, expected, span) {
		return unknown
	}
	// Conversion fixes the destination promise without narrowing the caller's
	// binding. Content roots survive independently; they never become borrowed
	// result sources merely because this value is callable. Callee bodies still
	// infer against their declared parameter promises, not actual body precision.
	out := actual.clone()
	out.callables = []returnOriginCallable{expected}
	return out
}
