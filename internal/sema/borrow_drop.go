package sema

import (
	"fmt"
	"slices"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// handleDrop performs `@drop x`: the binding ends here instead of at the end of
// its block (LANGUAGE.md §Lexical Lifetimes and Early Drop).
//
// A drop that releases storage is asked of the borrow table first, exactly as
// a move is: `let r = &s; @drop s; r.__len()` frees `s` under a live
// reference, so a dropped place that is still borrowed is refused (SEM3020) --
// the loan's holder must be dropped, or go out of scope, first.
//
// A reference binding's drop releases ITS borrow, and only its own: a loan
// copied into another binding (`let r = q;`, `r = q;`, `ident(q)`, a view
// built from it) is that binding's too, and `@drop q` must not end it while
// `r` can still be read. The loan is released when the last binding that
// holds it is dropped, and otherwise at its lexical end as before.
func (tc *typeChecker) handleDrop(expr ast.ExprID, span source.Span) {
	exprType := tc.typeExpr(expr)
	symID := tc.symbolForExpr(expr)
	if !symID.IsValid() {
		if tc.handleProjectionDrop(expr, exprType, span) {
			return
		}
		tc.report(diag.SemaBorrowNonAddressable, span, "drop target must be a binding")
		return
	}
	if tc.bindingBorrow == nil {
		return
	}
	// Only a drop that releases storage can free something under a borrow; a
	// value that owns no heap is not consumed by it (below), and a reference
	// binding gives up its own loan instead.
	consumes := exprType != types.NoTypeID && tc.ownsHeap(exprType)
	if consumes && tc.refuseDropOfBorrowedPlace(tc.canonicalPlace(placeDescriptor{Base: symID}), span) {
		return
	}
	// Dropping a value that owns heap storage consumes it on both paths below:
	// later uses, a second drop and scope exit all see it as moved.
	//
	// The question is OwnsHeap and not Copy, and the two stopped coinciding
	// for the arbitrary-precision scalars (see ownership_axes.go): a `float`
	// is duplicable, so it is Copy, yet it owns a heap block, so MIR emits a
	// drop for it. Asking about Copy here left an explicitly dropped `float`
	// unconsumed, so scope exit still counted it live and emitted a SECOND
	// InstrDrop on the same local — which the VM met with VM3301 at run time.
	// Asking about the axis MIR actually gates on makes the two agree, and
	// makes reading a dropped scalar a use-after-move diagnostic instead of a
	// runtime panic.
	if consumes && !tc.refuseDropUnderTaskPin(symID, span) {
		tc.markBindingMoved(symID, span)
	}
	bid := tc.bindingBorrow[symID]
	if bid == NoBorrowID {
		// A reference that holds no loan of its own (a compare arm's pattern
		// binding, whose loans are its scrutinee's) still ends here.
		if exprType != types.NoTypeID && tc.isReferenceType(exprType) {
			tc.endDroppedReference(symID, span)
		}
		tc.recordBorrowEvent(&BorrowEvent{
			Kind:    BorrowEvDrop,
			Binding: symID,
			Span:    span,
			Scope:   tc.currentScope(),
			Note:    "drop",
		})
		return
	}
	// The binding gives its loan up, so it ends with it: reading `q` after
	// `@drop q; sink(own s);` would read the freed `s`, and so would the next
	// turn of a loop the drop sits in (loanDroppedOutsideLoop).
	tc.endDroppedReference(symID, span)
	var place Place
	if tc.borrow != nil {
		if info := tc.borrow.Info(bid); info != nil {
			place = info.Place
		}
	}
	tc.recordBorrowEvent(&BorrowEvent{
		Kind:    BorrowEvDrop,
		Borrow:  bid,
		Place:   place,
		Binding: symID,
		Span:    span,
		Scope:   tc.currentScope(),
	})
	tc.bindingBorrow[symID] = NoBorrowID
	if tc.loanHeldByAnotherBinding(symID, bid, span) {
		return
	}
	if tc.borrow != nil {
		tc.borrow.DropBorrow(bid)
	}
	tc.recordBorrowEvent(&BorrowEvent{
		Kind:    BorrowEvBorrowEnd,
		Borrow:  bid,
		Place:   place,
		Binding: symID,
		Span:    span,
		Scope:   tc.currentScope(),
		Note:    "drop",
	})
}

// endDroppedReference: the dropped reference binding is dead from here, and a
// loop it was declared outside of re-enters with it dead
// (loanDroppedOutsideLoop).
func (tc *typeChecker) endDroppedReference(symID symbols.SymbolID, span source.Span) {
	tc.markBindingMoved(symID, span)
	if tc.loanDropped == nil {
		tc.loanDropped = make(map[symbols.SymbolID]int)
	}
	tc.loanDropped[symID] = tc.scopeStackIndex(tc.bindingHoldScope(symID))
}

// refuseDropOfBorrowedPlace: a drop gives the place away as a move does, so
// the borrow table is asked the same question (MoveAllowed) and the refusal is
// the move's code, SEM3020, worded for a drop.
func (tc *typeChecker) refuseDropOfBorrowedPlace(place Place, span source.Span) bool {
	if tc.borrow == nil || !place.IsValid() {
		return false
	}
	issue := tc.borrow.MoveAllowed(place)
	if issue.Kind == BorrowIssueNone {
		return false
	}
	label := tc.placeLabel(place)
	var msg string
	switch issue.Kind {
	case BorrowIssueFrozen:
		msg = fmt.Sprintf("cannot drop %s while it is shared-borrowed", label)
	case BorrowIssueTaken:
		msg = fmt.Sprintf("cannot drop %s while an exclusive borrow is active", label)
	default:
		msg = fmt.Sprintf("cannot drop %s due to an active borrow", label)
	}
	tc.emitBorrowDiag(diag.SemaBorrowMove, span, msg, issue.Borrow, label)
	return true
}

// loanHeldByAnotherBinding reports whether a binding other than symID, still
// in scope at the drop, holds the loan: as its own reference borrow, or as a
// loan the value it holds depends on (a view, or an aggregate carrying a
// reference or a view). A binding whose scope cannot be told counts as
// holding it: the loan then lives to its lexical end, which is never shorter
// than before.
func (tc *typeChecker) loanHeldByAnotherBinding(symID symbols.SymbolID, bid BorrowID, at source.Span) bool {
	for other, held := range tc.bindingBorrow {
		if other != symID && held == bid && tc.bindingInScopeAt(other, at) {
			return true
		}
	}
	for other, loans := range tc.viewLoans {
		if other != symID && slices.Contains(loans, bid) && tc.bindingInScopeAt(other, at) {
			return true
		}
	}
	return false
}

// bindingInScopeAt: the binding's declaring scope encloses the point. A
// binding of a compare arm or block expression that has already closed is
// not; one whose scope is unknown is assumed to be.
func (tc *typeChecker) bindingInScopeAt(symID symbols.SymbolID, at source.Span) bool {
	// An arm's scope spans only its pattern, so its bindings are told live by
	// the arm being typed rather than by the span.
	if tc.openArmBindings[symID] > 0 {
		return true
	}
	sym := tc.symbolFromID(symID)
	if sym == nil || tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Scopes == nil {
		return true
	}
	data := tc.symbols.Table.Scopes.Get(sym.Scope)
	if data == nil || data.Span.Empty() || at.Empty() || data.Span.File != at.File {
		return true
	}
	return data.Span.Start <= at.Start && at.End <= data.Span.End
}

// handleProjectionDrop performs `@drop o.inner` — releasing one place and
// leaving the rest of the binding readable. Reports whether the target was a
// projection, so the caller can fall through to its own error for a target that
// is no place at all.
//
// An explicit drop of a place IS a move: the value goes somewhere it can never
// come back from. So it records exactly what a move records, and everything
// downstream follows from that — reading `o.inner` afterwards is a
// use-after-move, reading `o` whole is too, the sibling is untouched, and the
// binding's own scope-exit drop reclaims the remainder rather than the whole.
// Like a move, and with the whole-binding drop's gate, a place that owns heap
// is refused while it is borrowed; one that owns none frees nothing under the
// borrow, and a read through it is refused as a read of a moved place.
//
// No `own` marker is wanted here, unlike a move into a binding: `@drop` already
// says the value is being disposed of, and the read cannot be mistaken for a
// borrow or a copy.
func (tc *typeChecker) handleProjectionDrop(expr ast.ExprID, exprType types.TypeID, span source.Span) bool {
	desc, ok := tc.resolvePlace(expr)
	if !ok || !desc.Base.IsValid() || len(desc.Segments) == 0 {
		return false
	}
	if tc.rejectUnnameableResidual(desc, expr, span) {
		return true
	}
	// Reading it is checked before it is emptied: `@drop o.inner` twice is a
	// use-after-move on the second, and so is dropping a field of a value that
	// has already gone whole.
	tc.checkPlaceUseAfterMove(expr, span)
	expanded, _ := tc.expandPlaceDescriptor(desc)
	place := tc.canonicalPlace(expanded)
	if !place.IsValid() {
		return true
	}
	if exprType != types.NoTypeID && tc.ownsHeap(exprType) && tc.refuseDropOfBorrowedPlace(place, span) {
		return true
	}
	tc.recordBorrowEvent(&BorrowEvent{
		Kind:  BorrowEvDrop,
		Place: place,
		Span:  span,
		Scope: tc.currentScope(),
		Note:  "drop",
	})
	tc.markPlaceMoved(place, span)
	return true
}

// loanDroppedOutsideLoop: the binding an `@drop` ended with its loan was
// declared outside the innermost loop being closed, so the loop's next turn
// would read it dead. Where it was declared is taken at the drop, while the
// body's scopes are still on the stack; a binding whose scope could not be
// told (-1) counts as declared outside.
func (tc *typeChecker) loanDroppedOutsideLoop(symID symbols.SymbolID) bool {
	at, dropped := tc.loanDropped[symID]
	if !dropped || len(tc.loopScopeFloors) == 0 {
		return false
	}
	return at < tc.loopScopeFloors[len(tc.loopScopeFloors)-1]
}

// openArm marks a compare arm's pattern bindings live while the arm is typed
// (open) and ends that when it is done.
func (tc *typeChecker) openArm(bindings []symbols.SymbolID, open bool) {
	if len(bindings) == 0 {
		return
	}
	if tc.openArmBindings == nil {
		tc.openArmBindings = make(map[symbols.SymbolID]int)
	}
	for _, sym := range bindings {
		if open {
			tc.openArmBindings[sym]++
		} else if tc.openArmBindings[sym] > 0 {
			tc.openArmBindings[sym]--
		}
	}
}

// holdScrutineeLoansForArmBindings: a compare arm's pattern binding copies
// what the scrutinee carries (`compare q { x => ... }` binds x to q's
// reference), so it holds the scrutinee's loans for as long as the arm runs.
// Recorded like a view's loans, so `@drop q` inside the arm cannot release a
// loan `x` still reads through.
func (tc *typeChecker) holdScrutineeLoansForArmBindings(bindings []symbols.SymbolID, scrutinee ast.ExprID) {
	if tc.borrow == nil || len(bindings) == 0 || !scrutinee.IsValid() {
		return
	}
	var loans []BorrowID
	if bid := tc.borrow.ExprBorrow(tc.unwrapGroupExpr(scrutinee)); bid != NoBorrowID {
		loans = append(loans, bid)
	}
	if bid := tc.inheritedBorrowForExpr(scrutinee); bid != NoBorrowID {
		loans = append(loans, bid)
	}
	loans = append(loans, tc.viewLoansOfExpr(scrutinee)...)
	if len(loans) == 0 {
		return
	}
	if tc.viewLoans == nil {
		tc.viewLoans = make(map[symbols.SymbolID][]BorrowID)
	}
	for _, sym := range bindings {
		if !tc.mayCarryView(tc.bindingType(sym)) {
			continue
		}
		for _, bid := range loans {
			if !slices.Contains(tc.viewLoans[sym], bid) {
				tc.viewLoans[sym] = append(tc.viewLoans[sym], bid)
			}
		}
	}
}
