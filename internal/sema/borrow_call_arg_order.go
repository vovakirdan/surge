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
//
// A local reference that stands on no loan of this function reaches what it
// was given (`let q = id(r)`), so the use is recorded on each of those places
// too: `use2(r[0], appr(q))` grows the array r[0] points into.
func (tc *typeChecker) noteExclusiveRefUse(place Place, span source.Span) {
	if !place.IsValid() || len(tc.tempFrames) == 0 {
		return
	}
	tc.exclusiveRefUses = append(tc.exclusiveRefUses, exclusiveRefUse{place: place, span: span})
	seen := map[symbols.SymbolID]bool{place.Base: true}
	for _, src := range tc.lentValues.sources[place.Base] {
		for _, at := range tc.referentReach(src, true, seen) {
			tc.exclusiveRefUses = append(tc.exclusiveRefUses, exclusiveRefUse{place: at.place, span: span})
		}
	}
}

// noteExclusiveBorrowThroughReference: a new `&mut` borrow of a place spelled
// through a reference -- `&mut r[0]`, `&mut (*k).items`, or `k.items` handed to
// a `&mut` parameter -- is an exclusive use of the referent the same as
// handing the reference on. An element read through the reference as an
// earlier argument took no loan the borrow could conflict with, so the use is
// recorded for the call's argument check (`use2(k.items[0], appr(&mut
// (*k).items))`). A refused borrow records nothing.
func (tc *typeChecker) noteExclusiveBorrowThroughReference(desc placeDescriptor, place Place, span source.Span, issue BorrowIssueKind) {
	if issue == BorrowIssueNone && desc.Base.IsValid() && tc.isReferenceType(tc.bindingType(desc.Base)) {
		tc.noteExclusiveRefUse(place, span)
	}
}

// applyBinaryOperandsOwnership applies a binary operator's two operands in
// evaluation order. The left operand is evaluated first: `xs[0] + grow(xs)` on
// `xs: &mut string[]` hands the operator an element the right operand may free,
// as a call's earlier argument would be.
func (tc *typeChecker) applyBinaryOperandsOwnership(params []symbols.TypeKey, left ast.ExprID, leftType types.TypeID, right ast.ExprID, rightType types.TypeID) {
	tc.applyParamOwnership(params[0], left, leftType, tc.exprSpan(left))
	tc.applyParamOwnership(params[1], right, rightType, tc.exprSpan(right))
	tc.refuseLaterArgumentOverEarlierElement([]argumentInOrder{
		{expr: left, ty: leftType, param: params[0]},
		{expr: right, ty: rightType, param: params[1]},
	})
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
	if tc.borrow == nil || len(tc.exclusiveRefUses) == 0 || len(args) < 2 || (tc.mutArgs != nil && tc.mutArgs.refused) {
		return
	}
	for i := 0; i+1 < len(args); i++ {
		param := strings.TrimSpace(string(args[i].param))
		if strings.HasPrefix(param, "&") {
			if !tc.isReferenceType(args[i].ty) {
				continue
			}
		} else if carries, _ := tc.carriesAnyReference(args[i].ty); !carries {
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
// holds its own loan. A compare, ternary or block value reaches what each of
// its values does.
func (tc *typeChecker) elementArgumentReach(arg ast.ExprID) []reached {
	arg = tc.unwrapGroupExpr(arg)
	if values := tc.choiceValues(arg); len(values) > 0 {
		out := make([]reached, 0, len(values))
		for _, value := range values {
			out = append(out, tc.elementArgumentReach(value)...)
		}
		return out
	}
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
