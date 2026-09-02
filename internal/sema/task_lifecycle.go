package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// This file contains typeChecker methods for tracking task lifecycle in structured concurrency.
// These methods integrate with the TaskTracker (defined in task_tracking.go) to ensure
// that tasks are properly awaited before their enclosing scope exits.
//
// The structured concurrency model requires that every task must be either:
//   1. Awaited within its scope (task.await())
//   2. Returned from the scope (return task)
//   3. Passed to another function that takes ownership (foo(task))
//
// Failing to do any of these results in a potential task leak, which these methods
// help detect by tracking task state transitions.

// trackTaskAwait marks a task as awaited for structured concurrency tracking.
// This is called when a .await() method call is detected on a task.
//
// The function handles two cases:
//  1. Direct spawn expression: spawn foo.await()
//     - The task expression itself is marked as awaited
//  2. Variable reference: let t = spawn foo(); t.await()
//     - The binding symbol is used to locate and mark the task
//
// After a task is marked as awaited, it won't generate a "task not awaited"
// warning when the scope ends.
func (tc *typeChecker) trackTaskAwait(targetExpr ast.ExprID) {
	if tc.taskTracker == nil || !targetExpr.IsValid() {
		return
	}

	targetExpr = tc.unwrapGroupExpr(targetExpr)
	expr := tc.builder.Exprs.Get(targetExpr)
	if expr == nil {
		return
	}

	// Case 1: a task-producing expression awaited in place: `spawn foo().await()`,
	// or a clone awaited without a binding, `t.clone().await()`.
	if expr.Kind == ast.ExprTask || expr.Kind == ast.ExprSpawn || tc.taskTracker.IsTrackedExpr(targetExpr) {
		tc.taskTracker.MarkAwaitedByExpr(targetExpr)
		// The join ends the child on THIS path. Whether it ends it on every path
		// that reaches a later use is decided at the next merge, not here.
		tc.releaseTaskBorrowPins(tc.taskIDForAwaitTarget(targetExpr, symbols.NoSymbolID))
		tc.noteTaskContainerPopConsumedByExpr(targetExpr)
		return
	}

	// Case 2: Variable reference (t.await() where let t = task foo())
	if expr.Kind == ast.ExprIdent {
		if symID := tc.symbolForExpr(targetExpr); symID.IsValid() {
			tc.taskTracker.MarkAwaited(symID)
			tc.releaseTaskBorrowPins(tc.taskIDForAwaitTarget(targetExpr, symID))
			tc.noteTaskContainerPopBindingConsumed(symID)
		}
		return
	}

	tc.noteTaskContainerPopConsumedByExpr(targetExpr)
}

// trackTaskReturn marks a task as returned for structured concurrency tracking.
// This is called when a return statement returns a Task<T> value.
//
// Returning a task transfers responsibility for awaiting it to the caller.
// This is a valid way to propagate tasks up the call stack while maintaining
// structured concurrency guarantees.
//
// The function handles two cases:
//  1. Direct spawn expression: return spawn foo()
//  2. Variable reference: return t where let t = spawn foo()
func (tc *typeChecker) trackTaskReturn(returnExpr ast.ExprID) {
	if tc.taskTracker == nil || !returnExpr.IsValid() {
		return
	}

	// Only track if the expression is actually a Task<T> or an affine
	// `far Task<T>` remote handle (from `spawn on`), which transfers ownership
	// on return just like a local task.
	returnExpr = tc.unwrapGroupExpr(returnExpr)
	// An expression the tracker already registered is a task by construction:
	// a `.clone()` on a generic `&Task<T>` is registered from its receiver
	// while its own type may still be deferred, so it is asked first, before
	// the type of the returned expression is.
	if tc.taskTracker.IsTrackedExpr(returnExpr) {
		tc.taskTracker.MarkReturnedByExpr(returnExpr)
		return
	}
	returnType := tc.result.ExprTypes[returnExpr]
	if !tc.isTaskType(returnType) && !tc.isFarTaskType(returnType) {
		return
	}

	expr := tc.builder.Exprs.Get(returnExpr)
	if expr == nil {
		return
	}

	// Case 1: a task-producing expression returned in place: `return spawn foo()`
	// or `return t.clone()`.
	if expr.Kind == ast.ExprTask || expr.Kind == ast.ExprSpawn || tc.taskTracker.IsTrackedExpr(returnExpr) {
		tc.taskTracker.MarkReturnedByExpr(returnExpr)
		return
	}

	// Case 1b: Direct remote spawn (return spawn on dst { ... }), which shares
	// the ExprOn node with the Spawn flag set.
	if expr.Kind == ast.ExprOn {
		if data, ok := tc.builder.Exprs.On(returnExpr); ok && data != nil && data.Spawn {
			tc.taskTracker.MarkReturnedByExpr(returnExpr)
		}
		return
	}

	// Case 2: Variable reference (return t where let t = task foo())
	if expr.Kind == ast.ExprIdent {
		if symID := tc.symbolForExpr(returnExpr); symID.IsValid() {
			tc.taskTracker.MarkReturned(symID)
		}
	}
}

