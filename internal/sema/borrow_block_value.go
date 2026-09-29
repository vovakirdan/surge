package sema

import (
	"slices"

	"surge/internal/ast"
)

// A block expression's value leaves by `ret`, and the loans that value depends
// on must last as long as the statement around the block expression, where a
// binding can hold them. Two ends came first:
//
//   - a statement-temporary loan -- the entry loan of `{ ret m.get_mut(&k); }`
//     taken through a `&mut Map` reference -- was released at the end of the
//     `ret` statement itself;
//   - a loan registered at a scope inside the block -- `{ if c { ret
//     mm.get_mut(&k); } ret nothing; }`, the receiver's own borrow of a local
//     map, or the entry loan through a reference -- expired with the `if`,
//     `for` or `while` body the `ret` sits in.
//
// Either way `let o = { .. }; insm(m);` grew the map under the entry o still
// wrote (valgrind: invalid read and write). A bare `&mut` value never had the
// gap: its loans are taken where the block's value is bound, by walking the
// block to each `ret` (projectedMutReborrow).

// handRetLoansOutward moves the `ret` statement's temporary loans to the
// statement enclosing the block expression that `ret` leaves, and the loans
// its value depends on to the scope that block expression is typed in, when
// the value can hold a loan at all.
func (tc *typeChecker) handRetLoansOutward(value ast.ExprID) {
	ctx := tc.currentBlockReturnContext()
	if ctx == nil || ctx.kind != returnCtxBlockExpr {
		return
	}
	// A value that can hold no loan -- a copy `ret *t`, a scalar a compare
	// computes from an arm binding -- carries none out of the block: its
	// loans end where they were taken.
	if tc.result == nil || !tc.mayHoldStorageLoan(tc.result.ExprTypes[value]) {
		return
	}
	var top []BorrowID
	if ctx.tempFrames > 0 && ctx.tempFrames < len(tc.tempFrames) {
		frame := &tc.tempFrames[len(tc.tempFrames)-1]
		outer := &tc.tempFrames[ctx.tempFrames-1]
		top = frame.loans
		for _, bid := range top {
			if !slices.Contains(outer.loans, bid) {
				outer.loans = append(outer.loans, bid)
			}
		}
		frame.loans = nil
	}
	target := tc.scopeStackIndex(ctx.scope)
	if target < 0 || tc.borrow == nil {
		return
	}
	for _, bid := range append(top, tc.valueLoans(value)...) {
		info := tc.borrow.Info(bid)
		if info != nil && tc.scopeStackIndex(info.Life.ToScope) > target {
			tc.borrow.Rehome(bid, ctx.scope)
		}
	}
}

// leaveBlockByRet answers a `ret` statement once its value is typed: its loans
// leave the block (handRetLoansOutward) and the exit is recorded.
func (tc *typeChecker) leaveBlockByRet(id ast.StmtID, value ast.ExprID) {
	tc.handRetLoansOutward(value)
	tc.recordRetExit(id)
}
