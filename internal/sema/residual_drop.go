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
	if !tc.appendResidualSteps(&steps, tc.bindingResidualProbe(symID), nil, tc.bindingType(symID)) {
		// The residual could not be expressed — see appendResidualSteps. The
		// caller keeps the whole-binding drop, which is what happens today.
		return nil
	}
	return steps
}

// residualProbe answers what the moved-set says about one path inside the value
// being reclaimed: `gone` when this place or an ancestor was given away,
// `partial` when something strictly UNDER it was, and `ok` false when the
// question cannot be answered at all.
//
// It exists because the same walk serves two callers that identify a value
// differently. A BINDING is a symbol with an interned moved-set; a TEMPORARY has
// no name at all, only the paths the statement took out of it. The plan they
// need is identical, so only the question differs.
type residualProbe func(path []PlaceSegment) (gone, partial, ok bool)

// bindingResidualProbe reads the answer out of the moved-set, through the same
// covering relation a read is judged by.
func (tc *typeChecker) bindingResidualProbe(base symbols.SymbolID) residualProbe {
	return func(path []PlaceSegment) (bool, bool, bool) {
		place := tc.canonicalPlace(placeDescriptor{Base: base, Segments: path})
		if !place.IsValid() {
			return false, false, false
		}
		moved, _, found := tc.movedPlaceCovering(place)
		if !found {
			return false, false, true
		}
		return placeCovers(moved, place), true, true
	}
}

// appendResidualSteps walks the value post-order, emitting a deep drop for each
// live subtree and a shallow free for each container that survives only in
// part. Reports whether the whole residual was expressible.
//
// Post-order because a container's own storage must go AFTER the fields inside
// it, and live siblings keep reverse declaration order, which is the order the
// whole-value glue already uses.
func (tc *typeChecker) appendResidualSteps(out *[]DropStep, probe residualProbe, path []PlaceSegment, ty types.TypeID) bool {
	gone, partial, ok := probe(path)
	if !ok {
		return false
	}
	if gone {
		// This place itself went. Nothing here is ours to release.
		return true
	}
	if !partial {
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

	// Partially moved: reclaim what remains, then the container's own storage.
	parts := tc.enumerableParts(ty)
	if len(parts) == 0 {
		// Something is moved UNDER this place, but its parts cannot be
		// enumerated — an array element, a union payload. Refusing here is what
		// keeps the caller on the whole-value drop rather than emitting a
		// shallow free that abandons the live remainder. The moves that create
		// these shapes are refused at the point they are written, so this is the
		// second line of defence rather than the first.
		return false
	}
	for i := len(parts) - 1; i >= 0; i-- {
		child := append(clonePlacePath(path), parts[i].segment)
		if !tc.appendResidualSteps(out, probe, child, parts[i].ty) {
			return false
		}
	}
	*out = append(*out, DropStep{Path: clonePlacePath(path), Shallow: true})
	return true
}

// enumerablePart is one statically nameable piece of a container, in
// declaration order.
type enumerablePart struct {
	segment PlaceSegment
	ty      types.TypeID
}

// enumerableParts lists the pieces a container is made of, or nothing when the
// pieces cannot be named. A struct answers with its fields and a TUPLE with its
// elements — a tuple has a fixed arity and is only ever indexed by a literal, so
// its parts are as statically known as a struct's, and it is only the syntax that
// makes it look like an array. An array answers with nothing: its length is not
// part of the question and its index is chosen at runtime.
func (tc *typeChecker) enumerableParts(ty types.TypeID) []enumerablePart {
	if tc.types == nil {
		return nil
	}
	resolved := tc.resolveAlias(tc.valueType(ty))
	if fields := tc.types.StructFields(resolved); len(fields) > 0 {
		out := make([]enumerablePart, 0, len(fields))
		for _, f := range fields {
			out = append(out, enumerablePart{
				segment: PlaceSegment{Kind: PlaceSegmentField, Name: f.Name},
				ty:      f.Type,
			})
		}
		return out
	}
	if info, ok := tc.types.TupleInfo(resolved); ok && info != nil && len(info.Elems) > 0 {
		out := make([]enumerablePart, 0, len(info.Elems))
		for i, elem := range info.Elems {
			out = append(out, enumerablePart{
				segment: PlaceSegment{Kind: PlaceSegmentTupleIndex, Elem: uint32(i)},
				ty:      elem,
			})
		}
		return out
	}
	return nil
}

// temporaryResidualPlan returns the steps that reclaim what is left of a
// statement-end temporary after the paths in `taken` were moved out of it, or
// nil when it should be released whole.
//
// A temporary is reclaimed by the same rule as a binding and identified by a
// different thing. It has no name, so there is no moved-set entry to look up
// and no way for a later statement to refer to it — which is also why the
// answer is complete here: everything that will ever be taken out of this value
// was taken inside the statement that produced it.
func (tc *typeChecker) temporaryResidualPlan(ty types.TypeID, taken [][]PlaceSegment) []DropStep {
	if len(taken) == 0 || ty == types.NoTypeID {
		return nil
	}
	var steps []DropStep
	if !tc.appendResidualSteps(&steps, temporaryResidualProbe(taken), nil, ty) {
		// Inexpressible: the caller keeps the whole release, which is what
		// happened before any of it could be taken apart.
		return nil
	}
	return steps
}

// temporaryResidualProbe answers from the paths taken out of the value, since a
// temporary has no interned places to consult.
func temporaryResidualProbe(taken [][]PlaceSegment) residualProbe {
	return func(path []PlaceSegment) (gone, partial, ok bool) {
		for _, t := range taken {
			switch {
			case pathPrefixes(t, path):
				// This place, or something it sits inside, was taken.
				return true, false, true
			case pathPrefixes(path, t):
				// Something strictly under it was taken; keep looking, because
				// a later entry may still cover this place outright.
				partial = true
			}
		}
		return false, partial, true
	}
}

// pathPrefixes reports whether `outer` names `inner` or an ancestor of it —
// the path-level form of the covering relation places use.
func pathPrefixes(outer, inner []PlaceSegment) bool {
	if len(outer) > len(inner) {
		return false
	}
	for i, seg := range outer {
		if seg != inner[i] {
			return false
		}
	}
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

// recordOneSidedDrops stores the plans a BRANCH owes: not "what this binding
// still holds" but "which of its places the other side gave away and this side
// must therefore reclaim". The two are different questions, so this cannot go
// through residualDropPlan — here the binding is still whole on this path, and
// the plan names exactly the places the join will declare gone.
func (tc *typeChecker) recordOneSidedDrops(site DropSite, plans map[symbols.SymbolID][]DropStep) {
	for symID, steps := range plans {
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
