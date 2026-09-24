package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// What a spawned task holds through a value it was lent, where the loan walk names no loan
// (RV2-DEBT-365, R-b(spawn) and R-d's spawn form).
//
// `spawn f(x)` starts f's task at once, and that task holds what its operand's reaching positions
// carry exactly as the task a plain call answers does (task_lent_value.go): a reference that
// reaches it through a parameter, a `let` whose value carries one, or an array a parameter may
// have handed in as a window. In `fn park(x: &int, ch: Channel<Task<int>>) { let t = spawn
// worker(x); ch.send(own t); }` the task holds what park's caller lent, and the caller is shown no
// Task. The call that is a spawn's operand is left to the spawn (typeTaskProducingCall), so what
// the call collects for itself is collected here for the spawn: a loan found by opening a `let`
// joins the spawn's own captures, and the flow-only pins wait for the spawn's task id
// (typeSpawnExpr). A callee recorded to hand back a task built from owned values clears the
// flow-only pins, as it clears them for a plain call. What a flow-only pin is, and what it is not
// -- frame storage for SEM3139, a capture for the callee record, a promoted place -- is
// task_lent_value.go's.

// noteSpawnLentValues collects, for the call a spawn is typing as its operand, what the call's
// reaching positions carry that the loan walk named no loan for.
func (tc *typeChecker) noteSpawnLentValues(id ast.ExprID, ty types.TypeID, call *ast.ExprCallData) {
	lent := tc.lentValueCaptures(ty, call)
	if tc.calleeTaskBorrowsNothing(id) {
		lent = nil
	}
	lent = append(lent, tc.windowParameterCaptures(call)...)
	tc.lentValues.spawnLent = append(tc.lentValues.spawnLent, lent...)
}

// beginSpawnLent starts one spawn's collection and hands back the enclosing spawn's, which the
// spawn puts back when it is done: a spawn nested in this operand collects for itself.
func (tc *typeChecker) beginSpawnLent() []spawnBorrowCapture {
	outer := tc.lentValues.spawnLent
	tc.lentValues.spawnLent = nil
	return outer
}
