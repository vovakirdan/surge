package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
)

// `@nonblocking` means the function waits for nothing (docs/ATTRIBUTES.md: it "forbids operations
// that may wait (park/unpark)"), and `.await()` waits until its task ends. So every `.await()` in a
// @nonblocking function is refused (owner ruling 2026-09-23): an awaited known blocking call
// (`m.lock().await()`) is reported as that call, exactly as `m.lock()` is; any other awaited task
// is reported at the await.

// checkNonblockingAwait is the @nonblocking walk's answer to `X.await()`.
func (tc *typeChecker) checkNonblockingAwait(call *ast.ExprCallData, span, fnSpan source.Span) {
	awaited := tc.awaitedTaskExpr(call)
	if !awaited.IsValid() {
		return
	}
	if inner, ok := tc.builder.Exprs.Call(tc.unwrapGroupExpr(awaited)); !ok || inner == nil || !tc.reportsBlockingCall(inner) {
		tc.report(diag.SemaLockNonblockingCallsWait, span,
			"@nonblocking function cannot await a task: `.await()` waits until the task ends")
	}
	tc.walkExprForBlockingCalls(awaited, fnSpan)
}

// reportsBlockingCall: the walk reports this call itself (checkBlockingCall), so its await adds nothing.
func (tc *typeChecker) reportsBlockingCall(call *ast.ExprCallData) bool {
	if _, _, blocking := tc.isBlockingMethodCall(call); blocking {
		return true
	}
	calleeSym, _ := tc.resolveCalleeAndReceiver(call)
	return calleeSym.IsValid() && tc.calleeHasWaitsOn(calleeSym)
}
