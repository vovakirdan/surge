package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// A child task that captured a borrow of its parent keeps reading that place for
// as long as it runs, so the parent's borrow has to outlive the borrow's own
// lexical region: it ends when the CHILD ends, not when the call that handed the
// reference over returns. `docs/RUNTIME_V2.md` section 9 states that lifetime.
// This file is the flow state that holds the compiler to it.
//
// The pin is deliberately not a flag on the task. A join releases it only when
// the child is DEFINITELY complete on every reachable incoming path, so after
//
//	let t = spawn child(&x);
//	if cond { t.await(); }
//	mutate(x);
//
// the pin is still active at the mutation, because the `cond == false` path left
// the child running. That makes it a may-be-live lattice with exactly the join
// rule ownership flow already uses for moved places:
//
//	ACTIVE   + ACTIVE   -> ACTIVE
//	ACTIVE   + RELEASED -> ACTIVE
//	RELEASED + RELEASED -> RELEASED
//
// which is the UNION of the two branch states. mergeTaskBorrowPins is therefore
// the same shape as mergeMovedPlaces, and the two are snapshotted, restored and
// merged at the same join points; see move_tracking.go for the other half of the
// pair. A global mutation performed wherever an await is written is refused as a
// mechanism, because it would release the pin for the paths that await never ran
// on.
//
// The handle's move state and the referent's pin state are different facts and do
// not share a slot. A task handle may be consumed on one path while the referent
// stays borrowed, because referent safety is decided by definite completion and
// not by the syntactic presence of a join.

// flowSnapshot bundles the two per-path lattices a branch has to carry. They are
// always snapshotted, restored and merged together -- moved places by union
// because a value given away on any path may not be used after the join, pins by
// union because a child still running on any path may not have its referent
// disturbed after it. Keeping them in one value is what stops the next join point
// from remembering one lattice and forgetting the other.
type flowSnapshot struct {
	moved map[Place]source.Span
	pins  map[taskBorrowPinKey]taskBorrowPin
}

func (tc *typeChecker) snapshotFlow() flowSnapshot {
	return flowSnapshot{moved: tc.snapshotMovedPlaces(), pins: tc.snapshotTaskBorrowPins()}
}

func (tc *typeChecker) restoreFlow(snapshot flowSnapshot) {
	tc.restoreMovedPlaces(snapshot.moved)
	tc.restoreTaskBorrowPins(snapshot.pins)
}

func mergeFlow(a, b flowSnapshot) flowSnapshot {
	return flowSnapshot{
		moved: mergeMovedPlaces(a.moved, b.moved),
		pins:  mergeTaskBorrowPins(a.pins, b.pins),
	}
}

// closeLoopFlow ends a loop body's flow against the state before the loop. The
// two halves are different rules for the same reason: a value moved in the body
// would be used moved on the next iteration, which is refused outright, while a
// join written in the body releases nothing after the loop because the
// zero-iteration path is a reachable predecessor of whatever follows.
func (tc *typeChecker) closeLoopFlow(before flowSnapshot, loopLabel string) {
	tc.rejectLoopBackEdgeMoves(before.moved, loopLabel)
	tc.taskBorrowPins = mergeTaskBorrowPins(tc.taskBorrowPins, before.pins)
}

// taskBorrowPinKey names one pinned place for one child task. A place borrowed by
// two children is two pins, so one child's completion cannot release the other's.
type taskBorrowPinKey struct {
	Task  uint32
	Place Place
}

// taskBorrowPin remembers where the pin came from. It carries its own span and
// kind rather than reading them back through Borrow, because the underlying
// borrow expires with its lexical scope while the pin does not, and a diagnostic
// must still be able to point at the spawn that took the reference.
type taskBorrowPin struct {
	Borrow BorrowID
	Kind   BorrowKind
	Span   source.Span
}

// spawnBorrowCapture is one borrowed place found in a spawn's operand.
type spawnBorrowCapture struct {
	Place  Place
	Borrow BorrowID
	Kind   BorrowKind
	Span   source.Span
}

func (tc *typeChecker) snapshotTaskBorrowPins() map[taskBorrowPinKey]taskBorrowPin {
	out := make(map[taskBorrowPinKey]taskBorrowPin, len(tc.taskBorrowPins))
	for key, value := range tc.taskBorrowPins {
		out[key] = value
	}
	return out
}

func (tc *typeChecker) restoreTaskBorrowPins(snapshot map[taskBorrowPinKey]taskBorrowPin) {
	tc.taskBorrowPins = make(map[taskBorrowPinKey]taskBorrowPin, len(snapshot))
	for key, value := range snapshot {
		tc.taskBorrowPins[key] = value
	}
}

// mergeTaskBorrowPins joins two branch states by UNION: a pin active on any
// reachable predecessor is active after the join, because a later use has to be
// rejected if any path left the child running. The intersection — "released on
// every path" — is the release condition, and taking the union here is what
// computes it: a pin disappears only when every predecessor dropped it.
func mergeTaskBorrowPins(a, b map[taskBorrowPinKey]taskBorrowPin) map[taskBorrowPinKey]taskBorrowPin {
	out := make(map[taskBorrowPinKey]taskBorrowPin, len(a)+len(b))
	for key, value := range a {
		out[key] = value
	}
	for key, value := range b {
		if _, ok := out[key]; ok {
			continue
		}
		out[key] = value
	}
	return out
}

