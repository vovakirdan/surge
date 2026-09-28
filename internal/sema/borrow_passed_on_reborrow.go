package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A local `&mut` reference bound from a call handed an existing reference that
// holds no loan of this function -- `let q = idam(r)`, `let q =
// idam(idam(r))` with `r: &mut int[]` a parameter -- pointed at r's referent
// while holding nothing on it. An exclusive use through q was checked on q's
// place only, so `let e = &mut r[0]; app(q); *e = 5;` grew the array under e,
// and a shared use through r was checked against no exclusive loan, so `let e
// = &mut q[0]; let v = r[0].bytes(); *e = ..` freed what v read. The same
// binding from a local borrow (`let q = idam(&mut x)`) stands on that borrow's
// loan. Here q takes an exclusive reborrow of r's referent instead, held to
// its scope: a use through q is then checked on the referent, and a borrow
// through r conflicts with q while q lives, as `let q = &mut *r` does.

// reborrowPassedOnMutReference takes that reborrow for a `let` binding of a
// `&mut` reference and returns it, or NoBorrowID when the value is not such an
// alias or the reborrow is refused (the conflict is reported).
func (tc *typeChecker) reborrowPassedOnMutReference(symID symbols.SymbolID, boundType types.TypeID, expr ast.ExprID) BorrowID {
	sym := tc.symbolFromID(symID)
	if sym == nil || sym.Kind != symbols.SymbolLet || !tc.isMutRefType(tc.resolveAlias(boundType)) {
		return NoBorrowID
	}
	// `let q = r` moves r: q is the only path left, and needs no loan.
	if _, isCall := tc.builder.Exprs.Call(tc.unwrapGroupExpr(expr)); !isCall {
		return NoBorrowID
	}
	src := tc.passedOnMutReferenceSource(expr, 0)
	if !src.IsValid() {
		return NoBorrowID
	}
	desc, ok := tc.resolvePlace(src)
	if !ok {
		return NoBorrowID
	}
	place, parent := tc.loanPlace(desc)
	scope := tc.currentScope()
	if !place.IsValid() || !scope.IsValid() {
		return NoBorrowID
	}
	span := tc.exprSpan(expr)
	bid, issue := tc.borrow.BeginBorrow(tc.unwrapGroupExpr(expr), span, BorrowMut, place, scope, parent)
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
		return NoBorrowID
	}
	return bid
}

// passedOnMutReferenceSource names the reference place a `&mut` value passes
// on: a `&mut` reference place that stands on no loan of this function, or a
// call whose result can alias exactly one `&mut` argument (or receiver) that
// itself passes one on. Anything else names none.
func (tc *typeChecker) passedOnMutReferenceSource(expr ast.ExprID, depth int) ast.ExprID {
	expr = tc.unwrapGroupExpr(expr)
	if !expr.IsValid() || depth > 8 || tc.isBorrowExpr(expr) ||
		!tc.isMutRefType(tc.resolveAlias(tc.result.ExprTypes[expr])) {
		return ast.NoExprID
	}
	if desc, ok := tc.resolvePlace(expr); ok {
		if !desc.Base.IsValid() || tc.bindingBorrow[desc.Base] != NoBorrowID {
			return ast.NoExprID
		}
		return expr
	}
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
	var candidates []ast.ExprID
	offset := 0
	if sym.Signature.HasSelf {
		offset = 1
		if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil && len(params) > 0 &&
			tc.refResultCanAliasParam(result, params[0]) {
			candidates = append(candidates, member.Target)
		}
	}
	for i, arg := range call.Args {
		if i+offset >= len(params) {
			break
		}
		if tc.refResultCanAliasParam(result, params[i+offset]) {
			candidates = append(candidates, arg.Value)
		}
	}
	if len(candidates) != 1 {
		return ast.NoExprID
	}
	return tc.passedOnMutReferenceSource(candidates[0], depth+1)
}

// A `&mut` alias a call returned from SEVERAL references -- `let q = pickm(r,
// s)` -- may point at either, so it holds no single reborrow. A holder taken
// through it and an exclusive use through it are answered on each source
// instead.

// exclusiveSourceLoans: an exclusive loan a binding holds on a place spelled
// through such an alias q -- `let e = &mut q[0]` -- is taken again on the same
// path under each reference q was given (`r[..]`, `s[..]`), so a view or an
// element taken through r while e lives conflicts with it, and a grow through
// r is refused as it is under `let e = &mut r[0]`.
func (tc *typeChecker) exclusiveSourceLoans(info *BorrowInfo, span source.Span) []BorrowID {
	base := info.Place.Base
	if !tc.isUnrootedLocalReference(base) {
		return nil
	}
	suffix := tc.borrow.placeSegments(info.Place)
	scope := tc.currentScope()
	if !scope.IsValid() {
		return nil
	}
	var out []BorrowID
	for _, src := range tc.aliasCallSources(base) {
		for _, ref := range tc.passedOnReferencePlaces(src, 0) {
			desc, ok := tc.resolvePlace(ref)
			if !ok {
				continue
			}
			at, parent := tc.loanPlace(desc)
			if !at.IsValid() {
				continue
			}
			place := tc.borrow.CanonicalPlace(at.Base, append(tc.borrow.placeSegments(at), suffix...))
			if bid := tc.borrow.ExprBorrow(ref); bid != NoBorrowID {
				if held := tc.borrow.Info(bid); held != nil && held.Kind == BorrowMut && held.Place == place {
					out = append(out, bid)
					continue
				}
			}
			bid, issue := tc.borrow.BeginBorrow(ref, span, BorrowMut, place, scope, parent)
			if issue.Kind != BorrowIssueNone {
				tc.reportBorrowConflict(place, span, issue, BorrowMut)
				return out
			}
			if tc.aliasSourceHolder == nil {
				tc.aliasSourceHolder = make(map[BorrowID]BorrowID)
			}
			tc.aliasSourceHolder[bid] = info.ID
			out = append(out, bid)
		}
	}
	return out
}

