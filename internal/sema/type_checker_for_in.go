package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A `for` loop READS its elements. It does not consume them, and it is not
// the way a container of affine elements is emptied — owner ruling 2026-08-26
// (RV2-DEBT-258), closing the question the read-only ruling of 2026-08-13 left
// open. Three refusals below follow from that, beside the two write refusals
// in for_in_readonly.go:
//
//   - moving out of the loop binding, whole (`take(s)`, `t.await()`) or by
//     field (`take(own it.name)`). The binding is a word-wise copy the
//     container still owns; the move gives the element away while its slot
//     still holds the same word, and the container's drop glue then frees it
//     again. Measured: `Invalid free` under valgrind for strings, and
//     `panic: async: invalid task owner shard` at teardown for task handles.
//     An element that owns no heap has nothing to free twice and is let
//     through (`compare r { Some(v) => ... }` over an `Option<int>`).
//   - `own` in the iterable position. `for x in own xs` is what a consuming
//     loop would look like, and the language does not have one: the spelling
//     was accepted and ignored, exactly the disposition SEM3202 refuses for
//     `&mut`. It is refused by name, and the body's move is not reported on
//     top of it — the author already asked for the loop that does not exist.
//   - a task container iterated by `for` stays PENDING. The tracker used to
//     demand that the loop move its binding, which is the double free above
//     spelled as a requirement; now the loop is a read, and the container's
//     own scope-exit refusal (SEM3107) names the loop that read it.
//
// The way out is the same in all three: drain by popping,
// `while xs.__len() > 0:uint { let x = xs.pop().safe(); ... }`, which the
// task tracker already accepts as a drain (taskContainerDrainLoop) and which
// empties the slot it takes from.

func (tc *typeChecker) walkForInStmt(id ast.StmtID, stmt *ast.Stmt) {
	if stmt == nil {
		return
	}
	forIn := tc.builder.Stmts.ForIn(id)
	if forIn == nil {
		return
	}
	scope := tc.scopeForStmt(id)
	pushed := tc.pushScope(scope)

	tc.rejectMutableIterable(forIn.Iterable)
	consumingRequested := tc.rejectOwnedIterable(forIn)

	iterableType := tc.typeExpr(forIn.Iterable)
	if tc.isTaskContainerType(iterableType) {
		if place, ok := tc.taskContainerPlace(forIn.Iterable); ok {
			tc.noteForInReadsTaskContainer(place, forIn, stmt.Span)
		}
	}

	elemType := tc.forInElementType(forIn, iterableType, scope, stmt.Span)

	var loopSym symbols.SymbolID
	if forIn.Pattern != source.NoStringID {
		if symID := tc.stmtSymbols[id]; symID.IsValid() && elemType != types.NoTypeID {
			tc.bindingTypes[symID] = elemType
			loopSym = symID
			// The loop binding does not own the element. It is bound by a
			// word-wise copy of the iterator's payload - visible in MIR as
			// `tag_payload_move copy` - and the frame emits no drop for it,
			// because the container is still the owner. Anything that frees
			// THROUGH this binding therefore frees the container's storage,
			// which the container then frees again on its own reclamation.
			// Measured: `for s in strs { s = mk(7); }` aborts natively with
			// `free(): double free detected in tcache 2`.
			tc.markNonOwningBinding(symID)
		}
	}

	movedBeforeLoop := tc.snapshotMovedPlaces()
	tc.enterLoopDropScope()
	tc.walkStmt(forIn.Body)
	tc.rejectLoopBackEdgeMoves(movedBeforeLoop, "for-in loop")
	tc.leaveLoopDropScope()
	if !consumingRequested {
		tc.rejectMoveOutOfLoopBinding(forIn, loopSym)
	}
	if pushed {
		tc.leaveScope()
	}
}

