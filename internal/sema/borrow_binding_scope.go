package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Lifetimes are lexical (LANGUAGE.md §Borrowing rules): a loan lives to the end
// of the scope it belongs to. A borrow is registered at the scope it is TAKEN
// in, which is also the binding's scope for `let r = &s;` -- so the two agree
// and the loan lives exactly as long as `r` can be read.
//
// They stop agreeing when the loan is stored into a binding declared further
// out: `let mut r = &a; { r = &s; }` takes `&s` in the inner block, and that
// block's end expired the loan while `r` -- still readable -- pointed at `s`.
// `s` could then be moved or mutated and `r` read afterwards (a native
// use-after-free). The same holds for every block form (if/else body, loop
// body) and for a loan reached through an inner binding (`{ let q = &s; r = q; }`).
//
// holdLoanForBinding keeps the lexical model and only corrects which scope the
// loan belongs to: the loan moves to the binding's scope when that scope
// outlives the one the loan is registered at. No lifetime shorter than today's
// is introduced anywhere.
func (tc *typeChecker) holdLoanForBinding(symID symbols.SymbolID, bid BorrowID) {
	if tc.borrow == nil || bid == NoBorrowID || !symID.IsValid() {
		return
	}
	info := tc.borrow.Info(bid)
	if info == nil {
		return
	}
	loanAt := tc.scopeStackIndex(info.Life.ToScope)
	if loanAt < 0 {
		return
	}
	target := tc.bindingHoldScope(symID)
	if !target.IsValid() || tc.scopeStackIndex(target) >= loanAt {
		return
	}
	tc.borrow.Rehome(bid, target)
}

// bindingHoldScope names the live scope a binding's loans must last to: the
// nearest scope on the checker's stack that encloses the binding's declaring
// scope. The resolver opens a scope for every block expression and compare
// arm, but the checker pushes only statement blocks, so a binding declared in
// an arm or block expression has a declaring scope the stack never holds; the
// loan then lasts to the pushed block that contains it -- which is where such
// a binding's lexical life ends as far as the borrow table can tell. Only a
// binding with no enclosing scope on the stack at all (none is known) holds
// to the outermost live scope.
func (tc *typeChecker) bindingHoldScope(symID symbols.SymbolID) symbols.ScopeID {
	if len(tc.scopeStack) == 0 {
		return symbols.NoScopeID
	}
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return tc.scopeStack[0]
	}
	scope := sym.Scope
	seen := make(map[symbols.ScopeID]struct{})
	for scope.IsValid() {
		if tc.scopeStackIndex(scope) >= 0 {
			return scope
		}
		if _, looped := seen[scope]; looped || tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Scopes == nil {
			break
		}
		seen[scope] = struct{}{}
		data := tc.symbols.Table.Scopes.Get(scope)
		if data == nil {
			break
		}
		scope = data.Parent
	}
	return tc.scopeStack[0]
}

func (tc *typeChecker) scopeStackIndex(scope symbols.ScopeID) int {
	if !scope.IsValid() {
		return -1
	}
	for i := len(tc.scopeStack) - 1; i >= 0; i-- {
		if tc.scopeStack[i] == scope {
			return i
		}
	}
	return -1
}

// loanOutlivesScopeStack reports whether a loan belongs to a scope that is
// still open. A binding released at its own scope's end must not drop such a
// loan: the loan is not the binding's alone -- an outer binding holds it (it
// was rehomed there, or the inner binding copied an outer reference as in
// `let q = &s; { let r = q; }`), and dropping it would end `q`'s loan while
// `q` is still readable.
func (tc *typeChecker) loanOutlivesScopeStack(bid BorrowID) bool {
	if tc.borrow == nil {
		return false
	}
	info := tc.borrow.Info(bid)
	return info != nil && tc.scopeStackIndex(info.Life.ToScope) >= 0
}

// holdViewLoansForBinding is holdLoanForBinding for a value that carries a
// core BytesView -- the view itself, or an aggregate holding one (an array of
// views, `Option<BytesView>`, a struct with a view field). A view is not a
// reference type, so its binding carries no bindingBorrow; the loan it depends
// on is the `&string` borrow its producer keeps past the call
// (dropImplicitBorrowForRefParam) -- registered, like any borrow, at the scope
// the call is made in. `let mut v = a.bytes(); { v = s.bytes(); }` then expired
// the loan on `s` at the inner block's end while `v` still viewed it, and so
// did `{ vs[0] = s.bytes(); }` and `{ o = Some(s.bytes()); }`.
//
// symID is the ROOT binding of the written place: handleAssignment passes the
// base of `vs[0]` or `h.v`, so a store into part of a binding is held for as
// long as the binding. The loans a binding holds are remembered (accumulated:
// a store into one element leaves the others' loans in place) so a view read
// back out of it (`v = w`, `v = vs[0]`) carries them too.
func (tc *typeChecker) holdViewLoansForBinding(symID symbols.SymbolID, expr ast.ExprID) {
	if tc.borrow == nil || tc.types == nil || tc.result == nil || !symID.IsValid() || !expr.IsValid() {
		return
	}
	if !tc.mayCarryView(tc.result.ExprTypes[expr]) && !tc.mayCarryView(tc.bindingType(symID)) {
		return
	}
	loans := tc.viewLoansOfExpr(expr)
	if len(loans) == 0 {
		return
	}
	if tc.viewLoans == nil {
		tc.viewLoans = make(map[symbols.SymbolID][]BorrowID)
	}
	for _, bid := range loans {
		if !slices.Contains(tc.viewLoans[symID], bid) {
			tc.viewLoans[symID] = append(tc.viewLoans[symID], bid)
		}
		tc.holdLoanForBinding(symID, bid)
	}
}

