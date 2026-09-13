package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/symbols"
)

// armResultDuplicatesPayload reports whether a consuming binding read creates
// a separate owner for the arm result. The binding keeps its existing drop.
func (tc *typeChecker) armResultDuplicatesPayload(symID symbols.SymbolID) bool {
	if tc.types == nil {
		return false
	}
	ty := tc.resolveAlias(tc.bindingType(symID))
	return tc.types.IsRefCounted(ty) || (tc.isCopyType(ty) && tc.types.IsValueComposite(ty))
}

// armHandsOutItsPayload reports whether this arm's result names a payload
// binding of its OWN pattern, and returns the arm's obligations without it.
//
// That value leaves the arm: the compare's result is the only thing holding it
// once the arm is done, so the arm's own release would free it while the
// compare's reader still points at it — measured as an invalid read per
// iteration where the compare's result was BORROWED, since a borrow never
// consumes and the retraction below never ran.
//
// Answered from the arm's OBLIGATIONS rather than from the pattern alone, which
// is what keeps a borrowed subject out of it: `compare *arg { Payload(s) => s }`
// binds a payload the union still owns, that binding earns no obligation, and
// nothing here is its to hand out.
//
// Counted values and Copy composites remain owned by their binding when read.
// A counted read retains; a Copy-composite read duplicates its members. The
// compare result receives a separate value while the binding still holds its
// original owner. Both therefore keep the binding's existing obligation.
// Withdrawing that obligation abandons its owner, including the counted
// members of a composite returned by `Some(x) => x`.
func (tc *typeChecker) armHandsOutItsPayload(
	result ast.ExprID,
	bindings []symbols.SymbolID,
	drops []symbols.SymbolID,
) ([]symbols.SymbolID, bool) {
	if !result.IsValid() || len(bindings) == 0 || len(drops) == 0 {
		return drops, false
	}
	symID := tc.symbolForExpr(tc.unwrapGroups(result))
	if !symID.IsValid() || !slices.Contains(bindings, symID) {
		return drops, false
	}
	idx := slices.Index(drops, symID)
	if idx < 0 {
		return drops, false
	}
	if tc.armResultDuplicatesPayload(symID) {
		return drops, true
	}
	return slices.Delete(slices.Clone(drops), idx, idx+1), true
}

// releaseArmResultObligations takes back the obligation an arm earned for a
// payload binding it RETURNED, once the compare's own value turns out to be
// consumed.
//
// Whether an arm handed its payload onward is not knowable while the arm is
// being typed. `let out = compare v { Payload(s) => s; ... }` consumes the
// result and the binding must NOT also drop it; `peek(compare v { ... })` with
// a `&string` parameter only borrows it, and then the binding's drop is the
// only thing that reclaims it; a discarded compare is the same. The arm is
// typed before any of those are known.
//
// So the arm keeps the obligation by default and the consuming context takes
// it away here — the leak-over-double-free direction while the answer is
// unknown, which is the direction the rest of this file errs in too. This runs
// from observeMove, which is exactly the set of positions that consume a
// value.
//
// Nested compares recurse: an arm whose result is itself a compare hands ITS
// arms' results onward by the same argument. An arm whose result is a BLOCK
// needs nothing here — a block's tail expression already observes its own move
// while the arm is being typed, so the binding never earned the obligation.
//
// Counted values and Copy composites are exempt, as armHandsOutItsPayload
// describes: a retaining or copying read gives the consumer a separate owner,
// while the binding still holds its own. Its obligation must survive.
// The two decisions must not drift — one withdrawing the obligation while the
// other keeps it is the difference between a leak and a double free — so they
// ask one predicate.
func (tc *typeChecker) releaseArmResultObligations(expr ast.ExprID) {
	if tc.builder == nil || tc.result == nil {
		return
	}
	cmp, ok := tc.builder.Exprs.Compare(tc.unwrapGroups(expr))
	if !ok || cmp == nil {
		return
	}

	// The arms are branches of one choice, exactly as a ternary's two are: one
	// runs and hands its result onward. An arm returning a binding declared
	// OUTSIDE the compare needs that whole treatment — the move, so the
	// binding's scope-exit drop stands down, and a drop on every sibling arm,
	// which still holds it.
	results := make([]ast.ExprID, 0, len(cmp.Arms))
	for _, arm := range cmp.Arms {
		if !arm.Result.IsValid() || tc.compareArmAbruptExit(arm.Result) {
			// An arm that exits abruptly hands nothing to the compare; its own
			// `return` already accounted for what it moved.
			continue
		}
		results = append(results, arm.Result)
	}
	tc.consumeBranchResults(results)

	if len(tc.result.ArmDropsExpr) == 0 {
		return
	}
	for _, arm := range cmp.Arms {
		if !arm.Result.IsValid() {
			continue
		}
		symID := tc.symbolForExpr(arm.Result)
		if !symID.IsValid() || tc.armResultDuplicatesPayload(symID) {
			continue
		}
		drops := tc.result.ArmDropsExpr[arm.Result]
		kept := drops[:0:0]
		for _, owed := range drops {
			if owed != symID {
				kept = append(kept, owed)
			}
		}
		if len(kept) == 0 {
			delete(tc.result.ArmDropsExpr, arm.Result)
			continue
		}
		tc.result.ArmDropsExpr[arm.Result] = kept
	}
}
