package sema

import (
	"fmt"
	"slices"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Drop obligations: the walker records, at every scope exit point, which
// droppable bindings are live (declared, not moved) so HIR lowering can
// synthesize the frees. Move tracking stays the single source of truth —
// the obligations are a projection of movedBindings at each exit point,
// never a re-derivation.

// dropScope mirrors one lexical scope on the walker's stack, holding the
// droppable bindings declared in it, in declaration order.
type dropScope struct {
	bindings []symbols.SymbolID
	// functionRoot marks the function's own scope (params live here);
	// the body block's end merges these so params drop on fallthrough.
	functionRoot bool
}

// isDroppableType reports whether scope exit must reclaim a value of this
// type. Droppability IS the OwnsHeap axis (`ownership_axes.go`); emission
// stays leaf-gated in the backend, and obligations are type-agnostic beyond
// this predicate.
func (tc *typeChecker) isDroppableType(id types.TypeID) bool {
	return tc.ownsHeap(id)
}

func (tc *typeChecker) isDroppableBinding(symID symbols.SymbolID) bool {
	if !symID.IsValid() {
		return false
	}
	return tc.isDroppableType(tc.bindingType(symID))
}

// dropObligationsSuppressed gates the recording paths for bodies that ship
// their state to a runtime with its own release discipline, where a
// synthesized drop would race that runtime's ownership.
//
// Crossing bodies used to be suppressed here too, on the authority of
// RV2-DEBT-034. That row CLOSED at the Epic 20 closeout: the crossing side now
// has compiled per-state drop functions with real ids at every publish site
// and drop-count censuses across the refusal, abandon, stale and cancel edges.
// The runtime's discipline covers the STATE and the edges into it — it never
// covered what the BODY allocates after unpacking, and with the row closed the
// suppression was reclaiming nothing and abandoning every droppable the body
// built and still held at its `ret`. Measured at 16 blocks over 8 crossings,
// valgrind, native backend.
//
// This says nothing about the moved-in CAPTURES, which are reclaimed by a site
// this epic could not identify — see RV2-DEBT-079. They do not leak; adding a
// body-side drop for them double-frees.
//
// Blocking bodies keep the suppression, and that is a parked question rather
// than a proven boundary: their release path is a separate shallow free on the
// pool side, unprobed here (RV2-DEBT-080).
func (tc *typeChecker) dropObligationsSuppressed() bool {
	return tc.blockingDepth > 0
}

func (tc *typeChecker) pushDropScope(functionRoot bool) {
	tc.dropScopes = append(tc.dropScopes, dropScope{functionRoot: functionRoot})
}

// paramTransfersOwnership reports whether passing a by-value argument of this
// type HANDED the value to the callee, making the callee responsible for
// dropping it.
//
// Ownership transfers on a move, not on a copy. A `string` param is owned
// because passing a string by value moves it and the caller's binding dies. A
// reference-counted scalar is Copy, so the caller keeps its binding AND its
// reference for the whole call — the parameter merely borrows, and dropping it
// in the callee would release a reference the callee never acquired.
//
// The exception is written as "reference-counted scalar", not "Copy", and the
// difference became load-bearing when value composites became droppable. A
// composite argument is CLONED at the call — the caller keeps its own value and
// the callee receives an independent one — so the callee genuinely owns what it
// was handed and must drop it. Excluding it as merely-Copy would abandon that
// clone on every call, silently, in a way no test of independence can see.
func (tc *typeChecker) paramTransfersOwnership(id types.TypeID) bool {
	if !tc.isDroppableType(id) {
		return false
	}
	if tc.types == nil {
		return true
	}
	return !tc.types.IsRefCountedScalar(tc.resolveAlias(id))
}

// registerDroppableParams registers a function's by-value owned params
// (including a by-value self) into the function-root drop scope: the callee
// owns them and drops them on every exit unless it moved them onward.
func (tc *typeChecker) registerDroppableParams(fn *ast.FnItem, scope symbols.ScopeID) {
	if tc.builder == nil || fn == nil {
		return
	}
	register := func(symID symbols.SymbolID) {
		if !symID.IsValid() || !tc.paramTransfersOwnership(tc.bindingType(symID)) {
			return
		}
		tc.registerDroppableBinding(symID)
	}
	register(tc.findSelfSymbol(fn, scope))
	for _, pid := range tc.builder.Items.GetFnParamIDs(fn) {
		param := tc.builder.Items.FnParam(pid)
		if param == nil || param.Name == source.NoStringID {
			continue
		}
		register(tc.symbolInScope(scope, param.Name, symbols.SymbolParam))
	}
}

func (tc *typeChecker) popDropScope() {
	if len(tc.dropScopes) == 0 {
		return
	}
	tc.dropScopes = tc.dropScopes[:len(tc.dropScopes)-1]
}

// markAliasedBinding records that a binding currently holds a handle its
// CONTAINER still owns, so it must never drop: its value came from a field,
// element or deref read that TOOK nothing, which leaves the container the only
// owner and makes a drop here a double free.
//
// The one projection read this must not cover is a partial move, where the
// place has left the container and this binding is its only owner — suppressing
// its drop there abandons the value. `partialMoveRead` is the predicate that
// separates them, and the two have to agree: a read they disagree about is
// either dropped twice or not at all.
func (tc *typeChecker) markAliasedBinding(symID symbols.SymbolID) {
	if !symID.IsValid() {
		return
	}
	if tc.aliasedBindings == nil {
		tc.aliasedBindings = make(map[symbols.SymbolID]struct{})
	}
	tc.aliasedBindings[symID] = struct{}{}
}

func (tc *typeChecker) clearAliasedBinding(symID symbols.SymbolID) {
	if !symID.IsValid() || tc.aliasedBindings == nil {
		return
	}
	delete(tc.aliasedBindings, symID)
}

func (tc *typeChecker) isAliasedBinding(symID symbols.SymbolID) bool {
	if tc.aliasedBindings == nil {
		return false
	}
	_, ok := tc.aliasedBindings[symID]
	return ok
}

// markNonOwningBinding records a binding that NEVER owns what it holds, for its
// whole life — as opposed to an aliased binding, which merely holds someone
// else's value until it is assigned a fresh one.
//
// The distinction is load-bearing and the alias set cannot carry it: assigning
// a fresh value to an ordinary binding legitimately makes it an owner again, so
// `recordReassignOldDrop` CLEARS the alias mark on the way past. A loop binding
// is not like that. `for it in xs` binds a word-wise COPY of the element — the
// container keeps ownership, the frame emits no drop for it — and assigning to
// the copy does not move the container's storage anywhere. A binding that had
// only the clearable mark would therefore be an owner again after its first
// assignment, and the second assignment in the same body would free the
// container's value.
func (tc *typeChecker) markNonOwningBinding(symID symbols.SymbolID) {
	if !symID.IsValid() {
		return
	}
	if tc.nonOwningBindings == nil {
		tc.nonOwningBindings = make(map[symbols.SymbolID]struct{})
	}
	tc.nonOwningBindings[symID] = struct{}{}
}

// isNonOwningBinding reports whether freeing through this binding would free
// storage that belongs to something else.
func (tc *typeChecker) isNonOwningBinding(symID symbols.SymbolID) bool {
	if tc.nonOwningBindings == nil {
		return false
	}
	_, ok := tc.nonOwningBindings[symID]
	return ok
}

// isProjectionRead reports whether the expression reads THROUGH a place
// (field access, element index, tuple index, deref): the resulting value
// is interior state another owner keeps.
func (tc *typeChecker) isProjectionRead(expr ast.ExprID) bool {
	if !expr.IsValid() {
		return false
	}
	desc, ok := tc.resolvePlace(expr)
	if !ok {
		return false
	}
	return len(desc.Segments) > 0
}

// projectionReadAliasesItsSource reports whether a binding taking this
// projection read must stay out of the drop obligations because the container
// still owns what it read.
//
// It does for the values that live INSIDE their container: a field of a struct,
// an element of an array. It does NOT for a reference-counted scalar, and that
// exception is what keeps sema in step with the lowering. Such a value is a
// COPY at the surface and a counted block underneath, so the read takes a
// reference of its own (`retainExtractedValue`, which every projection read
// emits) and the binding holding it is a real owner. Marked as an alias, the
// binding kept that reference and released nothing — `let f = b.value` on a
// `float` field leaked one block per evaluation.
func (tc *typeChecker) projectionReadAliasesItsSource(expr ast.ExprID, ty types.TypeID) bool {
	if !tc.isProjectionRead(expr) || tc.partialMoveRead(expr) {
		return false
	}
	if tc.types != nil && tc.types.IsRefCountedScalar(tc.resolveAlias(ty)) {
		return false
	}
	return true
}

// registerDroppableBinding adds a freshly declared binding to the current
// scope. Shadowing needs no special case: each shadow is its own symbol and
// registers separately, so a still-live shadowed value still drops.
func (tc *typeChecker) registerDroppableBinding(symID symbols.SymbolID) {
	if len(tc.dropScopes) == 0 || !tc.isDroppableBinding(symID) {
		return
	}
	top := &tc.dropScopes[len(tc.dropScopes)-1]
	// A method's self reaches here twice (findSelfSymbol AND the param
	// list); one binding drops once.
	for _, existing := range top.bindings {
		if existing == symID {
			return
		}
	}
	top.bindings = append(top.bindings, symID)
}

// liveDroppables returns the scope's unmoved droppables in reverse
// declaration order (drop order is reverse of construction).
func (tc *typeChecker) liveDroppables(scope *dropScope) []symbols.SymbolID {
	var out []symbols.SymbolID
	for i := len(scope.bindings) - 1; i >= 0; i-- {
		symID := scope.bindings[i]
		if tc.bindingMovedPlace(symID) {
			continue
		}
		if tc.isAliasedBinding(symID) {
			continue
		}
		out = append(out, symID)
	}
	return out
}

// recordScopeEndDrops captures the normal-exit obligations for the scope
// being closed, keyed by the closing statement. When the scope directly
// above is the function root, its live droppables (the by-value params)
// are appended — the body block's fallthrough is the function's exit.
func (tc *typeChecker) recordScopeEndDrops(id ast.StmtID) {
	if len(tc.dropScopes) == 0 || !id.IsValid() || tc.dropObligationsSuppressed() {
		return
	}
	top := &tc.dropScopes[len(tc.dropScopes)-1]
	drops := tc.liveDroppables(top)
	if len(tc.dropScopes) >= 2 {
		parent := &tc.dropScopes[len(tc.dropScopes)-2]
		if parent.functionRoot {
			drops = append(drops, tc.liveDroppables(parent)...)
		}
	}
	if len(drops) == 0 {
		return
	}
	if tc.result.ScopeEndDrops == nil {
		tc.result.ScopeEndDrops = make(map[ast.StmtID][]symbols.SymbolID)
	}
	tc.result.ScopeEndDrops[id] = drops
	tc.recordResidualDrops(DropSite{Stmt: id}, drops)
}

// recordEarlyExitDrops captures obligations for a return/ret/break/
// continue, keyed by that statement. toLoop limits collection to the
// scopes inside the innermost loop (break/continue); otherwise every
// scope out to the nearest function root is collected, innermost first.
// recordBlockExprEndDrops is recordScopeEndDrops for BLOCK EXPRESSIONS
// (compare/if arms, standalone value blocks), keyed by the expression:
// their locals live in the block's own scope and drop at its normal end.
func (tc *typeChecker) recordBlockExprEndDrops(id ast.ExprID) {
	if len(tc.dropScopes) == 0 || !id.IsValid() || tc.dropObligationsSuppressed() {
		return
	}
	top := &tc.dropScopes[len(tc.dropScopes)-1]
	drops := tc.liveDroppables(top)
	if len(drops) == 0 {
		return
	}
	if tc.result.BlockExprEndDrops == nil {
		tc.result.BlockExprEndDrops = make(map[ast.ExprID][]symbols.SymbolID)
	}
	tc.result.BlockExprEndDrops[id] = drops
	tc.recordResidualDrops(DropSite{Expr: id}, drops)
}

func (tc *typeChecker) recordEarlyExitDrops(id ast.StmtID, toLoop bool) {
	if !id.IsValid() || tc.dropObligationsSuppressed() {
		return
	}
	floor := 0
	if toLoop {
		if len(tc.loopDropMarks) == 0 {
			return
		}
		floor = tc.loopDropMarks[len(tc.loopDropMarks)-1]
	}
	var drops []symbols.SymbolID
	for i := len(tc.dropScopes) - 1; i >= floor; i-- {
		scope := &tc.dropScopes[i]
		if !toLoop && scope.functionRoot {
			drops = append(drops, tc.liveDroppables(scope)...)
			break
		}
		drops = append(drops, tc.liveDroppables(scope)...)
	}
	if len(drops) == 0 {
		return
	}
	if tc.result.EarlyExitDrops == nil {
		tc.result.EarlyExitDrops = make(map[ast.StmtID][]symbols.SymbolID)
	}
	tc.result.EarlyExitDrops[id] = drops
	tc.recordResidualDrops(DropSite{Stmt: id}, drops)
}

func (tc *typeChecker) enterLoopDropScope() {
	tc.loopDropMarks = append(tc.loopDropMarks, len(tc.dropScopes))
}

func (tc *typeChecker) leaveLoopDropScope() {
	if len(tc.loopDropMarks) == 0 {
		return
	}
	tc.loopDropMarks = tc.loopDropMarks[:len(tc.loopDropMarks)-1]
}

// bindingDeclaredAtOrAbove reports whether the binding registered in a
// drop scope at stack index >= floor (i.e. inside the region the floor
// marks, such as a loop body).
func (tc *typeChecker) bindingDeclaredAtOrAbove(symID symbols.SymbolID, floor int) bool {
	for i := len(tc.dropScopes) - 1; i >= floor && i >= 0; i-- {
		for _, b := range tc.dropScopes[i].bindings {
			if b == symID {
				return true
			}
		}
	}
	return false
}

// rejectLoopBackEdgeMoves diagnoses droppable bindings declared OUTSIDE
// a loop that were moved INSIDE its body: the second iteration would use
// (and later drop) a moved value. before is the moved snapshot taken
// before the body walk; the loop mark must still be on the stack.
func (tc *typeChecker) rejectLoopBackEdgeMoves(before map[Place]source.Span, loopLabel string) {
	if len(tc.loopDropMarks) == 0 {
		return
	}
	floor := tc.loopDropMarks[len(tc.loopDropMarks)-1]
	// One report per BINDING even though the moved-set is keyed by place:
	// several places can share a base, and the back edge is a property of the
	// binding the loop re-enters. The place is kept so the message can name
	// what actually went — `o.inner`, not just `o`.
	//
	// The kept place is the earliest move rather than whichever the map yields:
	// iteration order is random, so an arbitrary pick moves the diagnostic
	// between identical compiles once a base has more than one moved place.
	type loopMove struct {
		place Place
		span  source.Span
	}
	firstMove := make(map[symbols.SymbolID]loopMove)
	for place, span := range tc.movedPlaces {
		// A place already implied before the loop is not something the body
		// did. Covered rather than equal: the set collapses a field into its
		// container, so the entry seen now may be wider than the one recorded
		// in the snapshot.
		alreadyMoved := false
		for prior := range before {
			if placeCovers(prior, place) {
				alreadyMoved = true
				break
			}
		}
		if alreadyMoved {
			continue
		}
		prev, seen := firstMove[place.Base]
		if !seen || lessMovedCandidate(span, place, prev.span, prev.place) {
			firstMove[place.Base] = loopMove{place: place, span: span}
		}
	}
	for symID, move := range firstMove {
		span := move.span
		if !tc.isDroppableBinding(symID) {
			continue
		}
		// The body's scopes are already popped: a binding still present
		// on the stack was declared OUTSIDE the loop and is the one the
		// back-edge would re-use; body-locals are gone and stay valid.
		if !tc.bindingDeclaredAtOrAbove(symID, 0) || tc.bindingDeclaredAtOrAbove(symID, floor) {
			continue
		}
		name := tc.plainPlaceLabel(move.place)
		tc.report(diag.SemaUseAfterMove, span,
			"value '%s' is declared outside this %s but moved inside its body; the next iteration would use a moved value — move it outside the loop or recreate it each iteration",
			name, loopLabel)
	}
}

// oneSidedDroppables returns the droppable bindings moved in `other` but
// LIVE in `moved`, restricted to bindings still on the scope stack — the
// ones that must be dropped at the end of the `moved`-side arm so the
// value is reclaimed exactly once (per-arm drop synthesis, the friendly
// resolution of partial-path moves).
// recordIfArmDrops records the droppables to free at the end of one
// if-statement branch block (those moved in the sibling branch but live
// here), keyed by the branch's block statement.
func (tc *typeChecker) recordIfArmDrops(branch ast.StmtID, branchMoved, union map[Place]source.Span) {
	if !branch.IsValid() {
		return
	}
	drops, plans := tc.oneSidedObligations(branchMoved, union)
	if len(drops) == 0 {
		return
	}
	if tc.result.ArmDropsStmt == nil {
		tc.result.ArmDropsStmt = make(map[ast.StmtID][]symbols.SymbolID)
	}
	tc.result.ArmDropsStmt[branch] = drops
	tc.recordOneSidedDrops(DropSite{Stmt: branch}, plans)
}

// oneSidedObligations answers what this side of a branch still owes, given what
// the other side gave away. It returns the bindings to drop, and for a binding
// only PARTLY affected, the plan naming which places to drop.
//
// The condition is per PLACE, not per binding. A place given away on the other
// side and still held here has to be reclaimed here, because the join says it
// went and the exit will not reclaim it. A place given away on BOTH sides is
// nobody's obligation, and this is the case that made a partial move fatal:
// folding every moved place to its base and asking only whether the WHOLE
// binding had moved, a container with one field taken before the branch looked
// like a binding the other side had disposed of. Every arm then dropped it
// whole, on top of the residual drop at the exit — a double free on the native
// backend, a use-after-free in the VM, and both of them reachable from
// `compare` over a field taken out of a live value.
//
// The plan is empty for a binding whose WHOLE value is one-sided, which is the
// only shape that existed before places could move on their own; those keep
// dropping whole.
func (tc *typeChecker) oneSidedObligations(moved, other map[Place]source.Span) (owing []symbols.SymbolID, plans map[symbols.SymbolID][]DropStep) {
	// One entry per base: two places sharing a base owe one drop of that
	// binding, carrying a plan that names both.
	owed := make(map[symbols.SymbolID][]Place, len(other))
	for place := range other {
		if tc.placeCoveredBy(moved, place) {
			// Gone on this side too: neither side is holding it.
			continue
		}
		symID := place.Base
		if !tc.isDroppableBinding(symID) || !tc.bindingDeclaredAtOrAbove(symID, 0) {
			continue
		}
		if _, dup := owed[symID]; !dup {
			owing = append(owing, symID)
		}
		owed[symID] = append(owed[symID], place)
	}
	if len(owing) == 0 {
		return nil, nil
	}
	plans = make(map[symbols.SymbolID][]DropStep, len(owing))
	for symID, places := range owed {
		if steps := tc.placeDropSteps(places); len(steps) > 0 {
			plans[symID] = steps
		}
	}
	if len(plans) == 0 {
		plans = nil
	}
	return owing, plans
}

// placeCoveredBy reports whether a moved-set already accounts for this place,
// through the same covering relation a read is judged by: a container that went
// whole takes its fields with it.
func (tc *typeChecker) placeCoveredBy(set map[Place]source.Span, place Place) bool {
	if _, exact := set[place]; exact {
		return true
	}
	for existing := range set {
		if placeCovers(existing, place) {
			return true
		}
	}
	return false
}

// placeSegments decodes an interned place back into the projection steps that
// reach it.
func (tc *typeChecker) placeSegments(place Place) []PlaceSegment {
	if tc.borrow == nil {
		return nil
	}
	return tc.borrow.placeSegments(place)
}

// placeStepKey orders drop steps by the field names on their path, so the plan
// a branch records does not depend on map iteration.
func placeStepKey(step DropStep) string {
	var b strings.Builder
	for _, seg := range step.Path {
		fmt.Fprintf(&b, "%d:%d;", seg.Kind, seg.Name)
	}
	return b.String()
}

// placeDropSteps turns the places one arm owes into a drop plan, or nil when
// the obligation is the whole binding and an ordinary drop is right.
func (tc *typeChecker) placeDropSteps(places []Place) []DropStep {
	var steps []DropStep
	for _, place := range places {
		if place.Path == "" {
			// The whole binding is owed; a plan would only restate the
			// ordinary drop, and any place beside it is covered by it.
			return nil
		}
		segments := tc.placeSegments(place)
		if len(segments) == 0 {
			return nil
		}
		steps = append(steps, DropStep{Path: segments})
	}
	// Deepest first, then by path, so the order is a function of the program
	// rather than of map iteration.
	slices.SortFunc(steps, func(a, b DropStep) int {
		return strings.Compare(placeStepKey(a), placeStepKey(b))
	})
	return steps
}

func (tc *typeChecker) bindingName(symID symbols.SymbolID) string {
	name := "_"
	if sym := tc.symbolFromID(symID); sym != nil {
		if symName := tc.lookupName(sym.Name); symName != "" {
			name = symName
		}
	}
	return name
}

// recordReassignOldDrop captures the overwritten-value drop for a whole-
// binding assignment: recorded only when the target is still live after
// the RHS evaluated, so `x = f(x)` (the RHS moved x) suppresses it —
// move tracking is the source of truth for the suppression.
func (tc *typeChecker) recordReassignOldDrop(exprID ast.ExprID, symID symbols.SymbolID) {
	if !exprID.IsValid() || !tc.isDroppableBinding(symID) || tc.dropObligationsSuppressed() {
		return
	}
	// An aliased binding's current value belongs to its container.
	if tc.isAliasedBinding(symID) {
		return
	}
	// And a loop binding never owns what it holds, no matter how many times it
	// has been assigned - see markNonOwningBinding for why the alias mark alone
	// cannot answer this.
	if tc.isNonOwningBinding(symID) {
		return
	}
	if tc.bindingMovedPlace(symID) {
		return
	}
	if tc.result.ReassignOldDrops == nil {
		tc.result.ReassignOldDrops = make(map[ast.ExprID]symbols.SymbolID)
	}
	tc.result.ReassignOldDrops[exprID] = symID
}

// recordPlaceOldDrop captures the overwritten-value drop for an assignment INTO
// a place — a struct field, a field one level down, a tuple element, or a store
// through `&mut`. recordReassignOldDrop is its whole-binding sibling.
//
// Recording here is what makes the native lane free what it displaces. The VM
// already did, from inside its own store (`storeStorage` releases what it reads
// out of the cell), and that second release is the one thing this record must
// not collide with. It does not, because a projected drop RELEASES AND THEN
// CLEARS: `execDropProjected` zeroes the member, `emitInstrDrop` nulls the slot.
// By the time the store runs, what it would release a second time is gone.
//
// CALL THIS BEFORE revivePlace. Reviving deletes every moved place the target
// covers, and that set is the only thing able to answer the question the
// moved-place guard below asks.
func (tc *typeChecker) recordPlaceOldDrop(exprID, left, right ast.ExprID, desc placeDescriptor, target Place) {
	if !exprID.IsValid() || !left.IsValid() || tc.dropObligationsSuppressed() {
		return
	}
	// An element assignment is not this path's business. `xs[i] = v` is
	// rewritten into an `__index_set` call before the assignment's own data
	// exists, so a record made here could never reach a backend — and the
	// element release it would be asking for is already emitted at that store.
	// Recording it anyway would be an obligation that silently evaporates, or
	// a second release the day the rewrite moves.
	for _, seg := range desc.Segments {
		if seg.Kind == PlaceSegmentIndex {
			return
		}
	}
	placeType := tc.result.ExprTypes[left]
	if !tc.isDroppableType(placeType) {
		return
	}
	// The base holds a value that belongs to its container, so nothing under it
	// is this scope's to free: `let b = outer.item; b.name = ...` would release
	// a string `outer` still owns, and `for it in xs { it.name = ... }` would
	// release one the array still owns and then frees itself.
	if tc.isAliasedBinding(desc.Base) || tc.isNonOwningBinding(desc.Base) {
		return
	}
	// OVERLAP, not equality. Freeing this place frees everything under it, so a
	// member already given away would go a second time — `@drop o.inner;
	// o.inner = Inner{...}` is the exact program that refuted the blind
	// native-side release. Asking placeMoved instead would answer only for the
	// place named and miss both `o.inner.deep` below it and `o` above it.
	if _, _, moved := tc.movedPlaceCovering(target); moved {
		return
	}
	// A projection read hands this place a value its container still owns. The
	// OLD value is genuinely this place's to free, but telling the two apart
	// needs a place-shaped alias set and there is only a binding-shaped one.
	// Staying silent leaks exactly as much as today; freeing would free twice.
	if tc.projectionReadAliasesItsSource(right, placeType) {
		return
	}
	if tc.result.ReassignOldDrops == nil {
		tc.result.ReassignOldDrops = make(map[ast.ExprID]symbols.SymbolID)
	}
	tc.result.ReassignOldDrops[exprID] = desc.Base
}
