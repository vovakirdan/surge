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
		ty := tc.typeExprCall(id, span, call)
		// The spawn collects for itself. What no borrow record in its operand shows is
		// handed to it here: a reference a call gave back, and a fixed-array window.
		tc.noteReachingLoans(call)
		tc.spawnBorrowCaptures = append(tc.spawnBorrowCaptures, tc.reachingFixedViews(id, call)...)
		return ty
	}
	prevSpawnOperand := tc.spawnOperand
	prevSpawnCaptures := tc.spawnBorrowCaptures
	prevSpawnReaching := tc.spawnReachingExprs
	tc.spawnOperand = id
	tc.spawnBorrowCaptures = nil
	tc.spawnReachingExprs = tc.spawnReachingBorrowExprs(id)

	ty := tc.typeExprCall(id, span, call)
	tc.noteReachingLoans(call)
	lent := tc.lentValueCaptures(ty, call)

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
	// The STORAGE fact first: the place must not move while the task can read it,
	// so it is promoted to the activation's frame.
	for _, capture := range captures {
		tc.recordStableActivationPlace(capture.Place)
	}
	// Then the pin, which used to be the spawn's alone, and that is how
	// `let t = worker(&l); return t;` left its frame unremarked. A task answered
	// by a plain call holds its borrows exactly as a spawned one does, so it gets
	// an identity and the same pins. What it does not get is the await
	// obligation: it does not run until something starts it.
	//
	// A callee that recorded that the task it returns was built from owned values
	// only (`net.accept(&listener)`) was lent nothing by a REFERENCE. That record
	// is about references: a window into a fixed array is typed `T[]`, passes
	// through any by-value formal, and is pinned whatever the callee says. What a
	// reaching value carries through a parameter or a `let`, and a window a
	// parameter may have handed in, are flow-only pins (task_lent_value.go).
	if tc.calleeTaskBorrowsNothing(id) {
		captures = nil
		lent = nil
	}
	captures = append(captures, tc.reachingFixedViews(id, call)...)
	lent = append(lent, tc.windowParameterCaptures(call)...)
	if len(captures)+len(lent) == 0 || tc.taskTracker == nil {
		return ty
	}
	// A call whose value is dropped where it stands can be joined by nobody. It stays unpinned when
	// that drop discards the task unrun -- the value is the only handle on a task still cold, as a
	// direct `async fn` call or `m.lock()` answers -- and is refused otherwise (task_discarded_call.go).
	if tc.isExprDiscarded(id) {
		if !tc.callReturnsSoleColdTask(id) {
			tc.refuseDroppedRunningTask(id, append(captures, lent...))
		}
		return ty
	}
	taskID := tc.taskTracker.NoteCallTask(id, span, tc.currentScope(), tc.asyncBlockDepth > 0)
	tc.openTaskBorrowPins(taskID, captures)
	tc.openLentValuePins(taskID, lent)
	return ty
}

// reachingPositions lists, in source order, the expressions of a call whose value is handed to
// the task the call answers: the receiver of a method call, then the arguments.
func (tc *typeChecker) reachingPositions(call *ast.ExprCallData) []ast.ExprID {
	out := make([]ast.ExprID, 0, len(call.Args)+1)
	if member, ok := tc.builder.Exprs.Member(tc.unwrapGroupExpr(call.Target)); ok && member != nil {
		out = append(out, member.Target)
	}
	for _, arg := range call.Args {
		out = append(out, arg.Value)
	}
	return out
}

// noteReachingLoans captures the loan a reaching position CARRIES rather than takes. A borrow
// written in the call (`worker(&l)`) is recorded where it is made; a reference that was made
// earlier and arrives by name (`let r = &l; worker(r)`), or comes back out of a call
// (`worker(same(&l))`, `w(m.get_ref(&k))`), makes no new borrow, and the task holds its referent
// all the same. The question is what the value CARRIES, not how its type is spelled: an
// `Option<&V>` is a union that hands out a reference (carriedReferenceType).
func (tc *typeChecker) noteReachingLoans(call *ast.ExprCallData) {
	if tc.result == nil {
		return
	}
	for _, lender := range tc.reachingPositions(call) {
		if _, carries := tc.carriedReferenceType(tc.result.ExprTypes[lender]); carries {
			tc.noteCarriedLoans(lender)
		}
	}
}

// noteCarriedLoans captures every loan a reference-carrying expression can be carrying. One
// the shared walk names is taken as it is. Otherwise the expression is opened and each source
// asked the same question: the result carries one of them, and the checker cannot say which.
func (tc *typeChecker) noteCarriedLoans(lender ast.ExprID) {
	if bid := tc.inheritedBorrowForExpr(lender); bid != NoBorrowID {
		tc.noteSpawnBorrowCapture(bid, tc.exprSpan(lender))
		return
	}
	for _, source := range tc.carriedLoanSources(tc.unwrapGroupExpr(lender)) {
		tc.noteCarriedLoans(source)
	}
}

// carriedLoanSources lists the expressions a reference-carrying value can have come from. A
// call's result is judged by what went in -- the shared walk answers for a call only when
// exactly one argument can be the source (inheritedBorrowForCall), so with two or more every
// reaching position counts. A choice (`c ? &a : &b`, a `compare`, a value block's `ret`s) carries
// the reference of whichever arm ran, and the borrow written in an arm is not a reaching
// position of the call, so nothing else records it.
func (tc *typeChecker) carriedLoanSources(expr ast.ExprID) []ast.ExprID {
	if results := tc.blockResultExprs[expr]; len(results) != 0 {
		return results
	}
	if inner, ok := tc.builder.Exprs.Call(expr); ok && inner != nil {
		return tc.reachingPositions(inner)
	}
	if choice, ok := tc.builder.Exprs.Ternary(expr); ok && choice != nil {
		return []ast.ExprID{choice.TrueExpr, choice.FalseExpr}
	}
	var arms []ast.ExprID
	if choice, ok := tc.builder.Exprs.Compare(expr); ok && choice != nil {
		for _, arm := range choice.Arms {
			arms = append(arms, arm.Result)
		}
	}
	return arms
}

// reachingFixedViews answers the windows into a FIXED array that a call hands to its task. A
// fixed array has no header to retain: the window is a bare pointer into a frame slot
// (array_view_escape.go), typed `T[]` like an array that owns its buffer. It is a borrow of that
// slot in everything but spelling, so it pins the array it points into. A position that is
// not a window the checker traced is answered by untracedArrayCaptures.
func (tc *typeChecker) reachingFixedViews(id ast.ExprID, call *ast.ExprCallData) []spawnBorrowCapture {
	var views []spawnBorrowCapture
	for _, lender := range tc.reachingPositions(call) {
		if base := tc.fixedViewEscapeBase(lender); base.IsValid() {
			views = append(views, spawnBorrowCapture{Place: Place{Base: base}, Kind: BorrowShared, Span: tc.exprSpan(lender)})
		} else if untraced := tc.untracedArrayCaptures(id, lender); len(untraced) != 0 {
			views = append(views, untraced...)
		}
	}
	return views
}
