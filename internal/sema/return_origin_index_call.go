package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
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
//   - An erased result carries no origin. A borrowed result substitutes the
//     checked body's exact parameter roots through the already evaluated target
//     and index operands, just as an ordinary call does.
//   - No formal is `&mut`, and every formal passes the opaque-call effect rule, so
//     the call writes through nothing the caller can see.
//   - A by-value formal whose type holds no borrow erases whatever loan its actual
//     carries; that stays the loan-discard refusal an ordinary call reports.
//   - A source body contributes its checked summary and requirements; a body-less
//     declaration its declared promise, which for an erased result names nothing.
//
// Anything else keeps the unanswered refusal.
func (b *returnOriginBody) selectedIndexCall(out *returnOriginExprResult, id ast.ExprID, data *ast.ExprIndexData, target, index *returnOriginExprResult) string {
	u := b.function.unit
	in := u.Sema.TypeInterner
	if _, builtin := returnOriginIndexContainer(in, u.Sema.ExprTypes[data.Target]); builtin {
		return returnOriginIndexUnanswered
	}
	var fn *returnOriginFunction
	if selected, present := u.Sema.IndexSymbols[id]; present {
		var reason string
		fn, reason = b.analyzer.selectedIndexFunction(u, id)
		if reason != "" {
			sym := u.Symbols.Table.Symbols.Get(selected)
			if sym == nil || sym.Flags&symbols.SymbolFlagImported == 0 {
				return reason
			}
			fn = b.uniqueImportedIndexFunction(id, data)
			if fn == nil {
				return reason
			}
		}
	} else {
		fn = b.uniqueImportedIndexFunction(id, data)
		if fn == nil {
			return returnOriginIndexUnanswered
		}
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
	span := u.Builder.Exprs.Get(id).Span
	actualValues := [...]returnOriginCallValue{
		{value: target.value, storage: target.storage},
		{value: index.value, storage: index.storage},
	}
	actualExprs := [...]ast.ExprID{data.Target, data.Index}
	actuals := make([]returnOriginValue, len(actualValues))
	for i := range actualValues {
		if i == 0 && selectedIndexImplicitReceiver(in, u.Sema.ExprTypes[data.Target], fn.info.Params[0]) &&
			actualValues[i].storage.normal && len(actualValues[i].storage.roots) != 0 {
			actuals[i] = actualValues[i].storage.clone()
		} else {
			actuals[i] = b.callArgumentOrigin(actualExprs[i], fn.info.Params[i], &actualValues[i], false)
		}
	}
	b.guardIndexActuals(fn, span, actuals...)

	var summary returnOriginValue
	if fn.item.Body.IsValid() {
		summary = b.analyzer.summaries[fn.key].value
		if b.inheritRequirements(fn, returnOriginView(fn), span).failed() && summary.normal {
			return returnOriginIndexUnanswered
		}
	} else {
		slots, valid := b.declaredFunctionSources(fn, span)
		summary = b.opaqueReturnSources(fn, fn.info, slots, valid, span)
	}
	if !summary.normal {
		out.value, out.storage, out.flow.normal = returnOriginValue{}, returnOriginValue{}, returnOriginEnv{}
		return ""
	}
	if len(summary.callables) != 0 {
		return returnOriginIndexUnanswered
	}
	value := returnOriginValueOf()
	for _, root := range summary.roots {
		if root.kind != returnOriginParam || int64(root.param) >= int64(len(actuals)) {
			return returnOriginIndexUnanswered
		}
		value = value.join(actuals[root.param])
	}
	result := u.Sema.ExprTypes[id]
	if returnOriginTypeShape(in, result, nil) == returnOriginRefFree && !b.analyzer.loanCarrier(result) {
		if len(summary.roots) != 0 {
			return returnOriginIndexUnanswered
		}
		value = returnOriginValueOf()
	}
	b.checkExpired(value, span)
	out.value = value
	out.storage = returnOriginValue{}
	if returnOriginIsReference(in, result) {
		out.storage = value.clone()
	}
	return ""
}

func selectedIndexImplicitReceiver(in *types.Interner, actual, formal types.TypeID) bool {
	if returnOriginIsReference(in, actual) {
		return false
	}
	formal = returnOriginResolveAlias(in, formal)
	ref, ok := in.Lookup(formal)
	return ok && ref.Kind == types.KindReference && !ref.Mutable && returnOriginResolveAlias(in, ref.Elem) == returnOriginResolveAlias(in, actual)
}

// Some imported non-generic magic index calls reach HIR with their exact call
// but without a root-file IndexSymbols entry. Recover only a unique source body
// whose existing declaration/type agreement names this exact operation.
func (b *returnOriginBody) uniqueImportedIndexFunction(id ast.ExprID, data *ast.ExprIndexData) *returnOriginFunction {
	var found *returnOriginFunction
	for _, fn := range b.analyzer.functions {
		agrees := fn != nil && fn.unit != b.function.unit && fn.item.Body.IsValid() && b.indexCallAgrees(fn, id, data)
		if !agrees {
			continue
		}
		if found != nil {
			if found.key == fn.key && found.canonicalSourceKey == fn.canonicalSourceKey ||
				b.analyzer.sameTemplateBody(found.candidate.Symbol, fn.candidate.Symbol) {
				continue
			}
			return nil
		}
		found = fn
	}
	return found
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
	return b.indexSignatureAgrees(fn.candidate, fn.info.Params, fn.info.Result, id, data)
}

func (b *returnOriginBody) indexSignatureAgrees(c *CallableCandidate, params []types.TypeID, result types.TypeID, id ast.ExprID, data *ast.ExprIndexData) bool {
	u := b.function.unit
	in := u.Sema.TypeInterner
	if c == nil || c.Name != "__index" || !c.HasSelf || len(params) != 2 {
		return false
	}
	if !returnOriginIndexTypeEquivalent(in, result, u.Sema.ExprTypes[id]) ||
		!returnOriginIndexTypeEquivalent(in, params[1], u.Sema.ExprTypes[data.Index]) {
		return false
	}
	target, receiver := u.Sema.ExprTypes[data.Target], params[0]
	self, typed := in.Lookup(receiver)
	if !typed {
		return false
	}
	switch {
	case returnOriginIndexTypeEquivalent(in, receiver, target) && self.Kind == types.KindReference:
		return returnOriginIndexTypeEquivalent(in, c.ReceiverType, self.Elem)
	case returnOriginIndexTypeEquivalent(in, receiver, target):
		return returnOriginIndexTypeEquivalent(in, c.ReceiverType, receiver)
	default:
		return self.Kind == types.KindReference && returnOriginIndexTypeEquivalent(in, self.Elem, target) &&
			returnOriginIndexTypeEquivalent(in, c.ReceiverType, target)
	}
}

func returnOriginIndexTypeEquivalent(in *types.Interner, left, right types.TypeID) bool {
	left, right = returnOriginResolveAlias(in, left), returnOriginResolveAlias(in, right)
	if left == right {
		return true
	}
	lt, lok := in.Lookup(left)
	rt, rok := in.Lookup(right)
	if !lok || !rok || lt.Kind != rt.Kind || lt.Mutable != rt.Mutable || lt.Width != rt.Width || lt.Count != rt.Count {
		return false
	}
	switch lt.Kind {
	case types.KindReference, types.KindOwn, types.KindPointer, types.KindFar, types.KindArray:
		return returnOriginIndexTypeEquivalent(in, lt.Elem, rt.Elem)
	case types.KindBool, types.KindInt, types.KindUint, types.KindFloat, types.KindString,
		types.KindNothing, types.KindUnit, types.KindConst:
		return true
	}
	l, lok := in.StructInfo(left)
	r, rok := in.StructInfo(right)
	if !lok || !rok || l == nil || r == nil || l.Name != r.Name || l.Decl.Start != r.Decl.Start || l.Decl.End != r.Decl.End ||
		len(l.TypeArgs) != len(r.TypeArgs) {
		return false
	}
	for i := range l.TypeArgs {
		if !returnOriginIndexTypeEquivalent(in, l.TypeArgs[i], r.TypeArgs[i]) {
			return false
		}
	}
	return true
}
