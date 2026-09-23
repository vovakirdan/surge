package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// A Task-valued call whose value is dropped where it stands (`worker(&l);`, `m.lock();`) leaves
// nothing to join. That is sound only when the dropped value is the ONLY handle on a task that is
// still COLD: the runtime then discards the task unrun at that drop, and what it was lent is never
// read (docs/RUNTIME.md §3.1, `COLD --> DONE: last handle dropped, never polled`;
// docs/RUNTIME_MODEL_EXPLAINED.ru.md §6.3; RV2-DEBT-370). A direct call of an `async fn` answers
// such a task. So does a plain function each of whose returns IS such a call -- the call itself,
// so the function never held a handle it could cancel, clone or hand to a running task before
// returning it (`fn lock(self: &Mutex) -> Task<nothing> { return mutex_lock_task(self); }`).
// Anything else -- a function that returns a binding, a clone or a parameter, a function value,
// or a function not judged yet -- may hand back a task that is already running, and a dropped
// handle on a running task that borrows is refused where it is dropped.

// callReturnsSoleColdTask reports that the Task a call answers is fresh, unpublished, and the only
// handle on it WHEN THE CALLEE RETURNS: the callee is an `async fn` with a body, or a plain function
// whose signature records that every return is such a call (noteReturnedTaskCold). That it is
// still cold when the caller drops it -- a fail-fast cancel-all of its creation scope could reach
// it in between -- is the runtime's to keep (RT-COLD-2; R-j of RV2-DEBT-365).
func (tc *typeChecker) callReturnsSoleColdTask(call ast.ExprID) bool {
	callee := tc.calleeFunctionSymbol(call)
	return callee != nil && ((callee.Signature.Async && callee.Signature.HasBody) || callee.Signature.TaskCold == symbols.TaskReturnsCold)
}

// calleeFunctionSymbol is the declared function a call resolved to, or nil for a call through a
// value, a tag or anything else without a signature.
func (tc *typeChecker) calleeFunctionSymbol(call ast.ExprID) *symbols.Symbol {
	if tc.symbols == nil || tc.symbols.ExprSymbols == nil {
		return nil
	}
	callee := tc.symbolFromID(tc.symbols.ExprSymbols[call])
	if callee == nil || callee.Kind != symbols.SymbolFunction || callee.Signature == nil {
		return nil
	}
	return callee
}

// noteReturnedTaskCold accumulates, over the returns of the plain function being walked, whether
// every one hands back a direct call that answers a sole cold task. walkCallableBody commits it;
// anything else is sticky "other".
func (tc *typeChecker) noteReturnedTaskCold(returned ast.ExprID) {
	if tc.fnTaskCold == symbols.TaskReturnsOther {
		return
	}
	fact := symbols.TaskReturnsOther
	if ctx := tc.functionReturnContext(); ctx != nil && tc.awaitDepth == 0 && tc.isTaskType(ctx.expected) && returned.IsValid() {
		fact = symbols.TaskReturnsCold
		tc.forEachReturnLikeExpr(returned, func(candidate ast.ExprID) {
			if _, isCall := tc.builder.Exprs.Call(candidate); !isCall || !tc.callReturnsSoleColdTask(candidate) {
				fact = symbols.TaskReturnsOther
			}
		})
	}
	tc.fnTaskCold = fact
}

// refuseDroppedRunningTask reports a dropped Task-valued call whose task may already be running
// and holds a borrow. Nothing can join it, so the help says how to keep a handle that can -- in a
// way the enclosing function can actually write (`.await()` is legal only in an async body or an
// `@entrypoint` function) -- and the note points at the callee, where the other fix is.
func (tc *typeChecker) refuseDroppedRunningTask(call ast.ExprID, held []spawnBorrowCapture) {
	if tc.reporter == nil || len(held) == 0 {
		return
	}
	label := tc.placeLabel(held[0].Place)
	span := tc.exprSpan(call)
	builder := diag.ReportError(tc.reporter, diag.SemaBorrowThreadEscape, span,
		fmt.Sprintf("the task this call returns borrows %s and may already be running, and its handle is dropped here, so nothing can join it", label))
	if builder == nil {
		return
	}
	builder.WithNote(held[0].Span, fmt.Sprintf("the task borrowed %s here", label))
	callee := tc.calleeFunctionSymbol(call)
	name := ""
	if callee != nil {
		name = tc.lookupName(callee.Name)
		at := callee.Span
		if at == (source.Span{}) {
			at = span
		}
		if callee.Signature.TaskCold == symbols.TaskReturnsUnknown && callee.Signature.HasBody {
			builder.WithNote(at, fmt.Sprintf("'%s' is not fully checked yet at this call (it is declared below it, or this call is inside it), so whether it starts the task before returning it is not known here", name))
		} else {
			builder.WithNote(at, fmt.Sprintf("'%s' does not return a fresh `async fn` call directly, so it may start the task (a cancel, a clone handed on) before returning it", name))
		}
	}
	builder.WithHelp(span, tc.droppedRunningTaskHelp(label, name))
	builder.Emit()
}

// droppedRunningTaskHelp says what the enclosing function can write instead.
func (tc *typeChecker) droppedRunningTaskHelp(label, callee string) string {
	if tc.awaitAllowedHere() {
		return fmt.Sprintf("await the task here (`.await()`), or keep the handle and await it before %s goes out of scope", label)
	}
	help := fmt.Sprintf("this plain function cannot await: make it an `async fn` and await the task, or pass an owned value instead of borrowing %s", label)
	if callee != "" {
		help += fmt.Sprintf(", or have '%s' return its `async fn` call directly", callee)
	}
	return help
}

// awaitAllowedHere is the typing rule for `.await()` (type_expr_calling.go): inside an async body,
// or anywhere in an `@entrypoint` function.
func (tc *typeChecker) awaitAllowedHere() bool {
	if tc.awaitDepth > 0 {
		return true
	}
	sym := tc.symbolFromID(tc.currentFnSym())
	return sym != nil && sym.Flags&symbols.SymbolFlagEntrypoint != 0
}
