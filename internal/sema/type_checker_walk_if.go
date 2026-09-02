package sema

// The if-statement's FLOW JOIN, which is the one place in the walker where two
// per-path lattices have to be reconciled by hand. It lives beside the walk
// rather than in it because it is a rule and not a dispatch: which predecessor
// survives, and what the arms owe each other when only one of them gave a value
// away.
//
// The two lattices travel together as a flowSnapshot -- moved places and borrow
// pins -- so that no case here can remember one and forget the other. See
// task_borrow_pin.go for why they share a value.

import (
	"surge/internal/ast"
	"surge/internal/symbols"
)

func (tc *typeChecker) walkIfStmt(id ast.StmtID) {
	ifStmt := tc.builder.Stmts.If(id)
	if ifStmt == nil {
		return
	}
	tc.ensureBoolContext(ifStmt.Cond, tc.exprSpan(ifStmt.Cond))
	before := tc.snapshotFlow()
	tc.walkStmt(ifStmt.Then)
	thenFlow := tc.snapshotFlow()
	thenClosed := tc.returnStatus(ifStmt.Then) == returnClosed
	if ifStmt.Else.IsValid() {
		tc.walkIfElseArm(ifStmt, before, thenFlow, thenClosed)
		return
	}
	if thenClosed {
		tc.restoreFlow(before)
		return
	}
	// The arm-less path is a reachable predecessor, so a join written only in
	// the then-arm releases nothing after the `if`, and a value it moved is
	// maybe-moved.
	joined := mergeFlow(thenFlow, before)
	if drops, plans := tc.oneSidedObligations(before.moved, joined.moved); len(drops) > 0 {
		if tc.result.IfSyntheticElseDrops == nil {
			tc.result.IfSyntheticElseDrops = make(map[ast.StmtID][]symbols.SymbolID)
		}
		tc.result.IfSyntheticElseDrops[id] = drops
		tc.recordOneSidedDrops(DropSite{Stmt: id}, plans)
	}
	tc.restoreFlow(joined)
}

// walkIfElseArm walks the else arm and picks the state that survives the join.
// An arm that RETURNS is not a predecessor of what follows, so the other arm's
// state passes through unchanged; when both return, nothing after the `if` is
// reachable and the pre-branch state stands.
func (tc *typeChecker) walkIfElseArm(
	ifStmt *ast.IfStmt,
	before, thenFlow flowSnapshot,
	thenClosed bool,
) {
	tc.restoreFlow(before)
	tc.walkStmt(ifStmt.Else)
	elseFlow := tc.snapshotFlow()
	elseClosed := tc.returnStatus(ifStmt.Else) == returnClosed
	switch {
	case thenClosed && elseClosed:
		tc.restoreFlow(before)
	case thenClosed:
		tc.restoreFlow(elseFlow)
	case elseClosed:
		tc.restoreFlow(thenFlow)
	default:
		joined := mergeFlow(thenFlow, elseFlow)
		tc.recordIfArmDrops(ifStmt.Then, thenFlow.moved, joined.moved)
		tc.recordIfArmDrops(ifStmt.Else, elseFlow.moved, joined.moved)
		tc.restoreFlow(joined)
	}
}