func (tc *typeChecker) forInElementType(forIn *ast.ForInStmt, iterableType types.TypeID, scope symbols.ScopeID, span source.Span) types.TypeID {
	inferredElemType := types.NoTypeID
	if iterableType != types.NoTypeID {
		inferredElemType = tc.inferForInElementType(forIn.Iterable, iterableType, span)
	}
	if !forIn.Type.IsValid() {
		return inferredElemType
	}
	elemType := tc.resolveTypeExprWithScope(forIn.Type, scope)
	if elemType != types.NoTypeID && inferredElemType != types.NoTypeID && !tc.typesAssignable(elemType, inferredElemType, true) {
		tc.report(diag.SemaTypeMismatch, tc.typeSpan(forIn.Type),
			"iterator yields %s, not %s",
			tc.typeLabel(inferredElemType), tc.typeLabel(elemType))
	}
	return elemType
}

// rejectMoveOutOfLoopBinding refuses a body that moved the loop binding, whole
// or by field. It is asked AFTER the body is walked, from the moved-set the
// body left behind: the move was recorded where it happened, with its span,
// and the binding's whole place overlaps a field move as well as a whole one
// (movedPlaceCovering), so `take(own it.name)` is caught beside `take(it)`.
//
// Copy elements never reach here — observeMove records no move for them — so
// `for i in ns { take_int(i) }` stays legal, as it should: nothing is given
// away by copying an int. A move-only element that owns NO heap is let through
// on the same ground: `compare r { Some(v) => ... }` over an `Option<int>`
// binding moves `r` by the language's rule for unions, but what leaves is
// bits the container does not free again, so there is no double free to
// refuse — only the ordinary use-after-move, which the moved-set keeps. The
// refusal is for what owns heap: a string, a task handle, a reference-counted
// scalar inside a payload (an owned `compare` moves the count out of the
// envelope, and the binding releases what the container still counts).
func (tc *typeChecker) rejectMoveOutOfLoopBinding(forIn *ast.ForInStmt, loopSym symbols.SymbolID) {
	if !loopSym.IsValid() {
		return
	}
	moved, moveSpan, ok := tc.movedPlaceCovering(wholePlace(loopSym))
	if !ok {
		return
	}
	elemType := tc.bindingType(loopSym)
	movedType, known := tc.loopBindingMovedType(elemType, moved)
	if known && !tc.ownsHeap(movedType) {
		return
	}
	name := tc.bindingName(loopSym)
	headline := fmt.Sprintf("cannot move out of `%s`: a `for` loop binding only reads the element", name)
	if moved.Path != "" {
		headline = fmt.Sprintf("cannot move `%s` out of `%s`: a `for` loop binding only reads the element",
			tc.plainPlaceLabel(moved), name)
	}
	if tc.reporter == nil {
		tc.report(diag.SemaMoveOutOfLoopBinding, moveSpan, "%s", headline)
		return
	}
	b := diag.ReportError(tc.reporter, diag.SemaMoveOutOfLoopBinding, moveSpan, headline)
	if b == nil {
		return
	}
	b.WithNote(forIn.PatternSpan, fmt.Sprintf(
		"`%s` is a copy of an element the container still owns: what leaves through it stays in its slot, and the container frees it again",
		name))
	b.WithHelp(tc.exprSpan(forIn.Iterable), tc.forInDrainHelp(forIn.Iterable, name))
	// A loop that only meant to READ has a cheaper way out than a drain: pass
	// a copy. Offered for a whole move of an ordinary value, through the one
	// clone-advice table so the clause cannot name a clone the type lacks.
	// Not for a task handle - its `.clone()` is an entitlement, and the
	// container would still need its drain - and not for a field move, whose
	// subject is not the binding the sentence would name.
	if moved.Path == "" && !tc.isTaskType(elemType) {
		if advice := tc.cloneAdviceFor(adviceMoveOutOfLoopBinding, elemType, name); advice.Help != "" {
			b.WithHelp(moveSpan, advice.Help)
		}
	}
	// A union is read by `compare`, and a compare through a borrow is the form
	// that compiles: it matches the tags without taking a payload, so the
	// arm's bindings owe nothing the container still owns. Spelled with the
	// place the move named, whole (`&r`) or by field (`&it.opt`).
	if known && tc.isUnionValueType(movedType) {
		subject := name
		if moved.Path != "" {
			subject = tc.plainPlaceLabel(moved)
		}
		b.WithHelp(moveSpan, fmt.Sprintf(
			"to read the tags without taking the payload, compare through a borrow: `compare &%s { ... }`", subject))
	}
	b.Emit()
}

