package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// projectionSource is one referent a call's `&mut` result may point into: the
// loan place of a reference the call's argument passes on, and the loan it
// stands on. An explicit reborrow handed in -- `it0(&mut *k)`,
// `(&mut *m).get_mut(..)` -- is its own loan's place, reborrowed from it.
type projectionSource struct {
	at     Place
	parent BorrowID
	expr   ast.ExprID
	borrow bool
}

// projectionSources names every such referent of expr: a reference place, an
// explicit borrow, or through a call whose result can alias them (or a choice
// among values) each argument it may alias. It follows sub-expressions only
// and has no depth bound, so a deep nesting cannot drop a source.
func (tc *typeChecker) projectionSources(expr ast.ExprID) []projectionSource {
	expr = tc.unwrapGroupExpr(expr)
	if !expr.IsValid() {
		return nil
	}
	if tc.isBorrowExpr(expr) {
		bid := tc.borrow.ExprBorrow(expr)
		if info := tc.borrow.Info(bid); info != nil {
			return []projectionSource{{at: info.Place, parent: bid, expr: expr, borrow: true}}
		}
		return nil
	}
	if desc, ok := tc.resolvePlace(expr); ok {
		if !tc.isReferenceType(tc.result.ExprTypes[expr]) {
			return nil
		}
		at, parent := tc.loanPlace(desc)
		if !at.IsValid() {
			return nil
		}
		return []projectionSource{{at: at, parent: parent, expr: expr}}
	}
	call, ok := tc.builder.Exprs.Call(expr)
	if !ok || call == nil {
		var out []projectionSource
		for _, value := range tc.choiceValues(expr) {
			out = append(out, tc.projectionSources(value)...)
		}
		return out
	}
	sym := tc.symbolFromID(tc.symbolForExpr(expr))
	if sym == nil || sym.Signature == nil {
		return nil
	}
	result := tc.result.ExprTypes[expr]
	params := sym.Signature.Params
	var out []projectionSource
	offset := 0
	if sym.Signature.HasSelf {
		offset = 1
		if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil && len(params) > 0 &&
			tc.refResultCanAliasParam(result, params[0]) {
			out = append(out, tc.projectionSources(member.Target)...)
		}
	}
	for i, arg := range call.Args {
		if i+offset < len(params) && tc.refResultCanAliasParam(result, params[i+offset]) {
			out = append(out, tc.projectionSources(arg.Value)...)
		}
	}
	return out
}

// takeProjectionLoans takes, for a call expr whose `&mut resultElem` is
// reached from arg, an exclusive loan on every candidate place inside each
// source (candidatePaths), reborrowed from what the source stands on, and
// appends them to loans. A place already held by loans is not taken twice. A
// source with too many candidates holds one interior loan on the whole
// referent. It reports a refused loan (reported here).
func (tc *typeChecker) takeProjectionLoans(expr, arg ast.ExprID, resultElem types.TypeID, scope symbols.ScopeID, loans *[]BorrowID) bool {
	span := tc.exprSpan(expr)
	for _, src := range tc.projectionSources(arg) {
		given := tc.placeValueType(src.at)
		if given == types.NoTypeID {
			given = tc.valueType(tc.result.ExprTypes[src.expr])
		}
		paths, interior := tc.candidatePaths(given, resultElem)
		if interior {
			paths = [][]PlaceSegment{nil}
		}
		for _, path := range paths {
			place := tc.borrow.CanonicalPlace(src.at.Base, append(tc.borrow.placeSegments(src.at), path...))
			if slices.ContainsFunc(*loans, func(held BorrowID) bool {
				info := tc.borrow.Info(held)
				return info != nil && info.Place == place && info.Parent == src.parent
			}) {
				continue
			}
			// The first loan is the call's own; any other, and any loan keyed
			// by an explicit borrow, leaves that expression naming its own.
			var bid BorrowID
			var issue BorrowIssue
			if len(*loans) == 0 && !src.borrow && tc.borrow.ExprBorrow(expr) == NoBorrowID {
				bid, issue = tc.borrow.BeginBorrow(expr, span, BorrowMut, place, scope, src.parent)
			} else {
				bid, issue = tc.beginSideLoan(expr, span, BorrowMut, place, scope, src.parent)
			}
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
				// A hand-off of the reference to `&mut` that already refused
				// the whole referent is reported once.
				if tc.borrow.WriteThroughAllowed(src.at, src.parent).Kind == BorrowIssueNone {
					tc.reportBorrowConflict(place, span, issue, BorrowMut)
				}
				return true
			}
			if interior {
				tc.borrow.Info(bid).Interior = true
			}
			*loans = append(*loans, bid)
		}
	}
	return false
}

