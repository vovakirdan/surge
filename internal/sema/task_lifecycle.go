package sema

import (
	"surge/internal/ast"
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
//  1. Direct task expression: task foo().await()
//     - The task expression itself is marked as awaited
//  2. Variable reference: let t = task foo(); t.await()
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

	// Case 1: Direct task expression (task foo().await())
	if expr.Kind == ast.ExprTask {
		tc.taskTracker.MarkAwaitedByExpr(targetExpr)
		return
	}

	// Case 2: Variable reference (t.await() where let t = task foo())
	if expr.Kind == ast.ExprIdent {
		if symID := tc.symbolForExpr(targetExpr); symID.IsValid() {
			tc.taskTracker.MarkAwaited(symID)
		}
	}
}

// trackTaskReturn marks a task as returned for structured concurrency tracking.
// This is called when a return statement returns a Task<T> value.
//
// Returning a task transfers responsibility for awaiting it to the caller.
// This is a valid way to propagate tasks up the call stack while maintaining
// structured concurrency guarantees.
//
// The function handles two cases:
//  1. Direct task expression: return task foo()
//  2. Variable reference: return t where let t = task foo()
func (tc *typeChecker) trackTaskReturn(returnExpr ast.ExprID) {
	if tc.taskTracker == nil || !returnExpr.IsValid() {
		return
	}

	// Only track if the expression is actually a Task<T>
	returnExpr = tc.unwrapGroupExpr(returnExpr)
	returnType := tc.result.ExprTypes[returnExpr]
	if !tc.isTaskType(returnType) {
		return
	}

	expr := tc.builder.Exprs.Get(returnExpr)
	if expr == nil {
		return
	}

	// Case 1: Direct task expression (return task foo())
	if expr.Kind == ast.ExprTask {
		tc.taskTracker.MarkReturnedByExpr(returnExpr)
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
//   - Task combinators: join_all([task a(), task b()])
//   - Task storage: task_queue.push(task compute())
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
		switch expr.Kind {
		case ast.ExprTask:
			tc.taskTracker.MarkPassedByExpr(argExpr)
			return
		case ast.ExprIdent:
			if symID := tc.symbolForExpr(argExpr); symID.IsValid() && tc.isTaskType(tc.bindingType(symID)) {
				tc.taskTracker.MarkPassed(symID)
			}
			return
		case ast.ExprUnary:
			if data, ok := tc.builder.Exprs.Unary(argExpr); ok && data != nil {
				argExpr = data.Operand
				continue
			}
			return
		default:
			return
		}
	}
}
