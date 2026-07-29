package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// DropStep is one action in a residual drop: the sequence that reclaims what a
// PARTIALLY-MOVED binding still holds, without touching what left it.
//
// Path is relative to the binding; empty names the binding itself. Shallow
// frees only that place's own storage and leaves its contents alone — the
// fields still sitting in a partially-moved container are the ones that moved
// away, and releasing them here would free storage this value no longer owns.
type DropStep struct {
	Path    []PlaceSegment
	Shallow bool
}

// DropSite identifies one obligation: a binding, at one exit. The same binding
// can need different residuals at different exits, because different paths move
// different things out of it — so the plan cannot be attached to the binding
// alone.
type DropSite struct {
	Stmt   ast.StmtID
	Expr   ast.ExprID
	Symbol symbols.SymbolID
}

// residualDropPlan returns the steps that reclaim what symID still holds, or
// nil when an ordinary whole-binding drop is right.
//
// nil is the answer in every case reachable today: the partial-move gate keeps
// a binding either wholly live or wholly gone, and both of those drop whole or
// not at all. The plan only becomes non-trivial once Epic 24 step 8 lets a
// field move on its own.
func (tc *typeChecker) residualDropPlan(symID symbols.SymbolID) []DropStep {
	if len(tc.movedPlaces) == 0 || !symID.IsValid() {
		return nil
	}
	whole := wholePlace(symID)
	moved, _, found := tc.movedPlaceCovering(whole)
	if !found {
		// Nothing under it moved: the ordinary drop reclaims all of it.
		return nil
	}
	if placeCovers(moved, whole) {
		// The binding went whole; it carries no obligation at all, and
		// liveDroppables has already excluded it.
		return nil
	}
	var steps []DropStep
	if !tc.appendResidualSteps(&steps, symID, nil, tc.bindingType(symID)) {
		// The residual could not be expressed — see appendResidualSteps. The
		// caller keeps the whole-binding drop, which is what happens today.
		return nil
	}
	return steps
}

// appendResidualSteps walks the value post-order, emitting a deep drop for each
// live subtree and a shallow free for each container that survives only in
// part. Reports whether the whole residual was expressible.
//
// Post-order because a container's own storage must go AFTER the fields inside
// it, and live siblings keep reverse declaration order, which is the order the
// whole-value glue already uses.
func (tc *typeChecker) appendResidualSteps(out *[]DropStep, base symbols.SymbolID, path []PlaceSegment, ty types.TypeID) bool {
	place := tc.canonicalPlace(placeDescriptor{Base: base, Segments: path})
	if !place.IsValid() {
		return false
	}
	moved, _, found := tc.movedPlaceCovering(place)
	if !found {
		// Wholly live: a deep drop reclaims it.
		//
		// Emitted for EVERY live place, not only those sema calls droppable.
		// The two backends do not agree on what owns storage — an `int` is an
		// immediate on the native side and a reference-counted object in the
		// VM — and a whole-value drop hid that disagreement by walking every
		// field anyway. A residual drop stops walking, so a place skipped here
		// is a place nobody releases: measured as 8 leaked VM objects from a
		// struct whose only surviving field was an `int`.
		//
		// Naming a place that owns nothing costs nothing: both backends look at
		// what is actually there and emit no work for an immediate.
		*out = append(*out, DropStep{Path: clonePlacePath(path)})
		return true
	}
	if placeCovers(moved, place) {
		// This place itself went. Nothing here is ours to release.
		return true
	}

	// Partially moved: reclaim what remains, then the container's own storage.
	fields := tc.types.StructFields(tc.resolveAlias(tc.valueType(ty)))
	if len(fields) == 0 {
		// Something is moved UNDER this place, but its parts cannot be
		// enumerated — an array element, a union payload. Refusing here is what
		// keeps the caller on the whole-binding drop rather than emitting a
		// shallow free that abandons the live remainder.
		//
		// Unreachable while the gate is up. Step 8 must either enumerate these
		// shapes or keep rejecting the moves that create them: a constant array
		// index is enumerable, a computed one is not, and a union needs the tag.
		return false
	}
	for i := len(fields) - 1; i >= 0; i-- {
		child := append(clonePlacePath(path), PlaceSegment{
			Kind: PlaceSegmentField,
			Name: fields[i].Name,
		})
		if !tc.appendResidualSteps(out, base, child, fields[i].Type) {
			return false
		}
	}
	*out = append(*out, DropStep{Path: clonePlacePath(path), Shallow: true})
	return true
}

// clonePlacePath copies a path so appending a child segment cannot write into
// a slice a sibling step is still holding.
func clonePlacePath(path []PlaceSegment) []PlaceSegment {
	if len(path) == 0 {
		return nil
	}
	out := make([]PlaceSegment, len(path))
	copy(out, path)
	return out
}

// recordResidualDrops stores the non-trivial plans for one obligation list.
// Bindings that drop whole are absent, which is what makes the lookup's default
// the ordinary drop.
func (tc *typeChecker) recordResidualDrops(site DropSite, syms []symbols.SymbolID) {
	for _, symID := range syms {
		steps := tc.residualDropPlan(symID)
		if len(steps) == 0 {
			continue
		}
		if tc.result.ResidualDrops == nil {
			tc.result.ResidualDrops = make(map[DropSite][]DropStep)
		}
		key := site
		key.Symbol = symID
		tc.result.ResidualDrops[key] = steps
	}
}
