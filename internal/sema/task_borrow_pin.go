package sema

import (
	"fmt"
	"sort"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
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

// closeLoopFlow ends a loop body's flow against the state before the loop.
//
// Two rules, and they are different because the two lattices are: a value moved
// in the body would be used moved on the next iteration, which is refused
// outright, while a join written in the body releases nothing AFTER the loop
// because the zero-iteration path is a reachable predecessor of whatever
// follows -- so the exit state is a union.
//
// The union is not enough on its own, and that gap was a real hole. The body is
// walked ONCE, with the state at loop entry, so a pin opened late in the body is
// invisible to a use earlier in it: the next iteration's write is checked
// against a pin set that does not yet contain the previous iteration's child.
// Moved places never needed a second walk because "moved" is sticky -- it has no
// kill -- and because the back edge is where they REFUSE rather than merge. A
// pin has a kill, so the mirror argument does not transfer, and the honest fix
// in the same style is to refuse the shape rather than iterate to a fixpoint: a
// child spawned inside the body that is still running at the back edge would
// race the next turn of the same body.
func (tc *typeChecker) closeLoopFlow(before flowSnapshot, loopLabel string) {
	tc.rejectLoopBackEdgeMoves(before.moved, loopLabel)
	tc.rejectLoopBackEdgePins(loopLabel)
	tc.taskBorrowPins = mergeTaskBorrowPins(tc.taskBorrowPins, before.pins)
}

// currentLoopDepth is the enclosing loop nesting, borrowed from the drop
// machinery's own marks rather than counted a second time.
func (tc *typeChecker) currentLoopDepth() int {
	return len(tc.loopDropMarks)
}

// rejectLoopBackEdgePins refuses a pin opened in the loop body that reaches the
// back edge. The body's own depth is still current when this runs.
func (tc *typeChecker) rejectLoopBackEdgePins(loopLabel string) {
	tc.refuseLivePinsAtEdge(tc.currentLoopDepth(), func(label string) string {
		return fmt.Sprintf(
			"a task spawned in this %s still borrows %s at the end of the body; the next iteration would run beside it",
			loopLabel, label)
	})
}

// refuseLivePinsAtAbruptExit refuses a pin opened at or below the given depth
// that is live where control leaves normally-sequenced code. `break` and
// `continue` are exactly the edges the walker does not carry state along -- it
// keeps walking the enclosing block linearly -- so a join written after one of
// them releases a pin on a path that never reached it.
func (tc *typeChecker) refuseLivePinsAtAbruptExit(depth int, what string) {
	tc.refuseLivePinsAtEdge(depth, func(label string) string {
		return fmt.Sprintf("a spawned task still borrows %s at this %s", label, what)
	})
}

func (tc *typeChecker) refuseLivePinsAtEdge(depth int, message func(label string) string) {
	if len(tc.taskBorrowPins) == 0 || tc.reporter == nil {
		return
	}
	for _, key := range sortedTaskBorrowPinKeys(tc.taskBorrowPins) {
		pin := tc.taskBorrowPins[key]
		if pin.LoopDepth < depth {
			continue
		}
		label := tc.placeLabel(key.Place)
		builder := diag.ReportError(tc.reporter, diag.SemaBorrowThreadEscape, pin.Span, message(label))
		if builder == nil {
			continue
		}
		builder.WithHelp(pin.Span, "join the task before control leaves here")
		builder.Emit()
		delete(tc.taskBorrowPins, key)
	}
}

// sortedTaskBorrowPinKeys orders the map so a file with several stranded pins
// reports them in a stable order rather than in Go's map order.
func sortedTaskBorrowPinKeys(pins map[taskBorrowPinKey]taskBorrowPin) []taskBorrowPinKey {
	keys := make([]taskBorrowPinKey, 0, len(pins))
	for key := range pins {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Task != keys[j].Task {
			return keys[i].Task < keys[j].Task
		}
		return pins[keys[i]].Span.Start < pins[keys[j]].Span.Start
	})
	return keys
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
	// LoopDepth is the loop nesting the spawn sat at. A pin opened INSIDE a loop
	// body is the one that may not survive to the back edge; one opened outside
	// and merely carried through the loop is sound and stays legal.
	LoopDepth int
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
			Borrow:    capture.Borrow,
			Kind:      capture.Kind,
			Span:      capture.Span,
			LoopDepth: tc.currentLoopDepth(),
		}
		tc.recordStableActivationPlace(capture.Place)
	}
}

