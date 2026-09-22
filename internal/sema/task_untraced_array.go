package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// An array handed to a task whose provenance the checker does not hold (RV2-DEBT-365, R-e).
//
// A value typed `T[]` owns its buffer, views a dynamic array, or views a FIXED array, and its type
// does not say which (array_view_escape.go). The first two may go to a task: a dynamic array's
// view keeps its base alive through the runtime's registry. The third is a bare pointer into a
// frame slot. Where the checker holds the window's provenance -- a slice written in the call, a
// `let` bound to one -- the window pins the array it points into, or its capture is refused
// (reachingFixedViews, refuseFixedViewCapture). Everywhere else the provenance is gone: an
// assignment, a struct field, a helper that hands back its by-value formal, a slice of a slice, a
// method receiver. Following it through each of those is a flow analysis `T[]` cannot carry.
//
// What the checker can say is where a window into THIS frame could come from: a fixed array the
// function holds by value (typing refuses slicing or ranging an array that has no binding, SEM3023,
// so a temporary is not a source). A function that has none cannot
// hand its task a window into its own frame, whatever the value went through, and a window into a
// caller's storage is the caller's to answer, at its own call. A function that has one is refused,
// after its whole body has been walked, because a loop can carry a window from a later statement
// back to an earlier one. The one route no caller sees is a window over what a REFERENCE
// parameter points at, handed on in a returned task: that task is recorded as lent.
//
// A range cursor asks the same question of any array: `__range()` is registered with nothing, so a
// cursor into a dynamic array dangles as surely as a window into a fixed one (range_cursor_escape.go).

// Bits of arrayHoldings: what arrays a value of a type holds.
const (
	holdsDynamicArray uint8 = 1 << iota
	holdsFixedArray
	holdsRange
)

// untracedArrays is what the walk of one callable collected.
type untracedArrays struct {
	scope symbols.ScopeID
	uses  []untracedArrayUse
	seen  map[source.Span]struct{}
	lent  map[ast.ExprID]struct{}
	// cursors are the traced cursors the body being finished captured: they pin their arrays
	// once the body has an identity (pinCapturedCursors).
	cursors []spawnBorrowCapture
}

// untracedArrayUse is one place an array of unknown provenance reaches a task.
type untracedArrayUse struct {
	span source.Span
	to   string
	held uint8
}

// beginUntracedArrays opens the collection for the callable whose scope is current.
func (tc *typeChecker) beginUntracedArrays() *untracedArrays {
	outer := tc.untracedArrays
	tc.untracedArrays = &untracedArrays{scope: tc.currentScope()}
	return outer
}

// endUntracedArrays refuses what was collected, where the callable's own frame can be what the
// array windows or cursors, and hands the collection back to the callable outside.
func (tc *typeChecker) endUntracedArrays(outer *untracedArrays) {
	state := tc.untracedArrays
	tc.untracedArrays = outer
	if state == nil || len(state.uses) == 0 {
		return
	}
	fixed, anyArray := tc.frameArrayStorage(state.scope)
	for _, use := range state.uses {
		storage := symbols.NoSymbolID
		if use.held&holdsDynamicArray != 0 {
			storage = fixed
		}
		if !storage.IsValid() && use.held&holdsRange != 0 {
			storage = anyArray
		}
		if storage.IsValid() {
			tc.reportUntracedArray(use, storage)
		}
	}
}

