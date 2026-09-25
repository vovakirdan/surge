package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
)

// walkCrossingBody walks the body of an `on dst { ... }` or `spawn on dst { ... }` crossing as
// the frame it is.
//
// The body runs as an activation of its own, on the copies its captures made when they
// entered its state, and `ret` is its only exit (RUNTIME_V2 "The crossing constructs"). Those
// copies, and whatever the body declared, are freed when it ends. So the body is a frame for
// task borrows exactly as an `async { }` body is (taskBlockPayload):
//   - a task the body starts over one of those places must be joined before the body leaves;
//   - it is judged at the body's `ret` (recordRetExit) and at its end, not at the caller's exits.
//
// Judged at the caller's exits it was never judged at all in two cases. A capture's pin is
// filed under the caller's binding of the same name, a different place. And a caller that
// ends in `panic`, holding a parameter, has no exit that asks: the parameter is not a `let`,
// so no block end refuses it, and an unreachable function end is not an edge.
//
// The caller's pins are put back afterwards, so a join written inside the body cannot release
// a pin of the caller's on a path the caller never takes.
//
// The body's exits stop at this boundary rather than at the enclosing function, and the body
// owns what moved into it: the caller's binding is marked moved by checkOnCaptures, so nobody
// else will release it.
func (tc *typeChecker) walkCrossingBody(body ast.StmtID, anchor symbols.SymbolID, label string) {
	pinsOutsideBody := tc.snapshotTaskBorrowPins()
	tc.returnStack[len(tc.returnStack)-1].entryPins = pinsOutsideBody
	tc.pushDropScope(true)
	tc.registerCrossingBodyOwnership(body, anchor)
	tc.walkStmt(body)
	tc.popDropScope()
	if tc.returnStatus(body) != returnClosed {
		tc.refuseLivePinsAtExit(ast.NoExprID, pinsOutsideBody, label+" end")
	}
	tc.restoreTaskBorrowPins(pinsOutsideBody)
}
