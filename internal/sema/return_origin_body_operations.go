package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// bodyOperation answers an operator or conversion SEMA resolved to a non-generic,
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
func (b *returnOriginBody) bodyOperation(out *returnOriginExprResult, selections map[ast.ExprID]symbols.SymbolID, id ast.ExprID,
	result types.TypeID, name string, arity int, operands ...ast.ExprID,
) bool {
	u := b.function.unit
	selected, present := selections[id]
	// Erased: every reader drops the roots of such a value, because it holds no
	// borrow and is no loan carrier (return_origin_expr.go:44-47). Written out
	// rather than shared, so this file owns no name another packet also declares.
	if !present || !selected.IsValid() ||
		returnOriginTypeShape(u.Sema.TypeInterner, result, nil) != returnOriginRefFree || b.analyzer.loanCarrier(result) {
		return false
	}
	for _, operand := range operands {
		if _, converted := u.Sema.ImplicitConversions[operand]; converted {
			return false
		}
	}
	fn, reason := b.analyzer.selectedCallableFunction(u, selected)
	if reason != "" {
		return false
	}
	c, in := fn.candidate, u.Sema.TypeInterner
	if c.Name != name || len(fn.info.Params) != arity || !c.HasBody || !fn.item.Body.IsValid() || c.Async || len(c.TemplateParams) != 0 ||
		returnOriginContainerFormal(in, fn.info.Params) || returnOriginCallHasUnprovedEffects(in, fn.info.Params) {
		return false
	}
	summary := b.analyzer.summaries[fn.key].value
	if summary.normal && (len(summary.roots) != 0 || len(summary.callables) != 0) {
		return false
	}
	span := u.Builder.Exprs.Get(id).Span
	if b.inheritRequirements(fn, returnOriginView(fn), span).failed() {
		return false
	}
	out.storage = returnOriginValue{}
	if !summary.normal {
		out.value, out.flow.normal = returnOriginValue{}, returnOriginEnv{}
		return true
	}
	out.value = returnOriginValueOf()
	return true
}