// loopBindingMovedType names the type of what left through the binding: the
// element itself for a whole move, the field or tuple element for a partial
// one, walked by the same parts residual drops enumerate. A path the walk
// cannot name — an index, a deref — answers unknown, and the caller refuses
// what it cannot see.
func (tc *typeChecker) loopBindingMovedType(elemType types.TypeID, moved Place) (types.TypeID, bool) {
	ty := elemType
	if moved.Path == "" || tc.borrow == nil {
		return ty, ty != types.NoTypeID
	}
	for _, seg := range tc.borrow.placeSegments(moved) {
		next := types.NoTypeID
		for _, part := range tc.enumerableParts(ty) {
			if part.segment == seg {
				next = part.ty
				break
			}
		}
		if next == types.NoTypeID {
			return types.NoTypeID, false
		}
		ty = next
	}
	return ty, true
}

// isUnionValueType reports whether the value behind this type is a union.
func (tc *typeChecker) isUnionValueType(id types.TypeID) bool {
	if tc.types == nil || id == types.NoTypeID {
		return false
	}
	tt, ok := tc.types.Lookup(tc.resolveAlias(tc.valueType(id)))
	return ok && tt.Kind == types.KindUnion
}

// rejectOwnedIterable refuses `own` in the iterable position. The test is on
// the `own expr` SYNTAX, like rejectMutableIterable's on `&mut expr`: an
// iterable of `own T[]` type that arrived as a parameter is not the mistake,
// asking for consumption AT the loop is. Parentheses are looked through, so
// `for s in (own names)` is the same request as `for s in own names` and not a
// spelling that slips past the rule. Reports whether it refused, so the body's
// move is not reported a second time.
func (tc *typeChecker) rejectOwnedIterable(forIn *ast.ForInStmt) bool {
	if !forIn.Iterable.IsValid() {
		return false
	}
	iterable := tc.unwrapGroupExpr(forIn.Iterable)
	node := tc.builder.Exprs.Get(iterable)
	if node == nil || node.Kind != ast.ExprUnary {
		return false
	}
	unary := tc.builder.Exprs.Unaries.Get(uint32(node.Payload))
	if unary == nil || unary.Op != ast.ExprUnaryOwn {
		return false
	}
	span := tc.exprSpan(iterable)
	headline := "cannot iterate over `own`: a consuming `for` is not a feature yet"
	if tc.reporter == nil {
		tc.report(diag.SemaOwnedIterable, span, "%s", headline)
		return true
	}
	b := diag.ReportError(tc.reporter, diag.SemaOwnedIterable, span, headline)
	if b == nil {
		return true
	}
	b.WithNote(span,
		"a `for` loop reads each element and leaves it in the container whatever the iterable is, so this `own` would be accepted and then ignored")
	b.WithHelp(span, tc.forInDrainHelp(unary.Operand, tc.lookupName(forIn.Pattern)))
	b.Emit()
	return true
}

