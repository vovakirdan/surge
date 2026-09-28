package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

const returnOriginIndexUnanswered = "index requires a non-scalar index transfer"

// selectedIndexCall answers `x[i]` over a target that is neither a string nor a
// built-in array when SEMA resolved it to an `__index` declaration. HIR lowers
// that index as a call of exactly the recorded symbol with (target, index) as
// its actuals (lowerIndexExpr, internal/hir/lower_expr.go), so it is answered by
// the rules an ordinary call to the same declaration obeys, restricted to what
// the result can carry:
//
//   - The selection is checked against the typed operation SEMA recorded on this
//     node (indexCallAgrees). A selection that disagrees, such as a sibling
//     overload substituted for the one the checker chose, is refused by name and
//     never re-selected here.
//   - No operand and no result is implicitly converted: returnOriginTypedIndex
//     refuses a conversion on the node or either operand before this runs.
//   - The result is erased: its type holds no borrow and is no loan carrier, so no
//     operand origin or storage loan can reach the value.
//   - No formal is `&mut`, and every formal passes the opaque-call effect rule, so
//     the call writes through nothing the caller can see.
//   - A by-value formal whose type holds no borrow erases whatever loan its actual
//     carries; that stays the loan-discard refusal an ordinary call reports.
//   - A source body contributes its checked summary and requirements; a body-less
//     declaration its declared promise, which for an erased result names nothing.
//
// Anything else keeps the unanswered refusal.
func (b *returnOriginBody) selectedIndexCall(out *returnOriginExprResult, id ast.ExprID, data *ast.ExprIndexData, target, index returnOriginValue) string {
	u := b.function.unit
	in := u.Sema.TypeInterner
	if _, present := u.Sema.IndexSymbols[id]; !present {
		return returnOriginIndexUnanswered
	}
	if _, builtin := returnOriginIndexContainer(in, u.Sema.ExprTypes[data.Target]); builtin {
		return returnOriginIndexUnanswered
	}
	fn, reason := b.analyzer.selectedIndexFunction(u, id)
	if reason != "" {
		return reason
	}
	if c := fn.candidate; c.Async || len(c.TemplateParams) != 0 || c.ReceiverTemplateArity != 0 {
		return returnOriginIndexUnanswered // a generic body needs its finalized concrete use
	}
	if !b.indexCallAgrees(fn, id, data) {
		return "selected index call disagrees with its original typed operation"
	}
	if returnOriginBytesViewReader(fn) {
		// The core byte read, certified as the scalar path certifies it.
		out.storage, out.value = returnOriginValue{}, returnOriginValueOf()
		return ""
	}
	for _, param := range fn.info.Params {
		if kind, reference := returnOriginFormalBorrowKind(in, param); reference && kind == BorrowMut {
			return returnOriginIndexUnanswered
		}
	}
	if returnOriginCallHasUnprovedEffects(in, fn.info.Params) {
		return returnOriginIndexUnanswered
	}
	// An erased result: its type holds no borrow and is no loan carrier, and the
	// declaration's view of it requires no borrowed state.
	result, required := u.Sema.ExprTypes[id], returnOriginView(fn).requirement(returnOriginNoBorrowedState, fn.info.Result)
	if returnOriginTypeShape(in, result, nil) != returnOriginRefFree || b.analyzer.loanCarrier(result) ||
		required.failed() || len(required.atoms) != 0 {
		return returnOriginIndexUnanswered
	}
	span := u.Builder.Exprs.Get(id).Span
	b.guardIndexActuals(fn, span, target, index)
	out.storage = returnOriginValue{}
	out.value = returnOriginValueOf()
	if !fn.item.Body.IsValid() {
		return ""
	}
	summary := b.analyzer.summaries[fn.key].value
	if summary.normal && (len(summary.roots) != 0 || len(summary.callables) != 0) {
		return returnOriginIndexUnanswered
	}
	if b.inheritRequirements(fn, returnOriginView(fn), span).failed() {
		return returnOriginIndexUnanswered
	}
	if !summary.normal {
		// A body that never returns leaves the index unreachable, as a call does.
		out.value, out.flow.normal = returnOriginValue{}, returnOriginEnv{}
	}
	return ""
}

// guardIndexActuals is the loan-discard guard of an ordinary call's by-value
// formals (loanGuardFormal): a formal of a non-generic declaration that is not a
// reference and whose type holds no borrow keeps no loan its actual carries.
func (b *returnOriginBody) guardIndexActuals(fn *returnOriginFunction, span source.Span, actuals ...returnOriginValue) {
	in := b.function.unit.Sema.TypeInterner
	for i, actual := range actuals {
		param := fn.info.Params[i]
		if !returnOriginIsReference(in, param) && returnOriginTypeShape(in, param, nil) == returnOriginRefFree {
			b.discardLoans(actual, span)
		}
	}
}

// indexCallAgrees compares the selected declaration with the typed operation on
// the index node by exact type identity, so neither a namesake receiver nor a
// sibling overload stands in for the declaration the checker selected. The
// receiver formal is the target's own type, or a borrow of it that the checker
// takes implicitly; either way it names the declaring receiver type. Whether a
// `&mut` receiver can be answered is the caller's question, not agreement's.
func (b *returnOriginBody) indexCallAgrees(fn *returnOriginFunction, id ast.ExprID, data *ast.ExprIndexData) bool {
	u := b.function.unit
	in := u.Sema.TypeInterner
	c := fn.candidate
	if c.Name != "__index" || fn.name != "__index" || !c.HasSelf || len(fn.info.Params) != 2 {
		return false
	}
	if fn.info.Result != u.Sema.ExprTypes[id] || fn.info.Params[1] != u.Sema.ExprTypes[data.Index] {
		return false
	}
	target, receiver := u.Sema.ExprTypes[data.Target], fn.info.Params[0]
	self, typed := in.Lookup(receiver)
	if !typed {
		return false
	}
	switch {
	case receiver == target && self.Kind == types.KindReference:
		return c.ReceiverType == self.Elem
	case receiver == target:
		return c.ReceiverType == receiver
	default:
		return self.Kind == types.KindReference && self.Elem == target && c.ReceiverType == target
	}
}
