package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// wholePlace names a binding with no projection: the place a whole-binding
// move gives away, and the one every symbol-shaped caller is really asking
// about until the rest of the epic teaches them to ask about a field.
func wholePlace(symID symbols.SymbolID) Place {
	return Place{Base: symID}
}

func (tc *typeChecker) markPlaceMoved(place Place, span source.Span) {
	if !place.IsValid() {
		return
	}
	if tc.movedPlaces == nil {
		tc.movedPlaces = make(map[Place]source.Span)
	}
	insertMovedPlace(tc.movedPlaces, place, span)
}

// insertMovedPlace keeps the moved-set an ANTICHAIN: no entry covers another.
// Once `o` has gone whole, recording `o.inner` beside it adds nothing — every
// query that overlaps the field already overlaps the container — and leaving
// both in place makes the set's shape depend on the order moves were seen,
// which then leaks into which entry a diagnostic names.
//
// Joining two branches is where this earns its keep: one arm moving `o.inner`
// and the other moving `o` whole unions to `{o.inner, o}`, and the answer the
// language wants is that `o` went. Collapsing at insert makes the join say so
// rather than relying on every reader to work it out again.
func insertMovedPlace(set map[Place]source.Span, place Place, span source.Span) {
	for existing := range set {
		// Something already recorded covers this: nothing to add.
		if placeCovers(existing, place) {
			return
		}
	}
	for existing := range set {
		// This covers entries already recorded: they are now redundant.
		if placeCovers(place, existing) {
			delete(set, existing)
		}
	}
	// First move wins for an exact repeat: the diagnostic wants the span where
	// the value LEFT, and a later move of the same place is itself the error
	// being reported.
	if _, exists := set[place]; !exists {
		set[place] = span
	}
}

func (tc *typeChecker) markBindingMoved(symID symbols.SymbolID, span source.Span) {
	if !symID.IsValid() {
		return
	}
	tc.markPlaceMoved(wholePlace(symID), span)
}

// clearPlaceMoved revives exactly the place named, not everything under it.
// Matching by exact key is behaviour-identical today — the partial-move gate
// keeps every stored key whole — and it is deliberately the narrow choice, so
// that reinitialization semantics are decided where they belong rather than
// inherited from a helper.
func (tc *typeChecker) clearPlaceMoved(place Place) {
	if !place.IsValid() || tc.movedPlaces == nil {
		return
	}
	delete(tc.movedPlaces, place)
}

// revivePlace reinitializes a place: the store puts a value back, so this place
// and everything under it are live again.
//
// It answers "was this a legal reinitialization" rather than doing it blindly,
// because the two failing shapes are different. If a WIDER place is moved —
// `o` went whole and the program assigns `o.inner` — this is not a
// reinitialization at all: the container has no storage to assign into, and the
// state it would produce ("`o` moved except `o.inner`") is one the moved-set
// cannot represent. Rust rejects the same shape, and rejecting it is what keeps
// the set an antichain.
func (tc *typeChecker) revivePlace(place Place) (blockedBy Place, blockedSpan source.Span, ok bool) {
	if !place.IsValid() {
		return Place{}, source.Span{}, true
	}
	for existing, span := range tc.movedPlaces {
		if existing != place && placeCovers(existing, place) {
			return existing, span, false
		}
	}
	// Everything this place covers comes back with it: assigning `o` revives
	// `o.inner`, and assigning `o.inner` revives `o.inner.deep`.
	for existing := range tc.movedPlaces {
		if placeCovers(place, existing) {
			delete(tc.movedPlaces, existing)
		}
	}
	return Place{}, source.Span{}, true
}

func (tc *typeChecker) clearBindingMoved(symID symbols.SymbolID) {
	if !symID.IsValid() {
		return
	}
	tc.clearPlaceMoved(wholePlace(symID))
}

// placeMoved reports whether this exact place has been given away. It does NOT
// consult the overlap relation: with `o.inner` moved, asking about `o` still
// answers false here. Widening that is Epic 24 step 3, and it is left to that
// step on purpose — answering by overlap now would change the meaning of every
// caller while this step's gate is "the corpus is identical", which is exactly
// the shape of change such a gate cannot see.
func (tc *typeChecker) placeMoved(place Place) bool {
	if !place.IsValid() || tc.movedPlaces == nil {
		return false
	}
	_, moved := tc.movedPlaces[place]
	return moved
}

func (tc *typeChecker) bindingMovedPlace(symID symbols.SymbolID) bool {
	return symID.IsValid() && tc.placeMoved(wholePlace(symID))
}

// movedPlaceCovering finds a moved place that OVERLAPS the one being read, and
// overlap is the right relation in both directions:
//
//   - `o.inner` moved, reading `o.inner` — the same place, gone;
//   - `o.inner` moved, reading `o` — `o` is no longer whole, so reading it
//     whole is reading something partly given away;
//   - `o.inner` moved, reading `o.label` — disjoint, and still there;
//   - `o` moved whole, reading `o.label` — the container went, so did the field.
//
// Linear in the moved-set, which holds only the values given away and still in
// scope. If that ever stops being small, index it by base rather than making
// the relation cheaper — the relation is the part that has to stay exact.
func (tc *typeChecker) movedPlaceCovering(place Place) (Place, source.Span, bool) {
	if !place.IsValid() || tc.movedPlaces == nil {
		return Place{}, source.Span{}, false
	}
	// Exact match first: when a place was moved as itself, that is the span the
	// reader wants, even if a wider place also covers it.
	if span, ok := tc.movedPlaces[place]; ok {
		return place, span, true
	}
	var (
		best     Place
		bestSpan source.Span
		found    bool
	)
	for candidate, span := range tc.movedPlaces {
		if !placesOverlap(candidate, place) {
			continue
		}
		// Map order is random, so the choice has to be TOTAL, not merely
		// usually-stable: earliest move, then shortest path, then the path key
		// itself. Spans can tie — synthesized and shared spans do — and two
		// candidates left to map order would move the diagnostic between
		// identical compiles.
		if !found || lessMovedCandidate(span, candidate, bestSpan, best) {
			best, bestSpan, found = candidate, span, true
		}
	}
	return best, bestSpan, found
}