// openTaskBorrowPins records one pin per borrowed place a spawn captured.
func (tc *typeChecker) openTaskBorrowPins(task uint32, captures []spawnBorrowCapture) {
	if task == 0 || len(captures) == 0 {
		return
	}
	if tc.taskBorrowPins == nil {
		tc.taskBorrowPins = make(map[taskBorrowPinKey]taskBorrowPin)
	}
	for _, capture := range captures {
		if !capture.Place.IsValid() {
			continue
		}
		key := taskBorrowPinKey{Task: task, Place: capture.Place}
		if _, ok := tc.taskBorrowPins[key]; ok {
			continue
		}
		tc.taskBorrowPins[key] = taskBorrowPin{
			Borrow: capture.Borrow,
			Kind:   capture.Kind,
			Span:   capture.Span,
		}
	}
}

// releaseTaskBorrowPins drops every pin one task holds, on THIS path only. A join
// reached on some paths and not others is undone at the next merge, which is
// where the may-be-live rule lives.
func (tc *typeChecker) releaseTaskBorrowPins(task uint32) {
	if task == 0 || len(tc.taskBorrowPins) == 0 {
		return
	}
	for key := range tc.taskBorrowPins {
		if key.Task == task {
			delete(tc.taskBorrowPins, key)
		}
	}
}

// taskBorrowPinFor reports the pin covering a place, if any child still holds it.
// Overlap follows the borrow table's own place-overlap rule, so pinning a
// container also pins the field a later statement writes through.
func (tc *typeChecker) taskBorrowPinFor(place Place) (taskBorrowPin, bool) {
	if len(tc.taskBorrowPins) == 0 || !place.IsValid() {
		return taskBorrowPin{}, false
	}
	var (
		found  taskBorrowPin
		anyHit bool
	)
	for key, pin := range tc.taskBorrowPins {
		if key.Place == place {
			return pin, true
		}
		if !anyHit && tc.borrow != nil && tc.borrow.placesOverlap(key.Place, place) {
			found = pin
			anyHit = true
		}
	}
	return found, anyHit
}

// reportTaskBorrowPinConflict answers with the SAME diagnostic the ordinary
// shared-borrow rule answers with, because to the reader this IS that rule: the
// place is borrowed and the borrow is still live. What the note adds is the part
// the span cannot show — the borrow is live because a child task is still running
// with it, and the join that would end it does not happen on every path here.
func (tc *typeChecker) reportTaskBorrowPinConflict(place Place, span source.Span, pin taskBorrowPin, verb string) {
	if tc.reporter == nil {
		return
	}
	label := tc.placeLabel(place)
	var msg string
	if pin.Kind == BorrowMut {
		msg = fmt.Sprintf("cannot %s %s while an exclusive borrow is active", verb, label)
	} else {
		msg = fmt.Sprintf("cannot %s %s while it is shared-borrowed", verb, label)
	}
	code := diag.SemaBorrowMutation
	if verb == "move" {
		code = diag.SemaBorrowMove
	}
	builder := diag.ReportError(tc.reporter, code, span, msg)
	if builder == nil {
		return
	}
	builder.WithNote(pin.Span,
		fmt.Sprintf("a spawned task captured this borrow of %s and is not joined on every path to here", label))
	builder.WithHelp(pin.Span,
		"join the task before this line, on every path that can reach it")
	builder.Emit()
}

// refuseWriteToHeldPlace answers a write to a place something still holds. Both
// holders live here rather than at the call site because to the writer they are
// one question -- "is this place still spoken for" -- and only the answer's
// provenance differs: the borrow table knows about a borrow whose lexical region
// has not ended, and the pin knows about one whose region ended at the call that
// handed the reference to a child still running.
func (tc *typeChecker) refuseWriteToHeldPlace(place Place, span source.Span, issue BorrowIssue) {
	if issue.Kind != BorrowIssueNone {
		tc.reportBorrowMutation(place, span, issue)
		return
	}
	if pin, pinned := tc.taskBorrowPinFor(place); pinned {
		tc.reportTaskBorrowPinConflict(place, span, pin, "mutate")
	}
}

// refuseMoveOfHeldPlace is the same question asked of a move, and reports
// whether the move was refused so the caller can stop. evSpan is the fallback
// the move path already carries for a read with no span of its own.
func (tc *typeChecker) refuseMoveOfHeldPlace(place Place, span, evSpan source.Span, issue BorrowIssue) bool {
	if span == (source.Span{}) {
		span = evSpan
	}
	if issue.Kind != BorrowIssueNone {
		tc.reportBorrowMove(place, span, issue)
		return true
	}
	if pin, pinned := tc.taskBorrowPinFor(place); pinned {
		tc.reportTaskBorrowPinConflict(place, span, pin, "move")
		return true
	}
	return false
}

// taskIDForAwaitTarget resolves the awaited expression to the task it joins, by
// the same two routes trackTaskAwait itself uses: the task-producing expression,
// or the binding that holds the handle.
func (tc *typeChecker) taskIDForAwaitTarget(targetExpr ast.ExprID, binding symbols.SymbolID) uint32 {
	if tc.taskTracker == nil {
		return 0
	}
	if targetExpr.IsValid() {
		if id := tc.taskTracker.TaskIDForExpr(targetExpr); id != 0 {
			return id
		}
	}
	if binding.IsValid() {
		return tc.taskTracker.TaskIDForBinding(binding)
	}
	return 0
}
