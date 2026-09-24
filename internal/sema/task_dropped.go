package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/types"
)

// A task does its work only when it is awaited or spawned. A call of an `async fn` and an
// `async { }` block make their task cold: it runs only once a spawn, an await, a cancel or its
// scope's join publishes it, and a handle dropped before that ends it unrun
// (docs/RUNTIME_MODEL_EXPLAINED.ru.md 6.3; docs/RUNTIME.md 3.1). So `m.lock();` takes no lock
// and `worker(&l);` does no work. `checkpoint()`, `sleep(n)` and `blocking { }` are published
// when they are made, but a dropped handle leaves nothing waiting on them: the statement does
// not yield, does not sleep, and never learns that the body ended. Owner ruling 2026-09-23: a
// task dropped where it is made is an error, as `#[must_use]` makes it a warning for a Rust
// future.
//
// Where a value is dropped: an expression statement and `let _ = ...` (the roots), reached
// through what hands a value on unchanged -- a group, a value block's results and legacy
// tail, a compare's arms, a ternary's branches, a select's or race's results -- and a classic
// for loop's step. Kept: awaited, spawned, bound to a name, returned, passed, stored. A handle
// the task tracker files under its scope -- a `spawn`, a `clone` -- answers to SEM3107 when
// nothing awaits it, and is not asked again here; a dropped call the task check refused
// because its task may already be running over a borrow (SEM3021, task_discarded_call.go) is
// not reported twice.

// refuseDroppedTasks reports every task made and dropped by the discarded expression root.
func (tc *typeChecker) refuseDroppedTasks(root ast.ExprID) {
	if tc == nil || tc.reporter == nil || tc.result == nil || tc.builder == nil || !root.IsValid() {
		return
	}
	tc.forEachDroppedValue(root, func(leaf ast.ExprID) {
		if !tc.isOwnedTaskValue(tc.result.ExprTypes[leaf]) || tc.droppedTaskReported(leaf) {
			return
		}
		if !tc.makesTask(leaf) {
			return
		}
		if tc.taskTracker != nil && tc.taskTracker.IsScopedExpr(leaf) {
			return
		}
		tc.reportDroppedTask(root, leaf)
	})
}

// typeDroppedExpr types an expression statement and refuses the tasks it drops.
func (tc *typeChecker) typeDroppedExpr(expr ast.ExprID) {
	tc.typeExpr(expr)
	tc.refuseDroppedTasks(expr)
}

// typeDroppedStep types a classic for loop's step, which may not run, and refuses the tasks it drops.
func (tc *typeChecker) typeDroppedStep(step ast.ExprID) {
	tc.typeExprMaybeSkipped(step)
	tc.refuseDroppedTasks(step)
}

// forEachDroppedValue visits the expressions whose value becomes the discarded value of root.
func (tc *typeChecker) forEachDroppedValue(root ast.ExprID, visit func(ast.ExprID)) {
	var walk func(ast.ExprID)
	walk = func(expr ast.ExprID) {
		expr = tc.unwrapGroupExpr(expr)
		if !expr.IsValid() {
			return
		}
		node := tc.builder.Exprs.Get(expr)
		if node == nil {
			return
		}
		if results := tc.blockResultExprs[expr]; len(results) != 0 {
			for _, result := range results {
				// A `return` inside the block leaves the FUNCTION with its value (kept); only what the
				// block itself gives -- `ret`, an implicit return, the legacy tail -- is dropped with it.
				if _, leaves := tc.abruptBlockResults[result]; !leaves {
					walk(result)
				}
			}
			return
		}
		switch node.Kind {
		case ast.ExprBlock:
			if block, ok := tc.builder.Exprs.Block(expr); ok && block != nil {
				if tail, _, kind, has := tc.legacyImplicitBlockTailExpr(block); has && kind == legacyBlockTailExprStmt {
					walk(tail)
				}
			}
			return
		case ast.ExprCompare:
			if choice, ok := tc.builder.Exprs.Compare(expr); ok && choice != nil {
				for _, arm := range choice.Arms {
					walk(arm.Result)
				}
			}
			return
		case ast.ExprTernary:
			if choice, ok := tc.builder.Exprs.Ternary(expr); ok && choice != nil {
				walk(choice.TrueExpr)
				walk(choice.FalseExpr)
			}
			return
		case ast.ExprSelect, ast.ExprRace:
			data, ok := tc.builder.Exprs.Select(expr)
			if node.Kind == ast.ExprRace {
				data, ok = tc.builder.Exprs.Race(expr)
			}
			if ok && data != nil {
				for _, arm := range data.Arms {
					walk(arm.Result)
				}
			}
			return
		}
		visit(expr)
	}
	walk(root)
}