// lessMovedCandidate totally orders the moved places covering one read, so the
// reported move is a function of the program rather than of map iteration.
func lessMovedCandidate(aSpan source.Span, a Place, bSpan source.Span, b Place) bool {
	if aSpan.Start != bSpan.Start {
		return aSpan.Start < bSpan.Start
	}
	if aSpan.End != bSpan.End {
		return aSpan.End < bSpan.End
	}
	if len(a.Path) != len(b.Path) {
		return len(a.Path) < len(b.Path)
	}
	return a.Path < b.Path
}

// checkPlaceUseAfterMove reports reading a place whose storage has been given
// away, whole or in part.
func (tc *typeChecker) checkPlaceUseAfterMove(expr ast.ExprID, span source.Span) {
	if len(tc.movedPlaces) == 0 {
		return
	}
	desc, ok := tc.resolvePlace(expr)
	if !ok || !desc.Base.IsValid() {
		return
	}
	// Expanded the same way observeMove expands before RECORDING a move: a
	// binding that came from a borrow stands for the place it borrowed, so a
	// read through it has to ask about that place. Asking about the unexpanded
	// place would look up a key no move ever writes.
	desc, _ = tc.expandPlaceDescriptor(desc)
	place := tc.canonicalPlace(desc)
	if !place.IsValid() {
		return
	}
	moved, moveSpan, found := tc.movedPlaceCovering(place)
	if !found {
		return
	}
	// A WHOLE-binding move is the ordinary use-after-move however it is read, and
	// the binding-level check already words it well: "use of moved value 'b'"
	// names what went and where it went. The place-aware wording is for the
	// PARTIAL case, where the value that went and the value being read are
	// different names and a reader told only about the binding would go looking
	// for a move that is not in the source.
	//
	// Keyed on the MOVE alone, not on the read too. Asking that the read also be
	// whole sent `b[0]` after `b` had gone entirely through the place-aware
	// wording, which explained a partial move that had not happened.
	if moved.Path == "" {
		tc.checkUseAfterMove(desc.Base, span)
		return
	}
	tc.reportPlaceUseAfterMove(place, moved, span, moveSpan)
}

func (tc *typeChecker) checkUseAfterMove(symID symbols.SymbolID, span source.Span) {
	if !symID.IsValid() || tc.movedPlaces == nil {
		return
	}
	_, moveSpan, moved := tc.movedPlaceCovering(wholePlace(symID))
	if !moved {
		return
	}
	name := "_"
	if sym := tc.symbolFromID(symID); sym != nil {
		if symName := tc.lookupName(sym.Name); symName != "" {
			name = symName
		}
	}
	if tc.isTaskType(tc.bindingType(symID)) {
		tc.report(diag.SemaUseAfterMove, span, "use of moved task '%s'; call %s.clone() to keep a handle", name, name)
		return
	}
	bindingType := tc.bindingType(symID)
	if tc.isFarType(bindingType) && tc.channelPayloadType(tc.farInner(bindingType)) != types.NoTypeID {
		tc.report(diag.SemaUseAfterMove, span,
			"use of moved far channel handle '%s'; call %s.share() before moving it so each holder keeps its own lease",
			name, name)
		return
	}
	// The way out rides the diagnostic: name where the value went and
	// what keeps this use valid.
	if tc.reporter != nil {
		if b := diag.ReportError(tc.reporter, diag.SemaUseAfterMove, span,
			fmt.Sprintf("use of moved value '%s'", name)); b != nil {
			if moveSpan != (source.Span{}) {
				b.WithNote(moveSpan, fmt.Sprintf("'%s' gave its value away here", name))
			}
			b.WithNote(span, fmt.Sprintf(
				"hint: if the receiver only reads '%s', let it take a reference (&); to keep using '%s' here, pass a clone instead: %s.__clone()",
				name, name, name))
			b.Emit()
		}
	}
}

func (tc *typeChecker) snapshotMovedPlaces() map[Place]source.Span {
	out := make(map[Place]source.Span, len(tc.movedPlaces))
	for key, value := range tc.movedPlaces {
		out[key] = value
	}
	return out
}

func (tc *typeChecker) restoreMovedPlaces(snapshot map[Place]source.Span) {
	tc.movedPlaces = make(map[Place]source.Span, len(snapshot))
	for key, value := range snapshot {
		tc.movedPlaces[key] = value
	}
}

// mergeMovedPlaces joins two branch states by UNION: a value moved on any
// reachable predecessor is moved after the join, because a later use has to be
// rejected if any path gave it away. The intersection — "moved on every path" —
// is not the safety condition for a use, and the intersect helper that used to
// sit beside this one was computed at the compare join and never read, so it
// was deleted here rather than ported to places.
func mergeMovedPlaces(a, b map[Place]source.Span) map[Place]source.Span {
	out := make(map[Place]source.Span, len(a)+len(b))
	for key, value := range a {
		insertMovedPlace(out, key, value)
	}
	for key, value := range b {
		insertMovedPlace(out, key, value)
	}
	return out
}