// untracedArrayCaptures answers what a reaching position hands the task when it is not a window
// the checker traced. A cursor it traced pins the array it walks, as a window does. Anything else
// that can carry a window or a cursor is written down for the end of the walk; and a task over
// one, returned by a function whose reference parameters reach an array, may hold what they
// point at, which no caller's check can see.
func (tc *typeChecker) untracedArrayCaptures(callID, lender ast.ExprID) []spawnBorrowCapture {
	span := tc.exprSpan(lender)
	if base := tc.rangeCursorEscapeBase(lender); base.IsValid() {
		return []spawnBorrowCapture{{Place: Place{Base: base}, Kind: BorrowShared, Span: span}}
	}
	if node := tc.builder.Exprs.Get(tc.unwrapGroupExpr(lender)); node != nil && node.Kind == ast.ExprRangeLit {
		return nil
	}
	if tc.noteUntracedArray(tc.result.ExprTypes[lender], true, span, "this task") && tc.referenceParamsHoldArrays() {
		if tc.untracedArrays.lent == nil {
			tc.untracedArrays.lent = make(map[ast.ExprID]struct{})
		}
		tc.untracedArrays.lent[callID] = struct{}{}
	}
	return nil
}

// refuseUntracedCapture is the capture side, for a binding that is not a window the checker
// traced. A body has no identity a pin could be keyed by, so an array is written down for the end
// of the walk. A `let` whose value carries a reference -- `Option<&V>` out of `get_ref`, or an
// `own &T` (`let o = own xs[0];`), a reference under a move annotation -- names no loan
// (bindingBorrowForExpr asks isReferenceType of the surface type), so what it borrows is unknown:
// moved into a body, which can outlive the frame, it is refused (R-d's capture half, and the
// `own &T` form of RV2-DEBT-365). A plain `&T` let is scanSpawn's; a parameter's reference is the
// caller's.
func (tc *typeChecker) refuseUntracedCapture(symID symbols.SymbolID, span source.Span, body string) bool {
	ty := tc.bindingType(symID)
	sym := tc.symbolFromID(symID)
	_, carries := tc.carriedReferenceType(tc.ownStripped(ty))
	if carries && sym != nil && sym.Kind == symbols.SymbolLet && !tc.isReferenceType(ty) {
		msg := fmt.Sprintf("cannot capture '%s' into this %s: its value carries a reference whose source this checker does not know, and the body is a task that can outlive what it points at", tc.captureName(symID), body)
		if tc.reporter == nil {
			tc.report(diag.SemaBorrowThreadEscape, span, "%s", msg)
			return true
		}
		if b := diag.ReportError(tc.reporter, diag.SemaBorrowThreadEscape, span, msg); b != nil {
			b.WithHelp(span, "read what it refers to before the body starts, and capture that value")
			b.Emit()
		}
		return true
	}
	if base, traced := tc.rangeCursorBindingBase[symID]; traced && base.IsValid() && tc.untracedArrays != nil {
		tc.untracedArrays.cursors = append(tc.untracedArrays.cursors, spawnBorrowCapture{Place: Place{Base: base}, Kind: BorrowShared, Span: span})
		return false
	}
	tc.noteUntracedArray(ty, false, span, "this "+body)
	return false
}

// pinCapturedCursors gives an `async` or `blocking` body that captured a traced cursor what a
// task made by a call gets for a traced cursor argument: an identity and a pin on the array the
// cursor walks. `let job = blocking { for v in c { … } }; job.await()` then releases it, and a
// body handed out of the frame is refused by the return edge like any pinned task. A body that is
// a spawn's operand hands the pin to the spawn.
func (tc *typeChecker) pinCapturedCursors(id ast.ExprID, span source.Span) {
	state := tc.untracedArrays
	if state == nil || len(state.cursors) == 0 {
		return
	}
	captures := state.cursors
	state.cursors = nil
	if id == tc.spawnOperand {
		tc.spawnBorrowCaptures = append(tc.spawnBorrowCaptures, captures...)
		return
	}
	if tc.taskTracker == nil {
		return
	}
	taskID := tc.taskTracker.NoteCallTask(id, span, tc.currentScope(), tc.asyncBlockDepth > 0)
	tc.openTaskBorrowPins(taskID, captures)
}

