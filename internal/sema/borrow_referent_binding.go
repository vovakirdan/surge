package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A binding that receives an element or a returned reference reached through
// an existing reference -- `let x = r[0]`, `let x = h.items[0]`, `let e =
// first(r)`, `let e = b.get(0)`, `let x = id(r)[0]` with `r: &mut int[]`,
// `h: &mut H`, `b: &mut Bx` -- points into the reference's referent. Handing
// that reference to the index or the call took no loan: a parameter carries
// none, and a local reference's own exclusive loan is the authority for
// `app(r)`, `r.push(..)` and `*r = ..`, not a conflict with them. So the
// binding held nothing that could refuse the grow, the replace or the store
// that frees what it reads. It takes a shared child loan on the referent
// instead, exactly as `let x = xs[0]` keeps `xs` borrowed, and holds it for as
// long as it lives.
//
// A local reference that stands on no loan of this function -- `let q =
// id(r)`, `let mut q = a` with `a` a parameter -- reaches what it was given,
// so a loan taken through q is also taken on each of those (`let q = id(r);
// let x = q[0]; app(r);` is refused).
//
// The same loans are taken for an element or a returned reference handed to a
// `&` parameter, as statement temporaries: `use2(r[0], appr(r))` lets the
// second argument grow the array the first one points into, as `use2(xs[0],
// appr(&mut xs))` does and is refused for.

// bindsSharedReference: the binding's value carries a shared reference, and
// the value takes no loan of its own (an explicit `&r[0]` does).
func (tc *typeChecker) bindsSharedReference(symID symbols.SymbolID, expr ast.ExprID) bool {
	if tc.borrow == nil || tc.result == nil || !expr.IsValid() || tc.borrow.ExprBorrow(tc.unwrapGroupExpr(expr)) != NoBorrowID {
		return false
	}
	return tc.carriesSharedReference(symID, expr)
}

func (tc *typeChecker) carriesSharedReference(symID symbols.SymbolID, expr ast.ExprID) bool {
	bound := tc.bindingType(symID)
	if bound == types.NoTypeID {
		bound = tc.result.ExprTypes[expr]
	}
	carried, carries := tc.carriedReferenceType(bound)
	return carries && !tc.isMutRefType(tc.resolveAlias(carried))
}

// referentLoansForBinding takes the loans a binding of expr needs (above) and
// returns them. An explicit shared borrow `&q[0]` holds its own loan on q's
// place, and still reaches what a local reference q was given.
func (tc *typeChecker) referentLoansForBinding(symID symbols.SymbolID, expr ast.ExprID) []BorrowID {
	if tc.borrow == nil || tc.result == nil || !expr.IsValid() {
		return nil
	}
	if tc.bindsSharedReference(symID, expr) {
		return tc.borrowReferentsThroughReferences(expr, nil)
	}
	unary, ok := tc.builder.Exprs.Unary(tc.unwrapGroupExpr(expr))
	if !ok || unary == nil || unary.Op != ast.ExprUnaryRef || !tc.carriesSharedReference(symID, expr) {
		return nil
	}
	w := referentWalk{tc: tc}
	w.sources(unary.Operand)
	return w.out
}

// borrowReferentsThroughReferences takes a shared loan on each referent a
// value reaches through an existing reference and returns the loans, appended
// to out: the target of an index that is itself a reference (`r[0]`,
// `h.items[0]`, `r[0][1]` through `r[0]`) or a call's result (`id(r)[0]`), and
// for a call whose result carries a reference, each reference handed to a `&`
// parameter the result may alias -- a reference place, an element reached as
// above, or another such call's result (`first(id(r))`). A compare, ternary
// or block value reaches what each of its values does.
func (tc *typeChecker) borrowReferentsThroughReferences(expr ast.ExprID, out []BorrowID) []BorrowID {
	w := referentWalk{tc: tc, out: out}
	w.value(expr)
	return w.out
}

func (tc *typeChecker) markReferentLoan(bid BorrowID) {
	if tc.referentLoans == nil {
		tc.referentLoans = make(map[BorrowID]struct{})
	}
	tc.referentLoans[bid] = struct{}{}
}