// refuseExclusiveUseOverAliasSources: an exclusive use through such an alias
// -- `app(q)`, `*q = ..` -- is checked as a write through each reference q was
// given, so a view or an element taken through r does not outlive it. The
// loans taken on the sources for the writer itself (holder) or a loan it was
// reborrowed through are its own authority.
func (tc *typeChecker) refuseExclusiveUseOverAliasSources(place Place, holder BorrowID, span source.Span) bool {
	if !tc.isUnrootedLocalReference(place.Base) {
		return false
	}
	for _, src := range tc.aliasCallSources(place.Base) {
		for _, ref := range tc.passedOnReferencePlaces(src, 0) {
			desc, ok := tc.resolvePlace(ref)
			if !ok {
				continue
			}
			at, parent := tc.loanPlace(desc)
			issue := tc.borrow.writeThroughAllowedExcept(at, parent, func(bid BorrowID) bool {
				owner, ok := tc.aliasSourceHolder[bid]
				return ok && tc.borrow.isAncestorOrSelf(owner, holder)
			})
			if issue.Kind == BorrowIssueNone {
				continue
			}
			if issue.Kind == BorrowIssueFrozen {
				issue.Kind = BorrowIssueConflictShared
			} else {
				issue.Kind = BorrowIssueConflictMut
			}
			tc.reportBorrowConflict(at, span, issue, BorrowMut)
			return true
		}
	}
	return false
}

// isUnrootedLocalReference: a `let` reference that stands on no loan of this
// function (one whose reborrow was refused is already reported).
func (tc *typeChecker) isUnrootedLocalReference(base symbols.SymbolID) bool {
	if !base.IsValid() || tc.bindingBorrow[base] != NoBorrowID || !tc.isReferenceType(tc.bindingType(base)) {
		return false
	}
	if _, refused := tc.refusedAlias[base]; refused {
		return false
	}
	sym := tc.symbolFromID(base)
	return sym != nil && sym.Kind == symbols.SymbolLet
}

// aliasCallSources: the values a local reference was given, other than a
// reference moved into it (`let r3 = r` leaves r dead, and a use of r is
// refused as a use of a moved value): calls, and compare, ternary or block
// values choosing among them.
func (tc *typeChecker) aliasCallSources(base symbols.SymbolID) []ast.ExprID {
	var out []ast.ExprID
	for _, src := range tc.lentValues.sources[base] {
		if _, isPlace := tc.resolvePlace(tc.unwrapGroupExpr(src)); !isPlace {
			out = append(out, src)
		}
	}
	return out
}

// passedOnReferencePlaces names every reference place a value passes on: the
// place itself, or through a call whose result can alias them, each argument
// (and receiver) the result may alias.
func (tc *typeChecker) passedOnReferencePlaces(expr ast.ExprID, depth int) []ast.ExprID {
	expr = tc.unwrapGroupExpr(expr)
	if !expr.IsValid() || depth > 8 || tc.isBorrowExpr(expr) {
		return nil
	}
	if _, ok := tc.resolvePlace(expr); ok {
		if tc.isReferenceType(tc.result.ExprTypes[expr]) {
			return []ast.ExprID{expr}
		}
		return nil
	}
	call, ok := tc.builder.Exprs.Call(expr)
	if !ok || call == nil {
		var out []ast.ExprID
		for _, value := range tc.choiceValues(expr) {
			out = append(out, tc.passedOnReferencePlaces(value, depth+1)...)
		}
		return out
	}
	sym := tc.symbolFromID(tc.symbolForExpr(expr))
	if sym == nil || sym.Signature == nil {
		return nil
	}
	result := tc.result.ExprTypes[expr]
	params := sym.Signature.Params
	var out []ast.ExprID
	offset := 0
	if sym.Signature.HasSelf {
		offset = 1
		if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil && len(params) > 0 &&
			tc.refResultCanAliasParam(result, params[0]) {
			out = append(out, tc.passedOnReferencePlaces(member.Target, depth+1)...)
		}
	}
	for i, arg := range call.Args {
		if i+offset < len(params) && tc.refResultCanAliasParam(result, params[i+offset]) {
			out = append(out, tc.passedOnReferencePlaces(arg.Value, depth+1)...)
		}
	}
	return out
}