// candidatePaths: where a `&mut result` returned from a reference to given
// may point, below the referent. A dynamic array or a string is one buffer, so
// the result points at an element; a fixed array keeps its elements in place
// and names none; a record, a map or a tuple is walked (projectionPaths). The
// result of the referent's own type names no path. interior: too many
// candidates to name, so the whole referent is held as pointed into.
func (tc *typeChecker) candidatePaths(given, result types.TypeID) (paths [][]PlaceSegment, interior bool) {
	if given == types.NoTypeID || tc.resolveAlias(given) == tc.resolveAlias(result) {
		return nil, false
	}
	if _, _, fixed := tc.arrayFixedInfo(given); fixed {
		return nil, false
	}
	element := [][]PlaceSegment{{{Kind: PlaceSegmentIndex}}}
	if tc.isArrayType(given) {
		return element, false
	}
	if inner, ok := tc.types.Lookup(tc.resolveAlias(given)); ok && inner.Kind == types.KindString {
		return element, false
	}
	return tc.projectionPaths(given, result)
}

// borrowCarriedProjection answers a call whose result CARRIES a `&mut` inside
// a wrapper -- `m.get_mut(&k) -> Option<&mut V>` through `m: &mut Map`, `ent(k,
// &key) -> Option<&mut string>` through a record holding the map, a reference
// handed as `&mut *m` -- for the argument expr passed to param. Nothing binds
// such a result as a reference, so the loans are taken at the call, keyed by
// the argument: a `let` of the Option and a compare arm's binding hold them
// through it (inheritedBorrowForCall, viewLoansOfCall); otherwise they end
// with the statement. A bare `&mut` result is answered where it is bound
// (projectedMutReborrow).
func (tc *typeChecker) borrowCarriedProjection(expr ast.ExprID, param symbols.TypeKey, result types.TypeID) {
	carried, carries := tc.carriedReferenceType(result)
	if !carries || !tc.isMutRefType(carried) || tc.isReferenceType(tc.resolveAlias(result)) ||
		!tc.refResultCanAliasParam(result, param) {
		return
	}
	res, ok := tc.types.Lookup(carried)
	scope := tc.currentScope()
	if !ok || !scope.IsValid() {
		return
	}
	var loans []BorrowID
	if tc.takeProjectionLoans(expr, expr, res.Elem, scope, &loans) || len(loans) == 0 || len(tc.tempFrames) == 0 {
		return
	}
	// What a holder of the result finds is the argument's own loan -- the
	// first projection loan, or the explicit borrow's loan for `(&mut *m)`.
	// Every other loan ends with the statement unless a binding holds that
	// one (releaseStatementTemporaryLoans).
	via := tc.borrow.ExprBorrow(tc.unwrapGroupExpr(expr))
	frame := &tc.tempFrames[len(tc.tempFrames)-1]
	for _, bid := range loans {
		if bid != via && via != NoBorrowID {
			if tc.sideLoanVia == nil {
				tc.sideLoanVia = make(map[BorrowID]BorrowID)
			}
			tc.sideLoanVia[bid] = via
		}
		if !slices.Contains(frame.loans, bid) {
			frame.loans = append(frame.loans, bid)
		}
	}
}