// ActivationKey names ONE task activation. A callable has one of its own, and
// every `async { }` / `blocking { }` block inside it has another, because a
// block is a separate activation with a separate frame. A zero Block is the
// callable's own activation.
//
// Filing a block's local under its host callable is not a naming nicety. A
// lowering that trusted it would promote a field in the HOST's frame and leave
// the block's real local a per-poll `alloca` -- exactly the state the storage
// model calls forbidden, and it would do so silently, because both answers
// name a real binding.
//
// EXACTLY ONE half is set, and that is what makes the key resolvable by the
// lowering that will read it. A callable's activation is named by its symbol; a
// block's is named by its SPAN and carries no symbol at all, because the
// function a block lowers to carries none either: `lowerSyntheticFunc` builds
// `__async_block$N` with `Sym: symbols.NoSymbolID` and `Span: e.Span`. Storing
// the HOST's symbol beside the span would read well here and be unreconstructible
// there, so the key mirrors what the reader actually holds.
//
// The block half is a span rather than an expression id for the same reason one
// step earlier: `hir.Expr` carries kind, type, span and data and no AST id, so
// an id dies at HIR while a span reaches `mir.Func.Span`. A span carries its
// FileID, so it is unique across the program.
type ActivationKey struct {
	Fn    symbols.SymbolID
	Block source.Span
}

// IsBlock reports whether the key names an `async`/`blocking` block's own
// activation rather than a callable's.
func (k ActivationKey) IsBlock() bool { return k.Block != (source.Span{}) }

// currentActivation is the activation whose frame the checker is inside now.
// Blocks nest, so the innermost one wins, and it answers for itself rather than
// for the callable it is written inside.
func (tc *typeChecker) currentActivation() ActivationKey {
	if n := len(tc.activationBlocks); n > 0 {
		return ActivationKey{Block: tc.activationBlocks[n-1]}
	}
	return ActivationKey{Fn: tc.currentFnSym()}
}

