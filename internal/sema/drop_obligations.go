package sema

import (
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

// registerDroppableBinding adds a freshly declared binding to the current
// scope. Shadowing needs no special case: each shadow is its own symbol
// and registers separately, so a still-live shadowed value still drops.
// markAliasedBinding records that a binding currently holds a handle its
// CONTAINER still owns (its value came from a field/element/deref read —
// sema does not track partial moves yet, so the container keeps
// ownership and the alias must never drop: leak over double free).
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
		if _, moved := tc.movedBindings[symID]; moved {
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
func (tc *typeChecker) rejectLoopBackEdgeMoves(before map[symbols.SymbolID]source.Span, loopLabel string) {
	if len(tc.loopDropMarks) == 0 {
		return
	}
	floor := tc.loopDropMarks[len(tc.loopDropMarks)-1]
	for symID, span := range tc.movedBindings {
		if _, was := before[symID]; was {
			continue
		}
		if !tc.isDroppableBinding(symID) {
			continue
		}
		// The body's scopes are already popped: a binding still present
		// on the stack was declared OUTSIDE the loop and is the one the
		// back-edge would re-use; body-locals are gone and stay valid.
		if !tc.bindingDeclaredAtOrAbove(symID, 0) || tc.bindingDeclaredAtOrAbove(symID, floor) {
			continue
		}
		name := tc.bindingName(symID)
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
func (tc *typeChecker) recordIfArmDrops(branch ast.StmtID, branchMoved, union map[symbols.SymbolID]source.Span) {
	if !branch.IsValid() {
		return
	}
	drops := tc.oneSidedDroppables(branchMoved, union)
	if len(drops) == 0 {
		return
	}
	if tc.result.ArmDropsStmt == nil {
		tc.result.ArmDropsStmt = make(map[ast.StmtID][]symbols.SymbolID)
	}
	tc.result.ArmDropsStmt[branch] = drops
}

func (tc *typeChecker) oneSidedDroppables(moved, other map[symbols.SymbolID]source.Span) []symbols.SymbolID {
	var out []symbols.SymbolID
	for symID := range other {
		if _, both := moved[symID]; both {
			continue
		}
		if !tc.isDroppableBinding(symID) {
			continue
		}
		if !tc.bindingDeclaredAtOrAbove(symID, 0) {
			continue
		}
		out = append(out, symID)
	}
	return out
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
	if _, moved := tc.movedBindings[symID]; moved {
		return
	}
	if tc.result.ReassignOldDrops == nil {
		tc.result.ReassignOldDrops = make(map[ast.ExprID]symbols.SymbolID)
	}
	tc.result.ReassignOldDrops[exprID] = symID
}
