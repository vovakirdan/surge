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
// then holds none.
func (tc *typeChecker) projectedMutReborrow(symID symbols.SymbolID, boundType types.TypeID, expr ast.ExprID) (BorrowID, bool) {
	sym := tc.symbolFromID(symID)
	if sym == nil || sym.Kind != symbols.SymbolLet || !tc.isMutRefType(tc.resolveAlias(boundType)) {
		return NoBorrowID, false
	}
	expr = tc.unwrapGroupExpr(expr)
	arg := tc.singleAliasedArgument(expr)
	if !arg.IsValid() || !tc.projectsIntoBuffer(tc.result.ExprTypes[expr], tc.result.ExprTypes[arg]) {
		return NoBorrowID, false
	}
	refs := tc.passedOnReferencePlaces(arg, 0)
	if len(refs) == 0 {
		return NoBorrowID, false
	}
	scope := tc.currentScope()
	if !scope.IsValid() {
		return NoBorrowID, false
	}
	// An argument that may alias several references (`firstm(pickm(r, s))`)
	// points into one of them: the binding holds an element loan on each.
	span := tc.exprSpan(expr)
	var loans []BorrowID
	for i, ref := range refs {
		desc, ok := tc.resolvePlace(ref)
		if !ok {
			continue
		}
		at, parent := tc.loanPlace(desc)
		if !at.IsValid() {
			continue
		}
		place := tc.borrow.CanonicalPlace(at.Base, append(tc.borrow.placeSegments(at), PlaceSegment{Kind: PlaceSegmentIndex}))
		key := expr
		if i > 0 {
			key = ref
		}
		bid, issue := tc.borrow.BeginBorrow(key, span, BorrowMut, place, scope, parent)
		tc.recordBorrowEvent(&BorrowEvent{
			Kind:        BorrowEvBorrowStart,
			Borrow:      bid,
			BorrowKind:  BorrowMut,
			Place:       place,
			Span:        span,
			Scope:       scope,
			Issue:       issue.Kind,
			IssueBorrow: issue.Borrow,
		})
		if issue.Kind != BorrowIssueNone {
			tc.reportBorrowConflict(place, span, issue, BorrowMut)
			if tc.refusedAlias == nil {
				tc.refusedAlias = make(map[symbols.SymbolID]struct{})
			}
			tc.refusedAlias[symID] = struct{}{}
			return NoBorrowID, true
		}
		loans = append(loans, bid)
	}
	if len(loans) == 0 {
		return NoBorrowID, false
	}
	tc.holdLoansAsViewLoans(symID, loans[1:], loans[0])
	return loans[0], true
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

// projectsIntoBuffer: the result references something other than what the
// argument references, and the argument's referent is an array or a string.
func (tc *typeChecker) projectsIntoBuffer(result, arg types.TypeID) bool {
	res, ok := tc.types.Lookup(tc.resolveAlias(result))
	if !ok || res.Kind != types.KindReference || !res.Mutable {
		return false
	}
	given, ok := tc.types.Lookup(tc.resolveAlias(arg))
	if !ok || given.Kind != types.KindReference || tc.resolveAlias(res.Elem) == tc.resolveAlias(given.Elem) {
		return false
	}
	if _, _, fixed := tc.arrayFixedInfo(given.Elem); fixed {
		return false
	}
	if tc.isArrayType(given.Elem) {
		return true
	}
	inner, ok := tc.types.Lookup(tc.resolveAlias(given.Elem))
	return ok && inner.Kind == types.KindString
}
