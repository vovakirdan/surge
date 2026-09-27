package sema

import (
	"surge/internal/ast"
)

// A compare, ternary or block expression used as a VALUE hands out whatever
// its chosen arm yields, so the binding it initializes holds the loans of
// every arm's value: `let r = compare q { x => x; }` holds q's loan through x
// (an arm binding holds its scrutinee's, holdScrutineeLoansForArmBindings),
// and `let r = compare i { 0 => q; _ => &a; }` holds both. Without this the
// result held nothing, and `@drop q; sink(own s); r.__len()` read freed
// memory natively.

// choiceValueLoans names the loans of every value a choice expression can
// yield.
func (tc *typeChecker) choiceValueLoans(expr ast.ExprID) []BorrowID {
	values := tc.choiceValues(expr)
	loans := make([]BorrowID, 0, len(values))
	for _, value := range values {
		loans = append(loans, tc.valueLoans(value)...)
	}
	return loans
}

// choiceValues names every value a compare, ternary or block expression can
// yield; any other expression yields none.
func (tc *typeChecker) choiceValues(expr ast.ExprID) []ast.ExprID {
	var values []ast.ExprID
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return nil
	}
	switch node.Kind {
	case ast.ExprCompare:
		if data, ok := tc.builder.Exprs.Compare(expr); ok && data != nil {
			for _, arm := range data.Arms {
				values = append(values, arm.Result)
			}
		}
	case ast.ExprTernary:
		if data, ok := tc.builder.Exprs.Ternary(expr); ok && data != nil {
			values = append(values, data.TrueExpr, data.FalseExpr)
		}
	case ast.ExprBlock:
		if data, ok := tc.builder.Exprs.Block(expr); ok && data != nil {
			tc.blockValueExprs(data.Stmts, true, &values)
		}
	}
	return values
}

// valueLoans: the loans a value depends on -- a borrow it takes, the loan a
// reference it copies stands on, and the loans a view or aggregate carries.
func (tc *typeChecker) valueLoans(expr ast.ExprID) []BorrowID {
	if !expr.IsValid() {
		return nil
	}
	var loans []BorrowID
	if bid := tc.borrow.ExprBorrow(tc.unwrapGroupExpr(expr)); bid != NoBorrowID {
		loans = append(loans, bid)
	}
	if bid := tc.inheritedBorrowForExpr(expr); bid != NoBorrowID {
		loans = append(loans, bid)
	}
	return append(loans, tc.viewLoansOfExpr(expr)...)
}

// blockValueExprs collects the values a block expression can leave with: each
// `ret` in its statements (not inside a nested expression, whose `ret` is its
// own) and a legacy value tail.
func (tc *typeChecker) blockValueExprs(stmts []ast.StmtID, outermost bool, out *[]ast.ExprID) {
	for i, id := range stmts {
		stmt := tc.builder.Stmts.Get(id)
		if stmt == nil {
			continue
		}
		switch stmt.Kind {
		case ast.StmtRet:
			if ret := tc.builder.Stmts.Ret(id); ret != nil {
				*out = append(*out, ret.Expr)
			}
		case ast.StmtExpr:
			if data := tc.builder.Stmts.Expr(id); data != nil && outermost && i == len(stmts)-1 {
				*out = append(*out, data.Expr)
			}
		case ast.StmtBlock:
			if data := tc.builder.Stmts.Block(id); data != nil {
				tc.blockValueExprs(data.Stmts, false, out)
			}
		case ast.StmtIf:
			if data := tc.builder.Stmts.If(id); data != nil {
				tc.blockValueExprs([]ast.StmtID{data.Then, data.Else}, false, out)
			}
		case ast.StmtWhile:
			if data := tc.builder.Stmts.While(id); data != nil {
				tc.blockValueExprs([]ast.StmtID{data.Body}, false, out)
			}
		case ast.StmtForClassic:
			if data := tc.builder.Stmts.ForClassic(id); data != nil {
				tc.blockValueExprs([]ast.StmtID{data.Body}, false, out)
			}
		case ast.StmtForIn:
			if data := tc.builder.Stmts.ForIn(id); data != nil {
				tc.blockValueExprs([]ast.StmtID{data.Body}, false, out)
			}
		}
	}
}
