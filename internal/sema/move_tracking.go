package sema

import (
	"fmt"

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
	// First move wins: the diagnostic wants the span where the value LEFT,
	// and a later move of the same place is itself the error being reported.
	if _, exists := tc.movedPlaces[place]; !exists {
		tc.movedPlaces[place] = span
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

func (tc *typeChecker) checkUseAfterMove(symID symbols.SymbolID, span source.Span) {
	if !symID.IsValid() || tc.movedPlaces == nil {
		return
	}
	moveSpan, moved := tc.movedPlaces[wholePlace(symID)]
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
		out[key] = value
	}
	for key, value := range b {
		if _, exists := out[key]; !exists {
			out[key] = value
		}
	}
	return out
}