// mayCarryView: the type is not provably free of borrowed content. The shape
// query is the return-origin one, so a view, a reference, and any aggregate or
// generic whose shape is unknown all answer yes.
func (tc *typeChecker) mayCarryView(t types.TypeID) bool {
	return t != types.NoTypeID && returnOriginTypeShape(tc.types, t, nil) != returnOriginRefFree
}

// viewLoansOfExpr names the loans a view-carrying value depends on: the string
// borrow a borrow-carrying call keeps (receiver or argument), the loans of the
// binding a place expression reads, and those of every operand of a call,
// constructor or literal that builds the value. Anything else yields none,
// exactly as the borrow table tracks it today.
func (tc *typeChecker) viewLoansOfExpr(expr ast.ExprID) []BorrowID {
	expr = tc.unwrapGroupExpr(expr)
	if !expr.IsValid() || tc.builder == nil {
		return nil
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return nil
	}
	var operands []ast.ExprID
	switch node.Kind {
	case ast.ExprIdent:
		return append([]BorrowID(nil), tc.viewLoans[tc.symbolForExpr(expr)]...)
	case ast.ExprIndex:
		if data, ok := tc.builder.Exprs.Index(expr); ok && data != nil {
			operands = append(operands, data.Target)
		}
	case ast.ExprMember:
		if data, ok := tc.builder.Exprs.Member(expr); ok && data != nil {
			operands = append(operands, data.Target)
		}
	case ast.ExprTupleIndex:
		if data, ok := tc.builder.Exprs.TupleIndex(expr); ok && data != nil {
			operands = append(operands, data.Target)
		}
	case ast.ExprUnary:
		if data, ok := tc.builder.Exprs.Unary(expr); ok && data != nil &&
			(data.Op == ast.ExprUnaryDeref || data.Op == ast.ExprUnaryOwn) {
			operands = append(operands, data.Operand)
		}
	case ast.ExprArray:
		if data, ok := tc.builder.Exprs.Array(expr); ok && data != nil {
			operands = append(operands, data.Elements...)
		}
	case ast.ExprTuple:
		if data, ok := tc.builder.Exprs.Tuple(expr); ok && data != nil {
			operands = append(operands, data.Elements...)
		}
	case ast.ExprStruct:
		if data, ok := tc.builder.Exprs.Struct(expr); ok && data != nil {
			for _, field := range data.Fields {
				operands = append(operands, field.Value)
			}
		}
	case ast.ExprCall:
		return tc.viewLoansOfCall(expr)
	case ast.ExprCompare, ast.ExprTernary, ast.ExprBlock:
		return tc.choiceValueLoans(expr)
	}
	var loans []BorrowID
	for _, operand := range operands {
		loans = append(loans, tc.viewLoansOfExpr(operand)...)
	}
	return loans
}

// viewLoansOfCall: a call whose result carries a borrow keeps the borrows of
// its reference operands past the call (dropImplicitBorrowForRefParam), and
// the result depends on them; any call (a tag constructor such as `Some(...)`,
// a user function) may also pass a view operand through to its result, so the
// loans of its operands are carried.
func (tc *typeChecker) viewLoansOfCall(expr ast.ExprID) []BorrowID {
	call, ok := tc.builder.Exprs.Call(expr)
	if !ok || call == nil {
		return nil
	}
	operands := make([]ast.ExprID, 0, len(call.Args)+1)
	if member, isMember := tc.builder.Exprs.Member(call.Target); isMember && member != nil {
		operands = append(operands, member.Target)
	}
	for _, arg := range call.Args {
		operands = append(operands, arg.Value)
	}
	var loans []BorrowID
	producer := returnOriginTypeShape(tc.types, tc.result.ExprTypes[expr], nil) == returnOriginCarriesRef
	for _, operand := range operands {
		if producer {
			bid := tc.borrow.ExprBorrow(tc.unwrapGroupExpr(operand))
			if bid == NoBorrowID {
				bid = tc.inheritedBorrowForExpr(operand)
			}
			if bid != NoBorrowID {
				loans = append(loans, bid)
			}
		}
		loans = append(loans, tc.viewLoansOfExpr(operand)...)
	}
	return loans
}