// noteUntracedArray writes down a value of this type reaching a task where its provenance is not
// held, when the value can carry a window or a cursor at all, and reports whether it can.
func (tc *typeChecker) noteUntracedArray(ty types.TypeID, throughRefs bool, span source.Span, to string) bool {
	state := tc.untracedArrays
	if state == nil {
		return false
	}
	held := tc.arrayHoldings(ty, throughRefs, nil)
	if held&(holdsDynamicArray|holdsRange) == 0 {
		return false
	}
	if _, dup := state.seen[span]; !dup {
		if state.seen == nil {
			state.seen = make(map[source.Span]struct{})
		}
		state.seen[span] = struct{}{}
		state.uses = append(state.uses, untracedArrayUse{span: span, to: to, held: held})
	}
	return true
}

// untracedArrayLent reports that a returned call handed its task an array of unknown provenance
// while a reference parameter of this function reaches an array: the task is not built from
// owned values only.
func (tc *typeChecker) untracedArrayLent(call ast.ExprID) bool {
	if tc.untracedArrays == nil {
		return false
	}
	_, lent := tc.untracedArrays.lent[call]
	return lent
}

// referenceParamsHoldArrays reports that a reference parameter of the callable being walked
// reaches an array.
func (tc *typeChecker) referenceParamsHoldArrays() bool {
	for _, param := range tc.currentFnParams() {
		if ty := tc.bindingType(param); tc.isReferenceType(ty) && tc.arrayHoldings(ty, true, nil) != 0 {
			return true
		}
	}
	return false
}

// frameArrayStorage names a binding of the callable's own frame -- a local, or a parameter taken
// by value -- that holds a fixed array, and one that holds any array, over every scope of the
// callable in declaration order. What a reference parameter points at is the caller's.
func (tc *typeChecker) frameArrayStorage(scope symbols.ScopeID) (fixed, anyArray symbols.SymbolID) {
	if tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Scopes == nil {
		return symbols.NoSymbolID, symbols.NoSymbolID
	}
	pending := []symbols.ScopeID{scope}
	for len(pending) > 0 && !fixed.IsValid() {
		data := tc.symbols.Table.Scopes.Get(pending[0])
		pending = pending[1:]
		if data == nil {
			continue
		}
		pending = append(pending, data.Children...)
		for _, symID := range data.Symbols {
			if !tc.isFrameLocalStorage(symID) {
				continue
			}
			held := tc.arrayHoldings(tc.bindingType(symID), false, nil)
			if held&holdsFixedArray != 0 && !fixed.IsValid() {
				fixed = symID
			}
			if held&(holdsDynamicArray|holdsFixedArray) != 0 && !anyArray.IsValid() {
				anyArray = symID
			}
		}
	}
	return fixed, anyArray
}

// arrayHoldings says what arrays a value of this type holds: a dynamic array, which may be a
// window into a fixed one; a fixed array; a range, which may be a cursor. A task, a channel and a
// `far` value are handles and are not looked into: what went into one left by a send. throughRefs
// says whether what a reference points at counts.
func (tc *typeChecker) arrayHoldings(id types.TypeID, throughRefs bool, seen map[types.TypeID]struct{}) uint8 {
	id = tc.resolveAlias(id)
	if id == types.NoTypeID || tc.types == nil || tc.isTaskType(id) || tc.isChannelType(id) {
		return 0
	}
	if _, visited := seen[id]; visited {
		return 0
	}
	if seen == nil {
		seen = make(map[types.TypeID]struct{}, 4)
	}
	seen[id] = struct{}{}
	tt, found := tc.types.Lookup(id)
	if !found {
		return 0
	}
	switch tt.Kind {
	case types.KindReference, types.KindPointer:
		if !throughRefs {
			return 0
		}
		return tc.arrayHoldings(tt.Elem, throughRefs, seen)
	case types.KindOwn:
		return tc.arrayHoldings(tt.Elem, throughRefs, seen)
	case types.KindFar:
		return 0
	}
	if elem, _, fixed, isArray := tc.arrayInfo(id); isArray {
		held := holdsDynamicArray
		if fixed {
			held = holdsFixedArray
		}
		return held | tc.arrayHoldings(elem, throughRefs, seen)
	}
	if _, isRange := tc.rangePayload(id); isRange {
		return holdsRange
	}
	var held uint8
	switch tt.Kind {
	case types.KindStruct:
		if info, okStruct := tc.types.StructInfo(id); okStruct && info != nil {
			for i := range info.Fields {
				held |= tc.arrayHoldings(info.Fields[i].Type, throughRefs, seen)
			}
			for _, arg := range info.TypeArgs {
				held |= tc.arrayHoldings(arg, throughRefs, seen)
			}
		}
	case types.KindUnion:
		if info, okUnion := tc.types.UnionInfo(id); okUnion && info != nil {
			for i := range info.Members {
				held |= tc.arrayHoldings(info.Members[i].Type, throughRefs, seen)
				for _, arg := range info.Members[i].TagArgs {
					held |= tc.arrayHoldings(arg, throughRefs, seen)
				}
			}
		}
	case types.KindTuple:
		if info, okTuple := tc.types.TupleInfo(id); okTuple && info != nil {
			for _, elem := range info.Elems {
				held |= tc.arrayHoldings(elem, throughRefs, seen)
			}
		}
	}
	return held
}