type referentWalk struct {
	tc   *typeChecker
	out  []BorrowID // every loan the value stands on
	seen map[symbols.SymbolID]bool
}

func (w *referentWalk) value(expr ast.ExprID) {
	tc := w.tc
	expr = tc.unwrapGroupExpr(expr)
	if !expr.IsValid() || tc.builder == nil {
		return
	}
	if index, ok := tc.builder.Exprs.Index(expr); ok && index != nil {
		w.arg(index.Target)
		return
	}
	call, ok := tc.builder.Exprs.Call(expr)
	if !ok || call == nil {
		for _, value := range tc.choiceValues(expr) {
			w.value(value)
		}
		return
	}
	result := tc.result.ExprTypes[expr]
	if _, carries := tc.carriedReferenceType(result); !carries {
		return
	}
	sym := tc.symbolFromID(tc.symbolForExpr(expr))
	if sym == nil || sym.Signature == nil {
		return
	}
	params := sym.Signature.Params
	offset := 0
	if sym.Signature.HasSelf {
		offset = 1
		if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil && len(params) > 0 &&
			tc.refResultCanAliasParam(result, params[0]) {
			w.arg(member.Target)
		}
	}
	for i, arg := range call.Args {
		if i+offset >= len(params) {
			break
		}
		if tc.refResultCanAliasParam(result, params[i+offset]) || tc.passesCarriedSharedReference(params[i+offset], arg.Value) {
			w.arg(arg.Value)
		}
	}
}

// arg: a reference handed on is either a reference place, which took no loan
// of its own, or a value that reaches one.
func (w *referentWalk) arg(arg ast.ExprID) {
	if _, isPlace := w.tc.resolvePlace(w.tc.unwrapGroupExpr(arg)); isPlace {
		w.place(arg)
		return
	}
	w.value(arg)
}

// place names the shared loan on the referent of a reference place, taking it
// when the place holds none yet (a compare asks once per arm and finds the
// first arm's). A place that is no reference was borrowed when it was handed
// on, and an explicit borrow holds its own loan.
func (w *referentWalk) place(target ast.ExprID) {
	tc := w.tc
	inner := tc.unwrapGroupExpr(target)
	if !inner.IsValid() || tc.isBorrowExpr(inner) || !tc.isReferenceType(tc.result.ExprTypes[inner]) {
		return
	}
	if _, isPlace := tc.resolvePlace(inner); !isPlace {
		return
	}
	bid := tc.borrow.ExprBorrow(target)
	if bid == NoBorrowID {
		bid = tc.borrow.ExprBorrow(inner)
	}
	if bid == NoBorrowID {
		tc.handleBorrow(target, tc.exprSpan(target), ast.ExprUnaryRef, inner)
		bid = tc.borrow.ExprBorrow(target)
		if bid != NoBorrowID {
			tc.markReferentLoan(bid)
		}
	}
	if bid != NoBorrowID && !slices.Contains(w.out, bid) {
		w.out = append(w.out, bid)
	}
	w.sources(inner)
}

// sources: a place spelled through a local reference that stands on no loan
// of this function reaches whatever that reference was given.
func (w *referentWalk) sources(placeExpr ast.ExprID) {
	if desc, ok := w.tc.resolvePlace(w.tc.unwrapGroupExpr(placeExpr)); ok {
		w.sourcesOf(desc.Base)
	}
}

func (w *referentWalk) sourcesOf(base symbols.SymbolID) {
	tc := w.tc
	if !base.IsValid() || w.seen[base] || tc.bindingBorrow[base] != NoBorrowID ||
		!tc.isReferenceType(tc.bindingType(base)) {
		return
	}
	sym := tc.symbolFromID(base)
	if sym == nil || sym.Kind != symbols.SymbolLet {
		return
	}
	if w.seen == nil {
		w.seen = make(map[symbols.SymbolID]bool)
	}
	w.seen[base] = true
	for _, src := range tc.lentValues.sources[base] {
		w.arg(src)
	}
}

