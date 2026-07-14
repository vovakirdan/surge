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

// isDroppableType reports whether a value of this type owns heap state
// that scope exit must reclaim: non-copy, not a reference, not a raw
// pointer. Emission stays leaf-gated in the backend; obligations are
// type-agnostic beyond this predicate.
func (tc *typeChecker) isDroppableType(id types.TypeID) bool {
	if id == types.NoTypeID || tc.types == nil {
		return false
	}
	if tc.isCopyType(id) {
		return false
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindReference, types.KindPointer:
		return false
	}
	// Generic-typed bindings are excluded from vertical-1 synthesis:
	// call-arg moves are not observed through generic calls yet, so a
	// generic callee cannot tell an owned param from a moved-on one —
	// synthesizing there double-frees (push -> array_push -> intrinsic
	// would drop the same handle in two callee scopes). Concrete types
	// instantiated FROM generics drop at their concrete call sites; the
	// generic-body gap closes with the recursive-glue task.
	return !types.ContainsGenericParam(tc.types, resolved)
}

func (tc *typeChecker) isDroppableBinding(symID symbols.SymbolID) bool {
	if !symID.IsValid() {
		return false
	}
	return tc.isDroppableType(tc.bindingType(symID))
}

// dropObligationsSuppressed gates the recording paths: crossing bodies
// (`on`/spawn frames) and blocking bodies ship their state to a runtime
// that has its own release discipline (RV2-DEBT-034 activates it) —
// synthesized drops there would race the runtime's ownership. Local
// synthesis stays out until that vertical.
func (tc *typeChecker) dropObligationsSuppressed() bool {
	return len(tc.onCrossingStack) > 0 || tc.blockingDepth > 0
}

func (tc *typeChecker) pushDropScope(functionRoot bool) {
	tc.dropScopes = append(tc.dropScopes, dropScope{functionRoot: functionRoot})
}

// registerDroppableParams registers a function's by-value non-copy
// params (including a by-value self) into the function-root drop scope:
// the callee owns them and drops them on every exit unless it moved
// them onward.
func (tc *typeChecker) registerDroppableParams(fn *ast.FnItem, scope symbols.ScopeID) {
	if tc.builder == nil || fn == nil {
		return
	}
	if selfSym := tc.findSelfSymbol(fn, scope); selfSym.IsValid() {
		tc.registerDroppableBinding(selfSym)
	}
	for _, pid := range tc.builder.Items.GetFnParamIDs(fn) {
		param := tc.builder.Items.FnParam(pid)
		if param == nil || param.Name == source.NoStringID {
			continue
		}
		symID := tc.symbolInScope(scope, param.Name, symbols.SymbolParam)
		tc.registerDroppableBinding(symID)
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

// rejectPartialPathMoves diagnoses droppable bindings moved on one branch
// of a join but not the other: without per-binding runtime drop flags
// there is no correct static drop point for such a value.
func (tc *typeChecker) rejectPartialPathMoves(a, b map[symbols.SymbolID]source.Span) {
	tc.reportOneSidedMoves(a, b)
	tc.reportOneSidedMoves(b, a)
}

func (tc *typeChecker) reportOneSidedMoves(moved, other map[symbols.SymbolID]source.Span) {
	for symID, span := range moved {
		if _, both := other[symID]; both {
			continue
		}
		if !tc.isDroppableBinding(symID) {
			continue
		}
		// Branch-local bindings are popped by the time the join merges;
		// only a binding still on the scope stack outlives the join and
		// needs one fate.
		if !tc.bindingDeclaredAtOrAbove(symID, 0) {
			continue
		}
		name := tc.bindingName(symID)
		tc.report(diag.SemaPartialPathMove, span,
			"value '%s' is moved on some paths but not others; a droppable value needs one fate — move it on every branch or none",
			name)
	}
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
	if _, moved := tc.movedBindings[symID]; moved {
		return
	}
	if tc.result.ReassignOldDrops == nil {
		tc.result.ReassignOldDrops = make(map[ast.ExprID]symbols.SymbolID)
	}
	tc.result.ReassignOldDrops[exprID] = symID
}
