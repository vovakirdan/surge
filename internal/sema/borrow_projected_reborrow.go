package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A `&mut` reference a call returns INTO an array or a string it was handed --
// `let e = firstm(r)` with `fn firstm(s: &mut int[]) -> &mut int` -- points
// into the buffer, not at the value. Standing on a loan of the whole referent
// (the alias's reborrow, or the loan of a local reference it inherited), it
// was excused as part of the reference's own chain, so `seta(r)` or `app(q)`
// replaced the buffer and `*e = 3` wrote freed memory (valgrind: invalid read
// and write). `let e = &mut r[0]` is refused there; the binding now takes the
// same exclusive loan on an element of the referent, reborrowed from what the
// reference stands on.

// projectedMutReborrow takes that loan for a `let` binding and reports whether
// the value is such a projection. A refused loan is reported, and the binding
// then holds none. A value chosen by a compare, a ternary or a block is walked
// to each call it chooses among.
func (tc *typeChecker) projectedMutReborrow(symID symbols.SymbolID, boundType types.TypeID, expr ast.ExprID) (BorrowID, bool) {
	sym := tc.symbolFromID(symID)
	if sym == nil || sym.Kind != symbols.SymbolLet || !tc.isMutRefType(tc.resolveAlias(boundType)) {
		return NoBorrowID, false
	}
	scope := tc.currentScope()
	if !scope.IsValid() {
		return NoBorrowID, false
	}
	var loans []BorrowID
	refused := tc.projectionLoans(tc.unwrapGroupExpr(expr), scope, &loans)
	if refused {
		if tc.refusedAlias == nil {
			tc.refusedAlias = make(map[symbols.SymbolID]struct{})
		}
		tc.refusedAlias[symID] = struct{}{}
		return NoBorrowID, true
	}
	if len(loans) == 0 {
		return NoBorrowID, false
	}
	if len(loans) > 1 {
		if tc.projectionSiblings == nil {
			tc.projectionSiblings = make(map[BorrowID][]BorrowID)
		}
		tc.projectionSiblings[loans[0]] = append([]BorrowID(nil), loans[1:]...)
	}
	tc.holdLoansAsViewLoans(symID, loans[1:], loans[0])
	return loans[0], true
}

// projectionLoans takes the loans of a projected value: for a call, on each
// candidate place inside each reference its argument passes on; for a choice,
// those of each value it chooses among. It reports a refused loan (already
// reported) and takes no more after it. The values of a choice are
// alternatives: a place two of them point at holds one loan, not two that
// conflict. The walk follows sub-expressions only, so it ends without a depth
// bound; a bound would drop the loan of a deeper value.
func (tc *typeChecker) projectionLoans(expr ast.ExprID, scope symbols.ScopeID, loans *[]BorrowID) bool {
	expr = tc.unwrapGroupExpr(expr)
	if !expr.IsValid() {
		return false
	}
	if values := tc.choiceValues(expr); len(values) > 0 {
		for _, value := range values {
			if tc.projectionLoans(value, scope, loans) {
				return true
			}
		}
		return false
	}
	result, ok := tc.types.Lookup(tc.resolveAlias(tc.result.ExprTypes[expr]))
	if !ok || result.Kind != types.KindReference || !result.Mutable {
		return false
	}
	arg := tc.singleAliasedArgument(expr)
	if !arg.IsValid() {
		return false
	}
	return tc.takeProjectionLoans(expr, arg, result.Elem, scope, loans)
}

// singleAliasedArgument: the one argument (or receiver) of a call that its
// reference result can alias, or none.
func (tc *typeChecker) singleAliasedArgument(expr ast.ExprID) ast.ExprID {
	call, ok := tc.builder.Exprs.Call(expr)
	if !ok || call == nil {
		return ast.NoExprID
	}
	sym := tc.symbolFromID(tc.symbolForExpr(expr))
	if sym == nil || sym.Signature == nil {
		return ast.NoExprID
	}
	result := tc.result.ExprTypes[expr]
	params := sym.Signature.Params
	found := ast.NoExprID
	count := 0
	offset := 0
	if sym.Signature.HasSelf {
		offset = 1
		if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil && len(params) > 0 &&
			tc.refResultCanAliasParam(result, params[0]) {
			found, count = member.Target, count+1
		}
	}
	for i, arg := range call.Args {
		if i+offset < len(params) && tc.refResultCanAliasParam(result, params[i+offset]) {
			found, count = arg.Value, count+1
		}
	}
	if count != 1 {
		return ast.NoExprID
	}
	return found
}
