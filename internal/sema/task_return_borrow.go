package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
)

// A returned task handle is judged at the exit it actually leaves by, and by
// the spawn of the task it names. A clone has no spawn of its own: it is a
// second handle on its original's running task and holds the borrows that
// task's spawn took.

// taskReturnSpawn finds the spawn whose arguments decide whether the handle at
// exit still borrows this frame. cloneSpan names the clone for the diagnostic.
// released reports that no path to the exit can leave the task running with a
// borrow its spawn captured -- joined or drained on every one of them, which is
// what the pins record -- so the clone may leave.
func (tc *typeChecker) taskReturnSpawn(exit, returned ast.ExprID, kind ast.ExprKind) (spawn ast.ExprID, cloneSpan source.Span, released bool) {
	handle := tc.taskTracker.TaskIDForExpr(exit)
	if handle == 0 && kind == ast.ExprIdent {
		handle = tc.taskTracker.TaskIDForBinding(tc.symbolForExpr(exit))
	}
	root := tc.taskTracker.TaskIdentity(handle)
	origin, ok := tc.taskTracker.GetTask(root)
	if !ok {
		return ast.NoExprID, source.Span{}, false
	}
	if root != handle {
		if !tc.taskMayHoldBorrowAtExit(root, exit, returned) {
			return ast.NoExprID, source.Span{}, true
		}
		if clone, ok := tc.taskTracker.GetTask(handle); ok {
			cloneSpan = clone.Span
		}
	}
	return origin.SpawnExpr, cloneSpan, false
}

// taskMayHoldBorrowAtExit reads the pin state AT the exit being judged. The
// return's own expression was evaluated just now, so the current state is its
// state. A value block's `ret` was left earlier -- possibly from an arm whose
// flow the join has since thrown away -- so it answers with what was recorded
// there. A block result nobody recorded is treated as still pinned: an exit
// whose state is unknown must not let a borrow out.
func (tc *typeChecker) taskMayHoldBorrowAtExit(task uint32, exit, returned ast.ExprID) bool {
	if held, ok := tc.taskTracker.exitPins[exit]; ok {
		return slices.Contains(held, task)
	}
	if exit != tc.unwrapGroupExpr(returned) {
		return true
	}
	for key := range tc.taskBorrowPins {
		if key.Task == task {
			return true
		}
	}
	return false
}

// noteRetExitTaskPins records which tasks may still hold a borrow pin where a
// `ret` leaves its value block, for the return that later hands the value out.
func (tc *typeChecker) noteRetExitTaskPins(expr ast.ExprID) {
	if tc.taskTracker == nil || !expr.IsValid() {
		return
	}
	held := make([]uint32, 0, len(tc.taskBorrowPins))
	for key := range tc.taskBorrowPins {
		if !slices.Contains(held, key.Task) {
			held = append(held, key.Task)
		}
	}
	if tc.taskTracker.exitPins == nil {
		tc.taskTracker.exitPins = make(map[ast.ExprID][]uint32)
	}
	tc.taskTracker.exitPins[tc.unwrapGroupExpr(expr)] = held
}
