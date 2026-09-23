package sema

import (
	"fmt"
	"slices"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
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
			// A `spawn t` is a second handle too, and it is not a clone: no clone note for it.
			if node := tc.builder.Exprs.Get(clone.SpawnExpr); node != nil && node.Kind != ast.ExprSpawn {
				cloneSpan = clone.Span
			}
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

// refuseLivePinsAtExit refuses every pin live where control leaves a frame, except the pins of
// the tasks the exit hands back and the pins in outer, which belong to an enclosing frame and
// are put back. The handed-back tasks' pins are dropped: the path ends here.
func (tc *typeChecker) refuseLivePinsAtExit(handedBack ast.ExprID, outer map[taskBorrowPinKey]taskBorrowPin, what string) {
	if len(tc.taskBorrowPins) == 0 {
		return
	}
	named := tc.handedBackTasks(handedBack)
	kept := make(map[taskBorrowPinKey]taskBorrowPin)
	for key, pin := range tc.taskBorrowPins {
		_, isOuter := outer[key]
		if isOuter {
			kept[key] = pin
		}
		if isOuter || slices.Contains(named, key.Task) {
			delete(tc.taskBorrowPins, key)
		}
	}
	tc.refuseLivePinsAtAbruptExit(0, what)
	for key, pin := range kept {
		tc.taskBorrowPins[key] = pin
	}
}

// handedBackTasks names the tasks an exit's value is a handle on, by identity, through every
// block result the value can come from. A value nobody can name -- `pass(t)`, a ternary, an
// `async { }` that captured the handle -- names none, and then nothing is set aside.
func (tc *typeChecker) handedBackTasks(handedBack ast.ExprID) []uint32 {
	if tc.taskTracker == nil || !handedBack.IsValid() {
		return nil
	}
	var named []uint32
	tc.forEachReturnLikeExpr(handedBack, func(candidate ast.ExprID) {
		if id := tc.taskIDForAwaitTarget(candidate, tc.symbolForExpr(candidate)); id != 0 && !slices.Contains(named, id) {
			named = append(named, id)
		}
	})
	return named
}

// taskPinnedFrameLocal finds a place of this frame the task behind exit still holds. The pins
// are the record of what the task was actually handed, which the syntax of its call does not
// always show: a method receiver is lent without an `&` to read.
func (tc *typeChecker) taskPinnedFrameLocal(exit ast.ExprID) (symbols.SymbolID, source.Span) {
	task := tc.taskIDForAwaitTarget(exit, tc.symbolForExpr(exit))
	if task == 0 {
		return symbols.NoSymbolID, source.Span{}
	}
	for _, key := range sortedTaskBorrowPinKeys(tc.taskBorrowPins) {
		if key.Task == task && !tc.isFlowOnlyPin(key) && tc.isFrameLocalStorage(key.Place.Base) {
			return key.Place.Base, tc.taskBorrowPins[key].Span
		}
	}
	return symbols.NoSymbolID, source.Span{}
}

// refuseTaskBorrowsAtRet judges a `ret` that leaves an async/blocking body the way a `return`
// that leaves a function is judged. The body is a frame of its own: what it declared, and what
// it captured by value, is freed when it finishes, so a task it hands out must not borrow
// that, and no other task may still hold it. entryPins are the host's, not the body's.
func (tc *typeChecker) refuseTaskBorrowsAtRet(id ast.StmtID, entryPins map[taskBorrowPinKey]taskBorrowPin) {
	ret := tc.builder.Stmts.Ret(id)
	if ret == nil {
		return
	}
	tc.forEachReturnLikeExpr(ret.Expr, func(candidate ast.ExprID) {
		tc.checkTaskBorrowEscapeOnReturn(candidate, tc.result.ExprTypes[candidate], tc.exprSpan(candidate), ret.Expr)
	})
	tc.refuseLivePinsAtExit(ret.Expr, entryPins, "ret")
}

// walkCallableBody walks a function body, closes its last edge, and only then publishes what
// the body's returns said about the Task it hands back. A body that can fall off its end leaves
// the frame there exactly as a `return` does, and no `return` statement stands on that path to
// ask the question. The record is committed ONCE, here: while the body is being walked the
// signature still reads "unknown", so a call the function makes to itself -- or to a function
// that calls it back -- is pinned, whichever of its returns has been seen so far. The arrays of
// unknown provenance its tasks were handed are judged here too, once the whole body is known.
func (tc *typeChecker) walkCallableBody(body ast.StmtID) {
	outer, outerCold := tc.fnTaskBorrows, tc.fnTaskCold
	outerArrays := tc.beginUntracedArrays()
	tc.fnTaskBorrows = symbols.TaskBorrowsUnknown
	tc.fnTaskCold = symbols.TaskReturnsUnknown
	tc.walkStmt(body)
	if tc.returnStatus(body) != returnClosed {
		tc.refuseLivePinsAtAbruptExit(0, "function end")
	}
	tc.endUntracedArrays(outerArrays)
	if sym := tc.symbolFromID(tc.currentFnSym()); sym != nil && sym.Signature != nil {
		sym.Signature.TaskBorrows = symbols.TaskBorrowsLent
		if tc.fnTaskBorrows == symbols.TaskBorrowsNothing {
			sym.Signature.TaskBorrows = symbols.TaskBorrowsNothing
		}
		sym.Signature.TaskCold = symbols.TaskReturnsOther
		if tc.fnTaskCold == symbols.TaskReturnsCold {
			sym.Signature.TaskCold = symbols.TaskReturnsCold
		}
	}
	tc.fnTaskBorrows, tc.fnTaskCold = outer, outerCold
}

// noteReturnedTaskBorrows accumulates, over the returns of the plain function being walked,
// what the Task it returns can hold of the references it was lent. walkCallableBody commits the
// answer; a caller in a later module, or lower in this one, reads it (calleeTaskBorrowsNothing),
// and a caller checked earlier reads "unknown" and pins. Anything this does not recognise is
// "lent", and "lent" is sticky.
func (tc *typeChecker) noteReturnedTaskBorrows(returned ast.ExprID) {
	tc.noteReturnedTaskCold(returned)
	if tc.fnTaskBorrows == symbols.TaskBorrowsLent {
		return
	}
	fact := symbols.TaskBorrowsLent
	if ctx := tc.functionReturnContext(); ctx != nil && tc.awaitDepth == 0 && tc.isTaskType(ctx.expected) {
		fact = symbols.TaskBorrowsNothing
		tc.forEachReturnLikeExpr(returned, func(candidate ast.ExprID) {
			if !tc.callBuildsTaskFromOwnedValues(candidate) {
				fact = symbols.TaskBorrowsLent
			}
		})
	}
	tc.fnTaskBorrows = fact
}

// functionReturnContext is the function a `return` here leaves, or nil inside a body that has
// no function to leave.
func (tc *typeChecker) functionReturnContext() *returnContext {
	for i := len(tc.returnStack) - 1; i >= 0; i-- {
		switch tc.returnStack[i].kind {
		case returnCtxFunction:
			return &tc.returnStack[i]
		case returnCtxTaskPayload, returnCtxOnCrossing:
			return nil
		}
	}
	return nil
}

// callBuildsTaskFromOwnedValues reports that candidate is a call that captured nothing -- no
// borrow, no loan, no window into a fixed array -- to a declared function none of whose formals
// can carry a borrow or a task. Whatever the arguments were computed from, the task that call
// answers was built from owned values only: `accept_owned(listener_handle(l))` reads `l` to
// completion before the task exists. The judgement is on the FORMALS, so an argument the callee
// would borrow implicitly cannot slip through as a by-value expression; and on the CAPTURES,
// because a `T[]` formal is owned by type and a window into a fixed array is typed `T[]` too.
func (tc *typeChecker) callBuildsTaskFromOwnedValues(candidate ast.ExprID) bool {
	if _, ok := tc.builder.Exprs.Call(candidate); !ok || tc.symbols == nil || tc.types == nil {
		return false
	}
	if tc.callTaskCapturedAnything(candidate) {
		return false
	}
	if tc.untracedArrayLent(candidate) {
		return false
	}
	callee := tc.symbolFromID(tc.symbols.ExprSymbols[candidate])
	if callee == nil || callee.Kind != symbols.SymbolFunction {
		return false
	}
	info, ok := tc.types.FnInfo(callee.Type)
	if !ok || info == nil {
		return false
	}
	for _, param := range info.Params {
		if !tc.taskLendInertType(param) {
			return false
		}
	}
	return true
}

// taskLendInertType reports that a value of this type can carry neither a reference nor a
// task. A type the shape walk cannot read -- a callable, a generic parameter -- is not inert,
// and neither is a raw pointer, which the shape walk reads as owned.
func (tc *typeChecker) taskLendInertType(ty types.TypeID) bool {
	if ty == types.NoTypeID || tc.types == nil || tc.containsTaskType(ty) {
		return false
	}
	if tt, ok := tc.types.Lookup(tc.resolveAlias(ty)); !ok || tt.Kind == types.KindPointer {
		return false
	}
	if payloads, handle := tc.types.RuntimeHandlePayloads(tc.valueType(ty)); handle {
		for _, payload := range payloads {
			if !tc.taskLendInertType(payload) {
				return false
			}
		}
	}
	return returnOriginTypeShape(tc.types, ty, nil) == returnOriginRefFree
}

// calleeTaskBorrowsNothing reads that record at a call site.
func (tc *typeChecker) calleeTaskBorrowsNothing(call ast.ExprID) bool {
	if tc.symbols == nil || tc.symbols.ExprSymbols == nil {
		return false
	}
	callee := tc.symbolFromID(tc.symbols.ExprSymbols[call])
	return callee != nil && callee.Kind == symbols.SymbolFunction && callee.Signature != nil &&
		callee.Signature.TaskBorrows == symbols.TaskBorrowsNothing
}

// refusePinsOfEndedScope is the edge a block's own end is. A binding declared in the scope that
// ends here is freed here, so a task that still holds it must not be left running -- and "joined
// later" is no answer: a handle pushed into an outer container inside the block and drained
// after it was exactly that, and the drain released a pin whose place was already gone.
func (tc *typeChecker) refusePinsOfEndedScope(scope symbols.ScopeID) {
	tc.refusePinsOfEndedScopeExcept(scope, nil)
}

// refusePinsOfEndedScopeExcept is that edge with a binding spared: spared, when set, names a binding
// of the scope that does not die with it (task_block_expr_end.go).
func (tc *typeChecker) refusePinsOfEndedScopeExcept(scope symbols.ScopeID, spared func(symbols.SymbolID) bool) {
	if len(tc.taskBorrowPins) == 0 || tc.reporter == nil {
		return
	}
	for _, key := range sortedTaskBorrowPinKeys(tc.taskBorrowPins) {
		sym := tc.symbolFromID(key.Place.Base)
		if sym == nil || sym.Kind != symbols.SymbolLet || sym.Scope != scope || (spared != nil && spared(key.Place.Base)) {
			continue
		}
		pin := tc.taskBorrowPins[key]
		msg := fmt.Sprintf("a task still borrows %s at the end of the block that declares it", tc.placeLabel(key.Place))
		if b := diag.ReportError(tc.reporter, diag.SemaBorrowThreadEscape, pin.Span, msg); b != nil {
			b.WithHelp(pin.Span, "join the task before this block ends; a value that has to outlive the block is declared outside it")
			b.Emit()
		}
		delete(tc.taskBorrowPins, key)
	}
}

// refuseDropUnderTaskPin answers an explicit `@drop` of a binding a task still holds. A drop
// frees the storage as a move gives it away, and it used to ask nobody.
func (tc *typeChecker) refuseDropUnderTaskPin(symID symbols.SymbolID, span source.Span) bool {
	place := Place{Base: symID}
	pin, pinned := tc.taskBorrowPinFor(place)
	if pinned {
		tc.reportTaskBorrowPinConflict(place, span, pin, "drop")
	}
	return pinned
}