// sourceLoansOfHeldLoans: a loan the binding holds on a place spelled through
// a local reference that stands on no loan of this function -- `let e = &mut
// q[0]`, `let it = q.__range()` with `let q = id(r)` -- guards q's place
// only, and a grow or a replace through r frees what it points into. The
// binding also holds a shared loan on each referent q was given, as a shared
// reference binding does (referentLoansForBinding); an exclusive loan is held
// exclusively on each (exclusiveSourceLoans).
func (tc *typeChecker) sourceLoansOfHeldLoans(symID symbols.SymbolID, own BorrowID, span source.Span) []BorrowID {
	w := referentWalk{tc: tc}
	var exclusive []BorrowID
	for _, bid := range append([]BorrowID{own}, tc.viewLoans[symID]...) {
		info := tc.borrow.Info(bid)
		if info == nil {
			continue
		}
		w.out = append(w.out, tc.borrowedPayloadLoans(info.Place.Base)...)
		if info.Kind == BorrowMut {
			exclusive = append(exclusive, tc.exclusiveSourceLoans(info, span)...)
			continue
		}
		w.sourcesOf(info.Place.Base)
	}
	return append(w.out, exclusive...)
}

// holdLoansAsViewLoans: a loan the binding's own reference borrow is not (a
// ternary's two elements, a call whose result may alias two references, what
// a local reference was given) is held as a loan its value depends on, to the
// binding's scope.
func (tc *typeChecker) holdLoansAsViewLoans(symID symbols.SymbolID, loans []BorrowID, own BorrowID) {
	for _, bid := range loans {
		if bid == own {
			continue
		}
		if tc.viewLoans == nil {
			tc.viewLoans = make(map[symbols.SymbolID][]BorrowID)
		}
		if !slices.Contains(tc.viewLoans[symID], bid) {
			tc.viewLoans[symID] = append(tc.viewLoans[symID], bid)
		}
		tc.holdLoanForBinding(symID, bid)
	}
}

// loanNamesUnrootedReferent: the loan was taken on a reference binding's
// referent and is spelled by the binding, which holds no loan of its own to
// say where it points -- `let q = r; let x = q[0];` with r a reference
// parameter. It roots in no storage of this frame.
func (tc *typeChecker) loanNamesUnrootedReferent(bid BorrowID) bool {
	if _, marked := tc.referentLoans[bid]; !marked {
		return false
	}
	info := tc.borrow.Info(bid)
	return info != nil && tc.isReferenceType(tc.bindingType(info.Place.Base))
}

// rebindAllowed checks a store into a reference binding itself, `q = b`. A
// referent loan spelled on q reads what q pointed at, and rebinding q frees
// none of it, so it does not refuse the store. The loan is not ended by the
// store: it cannot tell which of q's values a reader holds (a rebind in one
// branch, or on one turn of a loop, leaves the other), so an exclusive use
// through q stays refused while the reader lives (`q = b; q.push(1)`), a
// conservative refusal. Every other loan of the place still refuses the store.
func (tc *typeChecker) rebindAllowed(place Place) BorrowIssue {
	return tc.borrow.mutationAllowedExcept(place, func(bid BorrowID) bool {
		_, marked := tc.referentLoans[bid]
		info := tc.borrow.Info(bid)
		return marked && info != nil && info.Place == place
	})
}

// mutationAllowedExcept is MutationAllowed with the loans skip names left out.
func (bt *BorrowTable) mutationAllowedExcept(place Place, skip func(BorrowID) bool) BorrowIssue {
	if bt == nil || !place.IsValid() {
		return BorrowIssue{}
	}
	state := bt.combinedState(place)
	for _, bid := range state.shared {
		if !skip(bid) {
			return BorrowIssue{Kind: BorrowIssueFrozen, Borrow: bid}
		}
	}
	if state.mut != NoBorrowID {
		return BorrowIssue{Kind: BorrowIssueTaken, Borrow: state.mut}
	}
	return BorrowIssue{}
}
