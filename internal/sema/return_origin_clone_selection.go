package sema

import "surge/internal/ast"

// A direct clone(x) of a concrete non-Copy type is the call HIR spells from
// CloneSymbols: the `clone` identifier is never a value, and x reaches the
// program-wide __clone by shared reference. A Copy result is a copy there, not
// a call. A certified selection returns an owned value that keeps no argument
// borrow or loan; every other form keeps the ordinary call reading.
func (b *returnOriginBody) selectedClone(id ast.ExprID, call *ast.ExprCallData, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, bool, error) {
	u := b.function.unit
	ident, ok := u.Builder.Exprs.Ident(call.Target)
	if !ok || ident == nil || len(call.Args) != 1 || call.HasNamedArgs() || u.Sema.TypeInterner.IsCopy(u.Sema.ExprTypes[id]) {
		return returnOriginExprResult{}, false, nil
	}
	if name, found := u.Builder.StringsInterner.Lookup(ident.Name); !found || name != "clone" ||
		!b.selectedOperation(u.Sema.CloneSymbols, id, "__clone", 1, call.Args[0].Value) {
		return returnOriginExprResult{}, false, nil
	}
	out, err := b.expr(call.Args[0].Value, env, targets)
	if err != nil || !out.flow.normal.reachable {
		return out, true, err
	}
	out.value, out.storage = returnOriginValueOf(), returnOriginValue{} // certified: no loan-discard refusal
	return out, true, nil
}