// recordStableActivationPlace names a place the enclosing activation must keep
// at a fixed address. It is recorded at the spawn, which is the only point that
// knows both the capture set and the activation it was taken from -- an
// `async fn`'s frame, a block's own frame, or the synthetic root activation of
// an `@entrypoint`, which needs promoted places on exactly the same terms.
func (tc *typeChecker) recordStableActivationPlace(place Place) {
	if tc.result == nil || !place.IsValid() || !place.Base.IsValid() {
		return
	}
	owner := tc.currentActivation()
	if !owner.Fn.IsValid() && !owner.IsBlock() {
		return
	}
	for _, existing := range tc.result.StableActivationPlaces[owner] {
		if existing == place.Base {
			return
		}
	}
	if tc.result.StableActivationPlaces == nil {
		tc.result.StableActivationPlaces = make(map[ActivationKey][]symbols.SymbolID)
	}
	tc.result.StableActivationPlaces[owner] = append(tc.result.StableActivationPlaces[owner], place.Base)
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

// noteTaskContainerHoldsTask records the child whose handle was just pushed into
// a container. A handle inside a container cannot be resolved back to its task
// by name, so the container carries the identity and its PROVEN drain answers
// for the pin instead.
func (tc *typeChecker) noteTaskContainerHoldsTask(place Place, value ast.ExprID) {
	if !place.IsValid() || tc.taskContainers == nil || tc.taskTracker == nil {
		return
	}
	info := tc.taskContainers[place]
	if info == nil {
		return
	}
	id := tc.taskIDForAwaitTarget(value, tc.symbolForExpr(tc.unwrapGroupExpr(value)))
	if id == 0 {
		return
	}
	for _, existing := range info.Tasks {
		if existing == id {
			return
		}
	}
	info.Tasks = append(info.Tasks, id)
}

// releaseTaskBorrowPinsDrained answers for every child a drained container held.
// Draining is exactly the construct that makes completion definite for all of
// them, and without it a borrow-capturing fan-out could never be released --
// no join the checker can see names a handle popped from a container.
func (tc *typeChecker) releaseTaskBorrowPinsDrained(info *taskContainerInfo) {
	if info == nil {
		return
	}
	for _, id := range info.Tasks {
		tc.releaseTaskBorrowPins(id)
	}
	info.Tasks = nil
}

// resetTaskBorrowPinsForCallable drops pins left by the previous callable. A pin
// cannot legitimately span two of them -- a place is keyed by its binding symbol,
// so nothing here can match another callable's -- and leaving stranded pins
// behind makes every later write pay for them on a path walked once per
// assignment. Clearing cannot change an answer and bounds the scan.
func (tc *typeChecker) resetTaskBorrowPinsForCallable() {
	tc.taskBorrowPins = make(map[taskBorrowPinKey]taskBorrowPin)
}

// refuseLivePinsAtReturn is the return edge's form of refuseLivePinsAtAbruptExit.
// It steps aside when the return HANDS THE TASK BACK, because SEM3139 already
// answers that and with the better sentence -- it names the binding that dies and
// why the caller cannot fix it. Two messages for one defect is worse than one.
func (tc *typeChecker) refuseLivePinsAtReturn(valueType types.TypeID) {
	if tc.isTaskType(valueType) {
		return
	}
	tc.refuseLivePinsAtAbruptExit(0, "return")
}

// taskBorrowPinFor reports the pin covering a place, if any child still holds it.
// Overlap follows the borrow table's own place-overlap rule, so pinning a
// container also pins the field a later statement writes through.
//
// The winner is chosen, not taken from map order. Several children may pin one
// place, and the pins differ in what they make the diagnostic SAY -- the kind
// picks the message and the span picks where the note points -- so ranging the
// map and keeping the first hit made the compiler print two different messages
// for one file depending on nothing. Measured at 37 of 40 runs against 3 of 40.
// The order is: an exact place match beats an overlap, an exclusive pin beats a
// shared one because it is the stronger claim, and the earliest span breaks the
// remaining tie so the note points at the first child that took the place.
func (tc *typeChecker) taskBorrowPinFor(place Place) (taskBorrowPin, bool) {
	if len(tc.taskBorrowPins) == 0 || !place.IsValid() {
		return taskBorrowPin{}, false
	}
	var (
		best      taskBorrowPin
		bestExact bool
		found     bool
	)
	for key, pin := range tc.taskBorrowPins {
		exact := key.Place == place
		if !exact && (tc.borrow == nil || !tc.borrow.placesOverlap(key.Place, place)) {
			continue
		}
		if !found || taskBorrowPinOutranks(exact, pin, bestExact, best) {
			best, bestExact, found = pin, exact, true
		}
	}
	return best, found
}

// taskBorrowPinOutranks is the total order taskBorrowPinFor picks its winner by.
func taskBorrowPinOutranks(exact bool, pin taskBorrowPin, bestExact bool, best taskBorrowPin) bool {
	if exact != bestExact {
		return exact
	}
	if (pin.Kind == BorrowMut) != (best.Kind == BorrowMut) {
		return pin.Kind == BorrowMut
	}
	if pin.Span.Start != best.Span.Start {
		return pin.Span.Start < best.Span.Start
	}
	return pin.Borrow < best.Borrow
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

// refuseBorrowOfPinnedPlace answers the ACQUISITION of a new borrow against the
// children already holding one, and reports whether it refused.
//
// This is the third operation the pin guards, and leaving it out was the hole
// that mattered most: writes and moves were checked while `&mut v` was not, so
// `spawn reader(&v); spawn writer(&mut v)` -- one child reading and one child
// writing the same place, concurrently -- was admitted. The rule is the borrow
// table's own, applied to a borrow whose lexical record has ended: an exclusive
// pin refuses any new borrow, and a shared pin refuses only an exclusive one,
// because readers do not conflict with each other.
func (tc *typeChecker) refuseBorrowOfPinnedPlace(place Place, span source.Span, kind BorrowKind) bool {
	pin, pinned := tc.taskBorrowPinFor(place)
	if !pinned || (kind != BorrowMut && pin.Kind != BorrowMut) {
		return false
	}
	if tc.reporter == nil {
		return true
	}
	label := tc.placeLabel(place)
	var msg string
	switch {
	case pin.Kind == BorrowMut && kind == BorrowMut:
		msg = fmt.Sprintf("cannot take mutable borrow of %s while another mutable borrow is active", label)
	case pin.Kind == BorrowMut:
		msg = fmt.Sprintf("cannot take shared borrow of %s while an exclusive borrow is active", label)
	default:
		msg = fmt.Sprintf("cannot take mutable borrow of %s while a shared borrow is active", label)
	}
	builder := diag.ReportError(tc.reporter, diag.SemaBorrowConflict, span, msg)
	if builder == nil {
		return true
	}
	builder.WithNote(pin.Span,
		fmt.Sprintf("a spawned task captured this borrow of %s and is not joined on every path to here", label))
	builder.WithHelp(pin.Span,
		"join the task before this line, on every path that can reach it")
	builder.Emit()
	return true
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
			return tc.taskTracker.TaskIdentity(id)
		}
	}
	if binding.IsValid() {
		return tc.taskTracker.TaskIdentity(tc.taskTracker.TaskIDForBinding(binding))
	}
	return 0
}