// trackTaskPassedAsArg marks a task as passed to a function as an argument.
// This is called when a Task<T> is used as an argument in a function call.
//
// Passing a task as an argument transfers ownership to the callee, who becomes
// responsible for awaiting the task. This is semantically equivalent to returning
// the task - the current scope is no longer responsible for awaiting it.
//
// Common patterns this enables:
//   - Task combinators: join_all([spawn a spawn b()])
//   - Task storage: task_queue.push(spawn compute())
//   - Higher-order functions: map_async(items, task_processor)
func (tc *typeChecker) trackTaskPassedAsArg(argExpr ast.ExprID) {
	if tc.taskTracker == nil || !argExpr.IsValid() {
		return
	}

	for {
		argExpr = tc.unwrapGroupExpr(argExpr)
		expr := tc.builder.Exprs.Get(argExpr)
		if expr == nil {
			return
		}
		if tc.taskTracker.IsTrackedExpr(argExpr) {
			// A clone passed on in place, `f(t.clone())`: the callee owns it now.
			tc.taskTracker.MarkPassedByExpr(argExpr)
			tc.noteTaskContainerPopConsumedByExpr(argExpr)
			return
		}
		switch expr.Kind {
		case ast.ExprTask, ast.ExprSpawn:
			tc.taskTracker.MarkPassedByExpr(argExpr)
			tc.noteTaskContainerPopConsumedByExpr(argExpr)
			return
		case ast.ExprIdent:
			if symID := tc.symbolForExpr(argExpr); symID.IsValid() && tc.isTaskType(tc.bindingType(symID)) {
				tc.taskTracker.MarkPassed(symID)
				tc.noteTaskContainerPopBindingConsumed(symID)
			}
			return
		case ast.ExprUnary:
			if data, ok := tc.builder.Exprs.Unary(argExpr); ok && data != nil {
				argExpr = data.Operand
				continue
			}
			return
		default:
			tc.noteTaskContainerPopConsumedByExpr(argExpr)
			return
		}
	}
}

// trackTaskClone records the handle a `.clone()` on a `Task<T>` produces as a
// task of its own.
//
// A clone is a second source-level entitlement to one result, and the
// structured-concurrency rule is per entitlement rather than per task: every
// handle the program holds must be awaited, returned or passed on, or the
// task's result and its failure are lost through that handle while its
// sibling looks perfectly observed. Before this, the tracker knew the spawn
// and the one binding it was assigned to, so `let s = t.clone(); t.await()`
// let `s` fall off the end of the scope unremarked while the mirror image was
// refused -- an asymmetry with no rule behind it.
//
// The clone's call expression is registered exactly as a spawn expression is,
// so the `let` that binds it (BindTaskByExpr) and the await, return and
// pass-as-argument sites all find it through the paths they already walk.
// Locality and async-block placement are the receiver's: a clone names the
// same task, so it can be no more remote than the handle it was made from.
func (tc *typeChecker) trackTaskClone(callExpr, receiverExpr ast.ExprID, receiverType types.TypeID, span source.Span) {
	if tc.taskTracker == nil || !callExpr.IsValid() || !tc.isTaskType(receiverType) {
		return
	}
	local := false
	receiverExpr = tc.unwrapGroupExpr(receiverExpr)
	if expr := tc.builder.Exprs.Get(receiverExpr); expr != nil {
		if expr.Kind == ast.ExprIdent {
			if symID := tc.symbolForExpr(receiverExpr); symID.IsValid() {
				local = tc.taskTracker.IsLocalBinding(symID)
			}
		} else {
			local = tc.taskTracker.IsLocalExpr(receiverExpr)
		}
	}
	tc.taskTracker.SpawnTask(callExpr, span, tc.currentScope(), tc.asyncBlockDepth > 0, local)
}
