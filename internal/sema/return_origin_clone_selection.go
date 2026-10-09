package sema

import "surge/internal/ast"

// A direct clone(x) of a concrete non-Copy type is the call HIR spells from
// CloneSymbols: the `clone` identifier is never a value, and x reaches the
// program-wide __clone by shared reference. A Copy result is a copy there, not
// a call; copyClone answers it. A certified selection returns an owned value that keeps no argument
// borrow or loan; every other form keeps the ordinary call reading.
func (b *returnOriginBody) selectedClone(id ast.ExprID, call *ast.ExprCallData, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, bool, error) {
	u := b.function.unit
	if u.Sema.TypeInterner.IsCopy(u.Sema.ExprTypes[id]) {
		return b.copyClone(id, call, env, targets)
	}
	ident, ok := u.Builder.Exprs.Ident(call.Target)
	if !ok || ident == nil || len(call.Args) != 1 || call.HasNamedArgs() {
		return returnOriginExprResult{}, false, nil
	}
	name, found := u.Builder.StringsInterner.Lookup(ident.Name)
	selected, present := u.Sema.CloneSymbols[id]
	fn, reason := b.analyzer.selectedCallableFunction(u, selected)
	body := present && reason == "" && fn != nil && fn.candidate.HasBody && fn.item.Body.IsValid()
	intrinsic := b.selectedOperation(u.Sema.CloneSymbols, id, "__clone", 1, call.Args[0].Value)
	if !found || name != "clone" || (!body && !intrinsic) {
		return returnOriginExprResult{}, false, nil
	}
	out, err := b.expr(call.Args[0].Value, env, targets)
	if err != nil || !out.flow.normal.reachable {
		return out, true, err
	}
	if body && !b.bodyOperation(&out, u.Sema.CloneSymbols, id, u.Sema.ExprTypes[id], "__clone", 1,
		[]returnOriginCallValue{{value: out.value, storage: out.storage}}, call.Args[0].Value) {
		b.pending(u.Builder.Exprs.Get(id).Span, "selected clone source body lacks its exact origin transfer")
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		out.storage = returnOriginValue{}
		return out, true, nil
	}
	if intrinsic {
		out.value, out.storage = returnOriginValueOf(), returnOriginValue{} // certified: no loan-discard refusal
	}
	return out, true, nil
}
