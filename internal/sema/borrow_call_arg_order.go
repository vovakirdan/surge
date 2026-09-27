package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// An element or a returned reference reached through a reference and handed
// to a `&` parameter -- `use2(r[0], appr(r))`, `use2(first(r), appr(r))` with
// `r: &mut int[]` -- is a reference into r's referent that the callee reads
// after every argument is evaluated. A later argument that grows, replaces or
// hands r on exclusively frees what the earlier one points into. The owned
// form keeps `xs` borrowed from its index to the call (`use2(xs[0],
// appr(&mut xs))` is refused); the reference took no loan to do the same, so
// each call asks it here: an exclusive use through a reference, recorded as
// it is checked, that sits inside a later argument and reaches a place an
// earlier such argument reaches is refused.

type exclusiveRefUse struct {
	place Place
	span  source.Span
}

// noteExclusiveRefUse records an exclusive use of a referent through a
// reference -- a `&mut` hand-off of a reference place, or a store through one
// -- for the enclosing call's argument check. The list lives for one
// outermost statement.
func (tc *typeChecker) noteExclusiveRefUse(place Place, span source.Span) {
	if place.IsValid() && len(tc.tempFrames) > 0 {
		tc.exclusiveRefUses = append(tc.exclusiveRefUses, exclusiveRefUse{place: place, span: span})
	}
}

// argumentInOrder is one argument of a call in evaluation order, with the
// parameter it is handed to.
type argumentInOrder struct {
	expr  ast.ExprID
	ty    types.TypeID
	param symbols.TypeKey
}

// refuseLaterArgumentOverEarlierElement checks one call's arguments in
// evaluation order.
func (tc *typeChecker) refuseLaterArgumentOverEarlierElement(args []argumentInOrder) {
	if tc.borrow == nil || len(tc.exclusiveRefUses) == 0 || len(args) < 2 {
		return
	}
	for i := 0; i+1 < len(args); i++ {
		param := strings.TrimSpace(string(args[i].param))
		if !strings.HasPrefix(param, "&") || strings.HasPrefix(param, "&mut ") || !tc.isReferenceType(args[i].ty) {
			continue
		}
		reach := tc.elementArgumentReach(args[i].expr)
		if len(reach) == 0 {
			continue
		}
		for _, later := range args[i+1:] {
			span := tc.exprSpan(later.expr)
			for _, use := range tc.exclusiveRefUses {
				// The later argument handed on itself (`usem(r[0], r)`) is
				// checked with the call's other reference arguments (noteRefArg).
				if !spanWithin(use.span, span) || use.span == tc.exprSpan(tc.unwrapGroupExpr(later.expr)) {
					continue
				}
				for _, at := range reach {
					if placesOverlap(at.place, use.place) {
						tc.reportBorrowConflict(use.place, use.span, BorrowIssue{Kind: BorrowIssueConflictShared}, BorrowMut)
						return
					}
				}
			}
		}
	}
}

// elementArgumentReach: the places an argument that is an element or a
// returned reference reaches through a reference (referentReach); a plain
// reference place or an explicit borrow reaches none here -- the first is
// checked with the call's other reference arguments (noteRefArg), the second
// holds its own loan.
func (tc *typeChecker) elementArgumentReach(arg ast.ExprID) []reached {
	arg = tc.unwrapGroupExpr(arg)
	if index, ok := tc.builder.Exprs.Index(arg); ok && index != nil {
		if _, isPlace := tc.resolvePlace(tc.unwrapGroupExpr(index.Target)); !isPlace {
			return tc.referentReach(index.Target, true, make(map[symbols.SymbolID]bool))
		}
		return tc.referentReach(arg, true, make(map[symbols.SymbolID]bool))
	}
	if call, ok := tc.builder.Exprs.Call(arg); ok && call != nil {
		return tc.referentReach(arg, true, make(map[symbols.SymbolID]bool))
	}
	return nil
}

func spanWithin(inner, outer source.Span) bool {
	return inner.File == outer.File && outer.Start <= inner.Start && inner.End <= outer.End && !outer.Empty()
}