// makesTask: the leaf creates the task value it gives -- a call, an `async { }` or a `blocking { }`
// block. (A cast of a Task is not expressible: `x to Task<int>` is SEM3015.) A PLACE (`t`, `h.t`, `ts[0]`, `pair.0`, `*r`) is read where it stands, not moved
// (let_forms.go), so `let _ = t;` and `t;` drop nothing: the handle stays with its binding.
func (tc *typeChecker) makesTask(leaf ast.ExprID) bool {
	node := tc.builder.Exprs.Get(leaf)
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.ExprCall, ast.ExprAsync, ast.ExprBlocking:
		return true
	}
	return false
}

// isOwnedTaskValue: a Task handle held by value. A reference to a task is not the task.
func (tc *typeChecker) isOwnedTaskValue(id types.TypeID) bool {
	if id == types.NoTypeID || tc.types == nil {
		return false
	}
	resolved := tc.resolveAlias(id)
	if tt, ok := tc.types.Lookup(resolved); ok && tt.Kind == types.KindOwn {
		resolved = tc.resolveAlias(tt.Elem)
	}
	if tt, ok := tc.types.Lookup(resolved); ok && (tt.Kind == types.KindReference || tt.Kind == types.KindPointer) {
		return false
	}
	return tc.isTaskType(resolved)
}

// noteAbruptBlockResult records a block result that is a `return` out of the function
// (collectedResult.abrupt, type_checker_returns.go), not the block's own value.
func (tc *typeChecker) noteAbruptBlockResult(expr ast.ExprID) {
	if tc.abruptBlockResults == nil {
		tc.abruptBlockResults = make(map[ast.ExprID]struct{})
	}
	tc.abruptBlockResults[expr] = struct{}{}
}

func (tc *typeChecker) noteDroppedTaskRefused(call ast.ExprID) {
	if tc.droppedTasksRefused == nil {
		tc.droppedTasksRefused = make(map[ast.ExprID]struct{})
	}
	tc.droppedTasksRefused[tc.unwrapGroupExpr(call)] = struct{}{}
}

func (tc *typeChecker) droppedTaskReported(leaf ast.ExprID) bool {
	_, ok := tc.droppedTasksRefused[leaf]
	return ok
}

// reportDroppedTask says what the dropped task would have done and how this function keeps it.
func (tc *typeChecker) reportDroppedTask(root, leaf ast.ExprID) {
	span := tc.exprSpan(leaf)
	message, note := tc.droppedTaskStory(leaf)
	builder := diag.ReportError(tc.reporter, diag.SemaTaskDropped, span, message)
	if builder == nil {
		return
	}
	if note != "" {
		builder.WithNote(span, note)
	}
	help, awaitHere := tc.droppedTaskHelp()
	builder.WithHelp(span, help)
	if awaitHere && leaf == tc.unwrapGroupExpr(root) {
		builder.WithFixSuggestion(fix.InsertText("await the task here", span.ZeroideToEnd(), ".await()", "", fix.Preferred()))
	}
	builder.Emit()
}

// droppedTaskStory is the message and the note: what the task was, and why dropping it loses it.
func (tc *typeChecker) droppedTaskStory(leaf ast.ExprID) (message, note string) {
	node := tc.builder.Exprs.Get(leaf)
	switch {
	case node != nil && node.Kind == ast.ExprAsync:
		return "this `async` block is dropped here without being awaited, so its body never runs",
			"an `async { }` block makes its task without starting it; the task runs only when it is awaited or spawned"
	case node != nil && node.Kind == ast.ExprBlocking:
		return "a `blocking` block without `.await()` is not waited for: the task it starts is dropped here, and nothing reads its result",
			"the body runs on the blocking pool, and nothing in this function learns when it ends"
	}
	callee := tc.calleeFunctionSymbol(leaf)
	name := ""
	if callee != nil {
		name = tc.lookupName(callee.Name)
		if callee.ReceiverKey == "" && !callee.Signature.HasBody {
			switch name {
			case "checkpoint":
				return "`checkpoint()` without `.await()` does not yield: the task it starts is dropped here",
					"`checkpoint()` gives the scheduler a turn only when its task is awaited"
			case "sleep":
				return "`sleep` without `.await()` does not pause: the task it starts is dropped here",
					"`sleep(ms)` suspends only when its task is awaited"
			}
		}
	}
	if tc.callReturnsSoleColdTask(leaf) {
		if what := tc.droppedSyncTaskNote(leaf, name); what != "" {
			return fmt.Sprintf("the task '%s' makes is dropped here without being awaited, so it never runs", name), what
		}
		return "the task this call makes is dropped here without being awaited, so it never runs",
			"a call of an `async fn` makes its task without starting it; the task runs only when it is awaited or spawned"
	}
	if name != "" {
		return "the task this call returns is dropped here, so nothing can wait for it or cancel it",
			fmt.Sprintf("'%s' may have started the task before returning it, and dropping the handle does not stop it", name)
	}
	return "the task this expression gives is dropped here, so nothing can wait for it or cancel it", ""
}