// reportUntracedArray words one refusal: an array may be a slice of a fixed array of this frame,
// a range may be a cursor over an array of it. It names that array.
func (tc *typeChecker) reportUntracedArray(use untracedArrayUse, storage symbols.SymbolID) {
	code, what, into := diag.SemaFixedArrayViewEscapes, "an array", "a slice of"
	where, help := "a fixed array of this call frame",
		"slice the fixed array where the task gets it -- `f(a[[0..2]])`, or `let v = a[[0..2]];` -- so the checker can follow it, or hand the task an array of its own with a copy of each element it needs"
	if use.held&holdsDynamicArray == 0 {
		code, what, into = diag.SemaRangeCursorEscapes, "a range", "a cursor over"
		where, help = "an array of this call frame",
			"take the range where the task gets it -- `f(xs.__range())`, or `let c = xs.__range();` -- so the checker can follow it, or hand the task an array of its own"
	}
	sym := tc.symbolFromID(storage)
	if sym != nil {
		where = fmt.Sprintf("'%s', which lives in this call frame", tc.lookupName(sym.Name))
	}
	msg := fmt.Sprintf("cannot hand %s %s whose source this checker cannot trace: it may be %s %s, and the task can outlive the frame", use.to, what, into, where)
	if tc.reporter == nil {
		tc.report(code, use.span, "%s", msg)
		return
	}
	b := diag.ReportError(tc.reporter, code, use.span, msg)
	if b == nil {
		return
	}
	if sym != nil && sym.Span != (source.Span{}) {
		b.WithNote(sym.Span, fmt.Sprintf("'%s' holds an array by value: its elements are this frame's storage, and neither a slice nor a cursor keeps them alive", tc.lookupName(sym.Name)))
	}
	b.WithHelp(use.span, help)
	b.Emit()
}

// refuseCapturedCursorEscape is SEM3139 for an `async` or `blocking` body handed back while it holds
// the pin pinCapturedCursors opened on the array a captured cursor walks. The body is not a call, so
// the argument scan of checkTaskBorrowEscapeOnReturn has nothing to read, and the return edge sets
// the pins of the task it names aside for that scan to judge: without this the pin reaches no
// refusal at all.
func (tc *typeChecker) refuseCapturedCursorEscape(inner, spawnExpr ast.ExprID, span, cloneSpan source.Span) {
	node := tc.builder.Exprs.Get(spawnExpr)
	if node == nil || (node.Kind != ast.ExprAsync && node.Kind != ast.ExprBlocking) {
		return
	}
	if base, at := tc.taskPinnedFrameLocal(inner); base.IsValid() && tc.symbolFromID(base) != nil {
		tc.reportTaskBorrowEscape(span, base, at, cloneSpan)
	}
}