// forInDrainHelp spells the legal drain for the container the loop named. The
// `__len` and `pop().safe()` forms are the ones the task tracker recognises as
// a drain (taskContainerDrainLoop, taskContainerPopSource), so the advice is
// the shape that compiles, not a paraphrase of it.
func (tc *typeChecker) forInDrainHelp(iterable ast.ExprID, binding string) string {
	container := "xs"
	if ident, ok := tc.builder.Exprs.Ident(tc.unwrapGroupExpr(iterable)); ok && ident != nil {
		if name := tc.lookupName(ident.Name); name != "" {
			container = name
		}
	}
	if binding == "" || binding == "_" {
		binding = "x"
	}
	return fmt.Sprintf(
		"to consume the elements, drain the container instead: `while %s.__len() > 0:uint { let %s = %s.pop().safe(); ... }`",
		container, binding, container)
}

// noteForInReadsTaskContainer records that a `for` read a pending task
// container, so the scope-exit refusal can point at the loop. The container
// stays pending: a read is not a drain, and the loop is no longer allowed to
// pretend to be one by moving its binding.
func (tc *typeChecker) noteForInReadsTaskContainer(place Place, forIn *ast.ForInStmt, span source.Span) {
	info := tc.taskContainers[place]
	if info == nil || !info.Pending || info.ForIn != (source.Span{}) {
		return
	}
	if forIn.PatternSpan != (source.Span{}) {
		span = forIn.PatternSpan
	}
	info.ForIn = span
}

// reportTaskContainerUndrained is the scope-exit refusal for a pending task
// container. It lives beside the for-in rules because what it adds to the
// tracker's bare sentence is the statement the author can change: the `for`
// that only read the tasks when the author believed it consumed them, or the
// `return`/`break` that left the drain loop with tasks still in the container
// -- there the refusal moves to the exit, and the container becomes the note.
func (tc *typeChecker) reportTaskContainerUndrained(place Place, info *taskContainerInfo, span source.Span) {
	headline := "task container has unconsumed tasks at scope exit (drain required)"
	if tc.reporter == nil || info == nil {
		tc.report(diag.SemaTaskNotAwaited, span, "%s", headline)
		return
	}
	container := tc.bindingName(place.Base)
	if info.Exit != (source.Span{}) {
		tc.reportTaskContainerDrainAbandoned(container, info, span)
		return
	}
	if info.ForIn == (source.Span{}) {
		tc.report(diag.SemaTaskNotAwaited, span, "%s", headline)
		return
	}
	b := diag.ReportError(tc.reporter, diag.SemaTaskNotAwaited, span, headline)
	if b == nil {
		return
	}
	b.WithNote(info.ForIn, fmt.Sprintf(
		"the `for` loop here only reads the tasks in `%s`; it does not drain them", container))
	b.WithHelp(info.ForIn, fmt.Sprintf(
		"drain the container instead: `while %s.__len() > 0:uint { let t = %s.pop().safe(); ... }`",
		container, container))
	b.Emit()
}

// reportTaskContainerDrainAbandoned is the same refusal, at the exit. The
// rule is the strict one the owner kept -- a spawned task is awaited or
// returned, never abandoned -- and the drain loop is where the author meets
// it: `if !ok { return 1; }` inside `while tasks.__len() > 0:uint { ... }`
// leaves every task not yet popped in the container on that path. The way
// out is the shape the corpus already uses, a flag set inside and the exit
// after the loop.
func (tc *typeChecker) reportTaskContainerDrainAbandoned(container string, info *taskContainerInfo, containerSpan source.Span) {
	b := diag.ReportError(tc.reporter, diag.SemaTaskNotAwaited, info.Exit, fmt.Sprintf(
		"this `%s` leaves the drain of `%s` unfinished: the tasks not yet popped are abandoned on this path",
		info.ExitKind, container))
	if b == nil {
		return
	}
	b.WithNote(containerSpan, fmt.Sprintf(
		"`%s` still holds tasks here; every task pushed into it must be awaited or returned before its scope ends", container))
	b.WithHelp(info.Exit, fmt.Sprintf(
		"finish the drain first: keep the outcome in a local (`let mut failed = false;` ... `failed = true;`) and `%s` after the `while` has emptied `%s`",
		info.ExitKind, container))
	b.Emit()
}