// droppedSyncTaskNote names what a dropped core synchronisation task leaves undone.
func (tc *typeChecker) droppedSyncTaskNote(leaf ast.ExprID, method string) string {
	call, ok := tc.builder.Exprs.Call(leaf)
	if !ok || call == nil {
		return ""
	}
	member, ok := tc.builder.Exprs.Member(tc.unwrapGroupExpr(call.Target))
	if !ok || member == nil {
		return ""
	}
	receiver := tc.resolveAlias(tc.result.ExprTypes[member.Target])
	for {
		tt, found := tc.types.Lookup(receiver)
		if !found || (tt.Kind != types.KindReference && tt.Kind != types.KindOwn) {
			break
		}
		receiver = tc.resolveAlias(tt.Elem)
	}
	info, found := tc.types.StructInfo(receiver)
	if !found || info == nil {
		return ""
	}
	switch tc.lookupTypeName(receiver, info.Name) + "." + method {
	case "Mutex.lock":
		return "`lock()` takes the mutex only when its task is awaited, so nothing is locked here"
	case "Semaphore.acquire":
		return "`acquire()` takes a permit only when its task is awaited, so no permit is taken here"
	case "Barrier.arrive_and_wait":
		return "`arrive_and_wait()` arrives only when its task is awaited, so this party never arrives"
	case "Condition.wait":
		return "`wait()` has already released the mutex, and only its awaited task takes it back, so the mutex stays released"
	}
	return ""
}

// droppedTaskHelp says how the enclosing function keeps the task, and whether `.await()` is
// legal where the task was dropped.
func (tc *typeChecker) droppedTaskHelp() (string, bool) {
	if tc.inBlockingBody() {
		return "a `blocking` body cannot wait for a task: make the task and await it outside the `blocking` body", false
	}
	if tc.awaitAllowedHere() {
		if tc.asyncBlockDepth > 0 {
			return "await it here (`.await()`), keep the handle (`let t = ...;`) and await it later, or `spawn` it: this `async` block waits for what it spawns before it ends", true
		}
		return "await it here (`.await()`), or keep the handle (`let t = ...;`) and await it before this function returns", true
	}
	name := "this function"
	if sym := tc.symbolFromID(tc.currentFnSym()); sym != nil {
		name = fmt.Sprintf("'%s'", tc.lookupName(sym.Name))
	}
	return fmt.Sprintf("%s is not `async`, so it cannot wait for the task: make it an `async fn` and await the task, or return the task to its caller", name), false
}

// inBlockingBody reports that the innermost task body around this point is a `blocking { }` body
// (an `async { }` nested in one is its own task and may await). The model forbids suspension on
// the blocking pool; `.await()` there is not refused yet -- SEM3152 covers `on` only, and the typing
// rule of `.await()` reads awaitDepth alone (type_expr_calling.go) -- a pre-existing gap this help
// does not widen.
func (tc *typeChecker) inBlockingBody() bool {
	for i := len(tc.returnStack) - 1; i >= 0; i-- {
		switch tc.returnStack[i].kind {
		case returnCtxTaskPayload:
			return tc.returnStack[i].bodyLabel == "blocking body"
		case returnCtxFunction:
			return false
		}
	}
	return false
}

// awaitedTaskExpr is X in the call `X.await()`, or no expression. The lock-balance and
// @nonblocking walks read an awaited task's creation through it: `m.lock().await()` is where
// the lock is taken.
func (tc *typeChecker) awaitedTaskExpr(call *ast.ExprCallData) ast.ExprID {
	if call == nil || len(call.Args) != 0 {
		return ast.NoExprID
	}
	member, ok := tc.builder.Exprs.Member(tc.unwrapGroupExpr(call.Target))
	if !ok || member == nil || tc.lookupName(member.Field) != "await" {
		return ast.NoExprID
	}
	return member.Target
}
