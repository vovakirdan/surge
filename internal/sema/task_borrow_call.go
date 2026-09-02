package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

// typeTaskProducingCall types a call and, when the call answers a Task, treats
// it exactly as `spawn` treats its operand: every reference the call's
// arguments (and a method receiver) borrow is a borrow the TASK holds, not the
// call, and it holds it until the task completes.
//
// `spawn f(&x)` was the only shape that said so, and it was not the only shape
// that borrows. `s.acquire().await()` reaches `async fn
// semaphore_acquire_task(s: &Semaphore)` through a plain method call that
// returns `Task<nothing>`; the task reads `s` while its creator is suspended,
// and `s` must therefore sit in storage that does not move under it -- the
// same StableActivationPlaces fact `spawn` records (task_borrow_pin.go), and
// the same pin until the join. Nothing about the call being spelled without
// `spawn` changes what the task does with the reference. Until the box that
// copied every shared reference at the constructor was removed, this shape
// was masked: the copy made the borrow a value. It is a borrow.
//
// The call that IS a spawn's operand is left to the spawn: the captures are
// the spawn's, and so is the pin. Any other call collects for itself, even
// one typed while an enclosing call is collecting -- `f(&s).await()` types
// `f(&s)` inside the `.await()` call, and it is `f`'s task that holds `s`,
// not `await`'s answer.
func (tc *typeChecker) typeTaskProducingCall(id ast.ExprID, span source.Span, call *ast.ExprCallData) types.TypeID {
	if tc.spawnOperand.IsValid() && tc.unwrapGroupExpr(tc.spawnOperand) == id {
		return tc.typeExprCall(id, span, call)
	}
	prevSpawnOperand := tc.spawnOperand
	prevSpawnCaptures := tc.spawnBorrowCaptures
	prevSpawnReaching := tc.spawnReachingExprs
	tc.spawnOperand = id
	tc.spawnBorrowCaptures = nil
	tc.spawnReachingExprs = tc.spawnReachingBorrowExprs(id)

	ty := tc.typeExprCall(id, span, call)

	collected := tc.spawnBorrowCaptures
	tc.spawnOperand = prevSpawnOperand
	tc.spawnReachingExprs = prevSpawnReaching
	tc.spawnBorrowCaptures = prevSpawnCaptures

	if ty == types.NoTypeID || !tc.isTaskType(ty) {
		return ty
	}
	// A borrow OF a task handle -- `t.clone()`, the entitlement intrinsics --
	// is not a frame borrow a child reads through: the task tracker already
	// owns that handle's story (clone entitlement, join), and a pin here would
	// refuse moving `t` after its clone and demand a second await for what is
	// the same task. Only places that are not themselves tasks are the frame.
	captures := collected[:0:0]
	for _, capture := range collected {
		if !capture.Place.IsValid() || !capture.Place.Base.IsValid() {
			continue
		}
		if tc.isTaskType(tc.bindingType(capture.Place.Base)) {
			continue
		}
		captures = append(captures, capture)
	}
	// The STORAGE fact, and only that: the place must not move while the task
	// can read it, so it is promoted to the activation's frame. The pin --
	// refusing a write to the place while the task may still be live -- stays
	// the spawn's: a Task answered by a plain call is not tracked as a spawn
	// today (`m.lock();` with the task dropped is an accepted program), and
	// opening a spawn's pin here would refuse it. The pointer is safe either
	// way: the storage is resident for the activation's whole life, so a write
	// under a live child is a value the child reads late, not a freed frame.
	for _, capture := range captures {
		tc.recordStableActivationPlace(capture.Place)
	}
	return ty
}
