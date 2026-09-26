package sema

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
)

// A rehomed loan ends with its new scope, not the one it was taken in.
func TestBorrowRehomeMovesTheLoanToTheHoldingScope(t *testing.T) {
	bt := NewBorrowTable()
	outer, inner := symbols.ScopeID(1), symbols.ScopeID(2)
	place := bt.CanonicalPlace(symbols.SymbolID(7), nil)
	bid, issue := bt.BeginBorrow(ast.ExprID(3), source.Span{}, BorrowShared, place, inner, NoBorrowID)
	if issue.Kind != BorrowIssueNone || bid == NoBorrowID {
		t.Fatalf("PRECONDITION: borrow not admitted: %+v", issue)
	}
	bt.Rehome(bid, outer)
	bt.EndScope(inner)
	if bt.MoveAllowed(place).Kind == BorrowIssueNone {
		t.Fatal("the loan expired with the scope it was taken in, though it was rehomed outward")
	}
	bt.EndScope(outer)
	if got := bt.MoveAllowed(place); got.Kind != BorrowIssueNone {
		t.Fatalf("the loan outlived its holding scope: %+v", got)
	}
}

// Control for the guard in Rehome: a loan already dropped is not revived.
func TestBorrowRehomeLeavesADroppedLoanDead(t *testing.T) {
	bt := NewBorrowTable()
	outer, inner := symbols.ScopeID(1), symbols.ScopeID(2)
	place := bt.CanonicalPlace(symbols.SymbolID(7), nil)
	bid, _ := bt.BeginBorrow(ast.ExprID(3), source.Span{}, BorrowShared, place, inner, NoBorrowID)
	bt.DropBorrow(bid)
	bt.Rehome(bid, outer)
	if ids := bt.ScopeBorrows(outer); len(ids) != 0 {
		t.Fatalf("a dropped loan was registered at the outer scope: %v", ids)
	}
	if got := bt.MoveAllowed(place); got.Kind != BorrowIssueNone {
		t.Fatalf("a dropped loan still blocks the move: %+v", got)
	}
}
