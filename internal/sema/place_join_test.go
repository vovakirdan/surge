package sema

import (
	"testing"

	"surge/internal/source"
	"surge/internal/symbols"
)

// Branch joins over places. The partial-move gate keeps these states
// unreachable from source, so the join is exercised where it is computed.
//
// The union is the join because a later use has to be rejected if ANY reachable
// predecessor gave the value away. What the union must not do is flatten a
// binding: two arms moving two different fields leave the rest of the value
// readable, and that is the case separating a real implementation from one that
// invalidates the whole binding whenever any part of it moves.
func TestJoinOverPlaces(t *testing.T) {
	bt := NewBorrowTable()
	o := symbols.SymbolID(1)
	field := func(name uint32) PlaceSegment {
		return PlaceSegment{Kind: PlaceSegmentField, Name: source.StringID(name)}
	}
	at := func(segs ...PlaceSegment) Place { return bt.CanonicalPlace(o, segs) }

	inner := at(field(1))
	label := at(field(2))
	spare := at(field(3))
	whole := wholePlace(o)

	armMoving := func(place Place, start uint32) map[Place]source.Span {
		set := map[Place]source.Span{}
		insertMovedPlace(set, place, source.Span{Start: start, End: start + 1})
		return set
	}
	covered := func(set map[Place]source.Span, place Place) bool {
		tc := &typeChecker{movedPlaces: set}
		_, _, found := tc.movedPlaceCovering(place)
		return found
	}

	t.Run("a field moved on one arm only is moved after the join", func(t *testing.T) {
		// Contract row 7: reading it after the join is rejected.
		union := mergeMovedPlaces(armMoving(inner, 10), map[Place]source.Span{})
		if !covered(union, inner) {
			t.Errorf("`o.inner` read as live after an arm moved it")
		}
		// ...and the rest of the value is still there.
		if covered(union, label) {
			t.Errorf("`o.label` was rejected although no arm moved it")
		}
	})

	t.Run("a field on one arm and the whole value on the other joins to whole", func(t *testing.T) {
		// Contract row 8. The union is `{o.inner, o}` before collapsing, and
		// the answer the language wants is that `o` went.
		union := mergeMovedPlaces(armMoving(inner, 10), armMoving(whole, 20))
		if len(union) != 1 {
			t.Fatalf("expected the join to collapse to the container, got %d entries", len(union))
		}
		if _, ok := union[whole]; !ok {
			t.Fatalf("the join kept the field instead of the container it is inside")
		}
		// Every place under it is now unreadable, including one no arm named.
		for _, place := range []Place{whole, inner, label, spare} {
			if !covered(union, place) {
				t.Errorf("a place under a wholly moved binding read as live: %v", place)
			}
		}
	})

	t.Run("different fields on different arms leave the rest readable", func(t *testing.T) {
		// The opposite case, and the one a conservative implementation fails:
		// arm A moves `inner`, arm B moves `label`, and `spare` survives both.
		union := mergeMovedPlaces(armMoving(inner, 10), armMoving(label, 20))
		if len(union) != 2 {
			t.Fatalf("expected both fields to survive the join, got %d entries", len(union))
		}
		if !covered(union, inner) || !covered(union, label) {
			t.Errorf("a field moved on one arm read as live after the join")
		}
		if covered(union, spare) {
			t.Errorf("`o.spare` was rejected although neither arm moved it")
		}
		// The container as a whole is not readable — part of it went on every
		// path — while its untouched field still is.
		if !covered(union, whole) {
			t.Errorf("`o` read as whole although both arms moved a field out of it")
		}
	})

	t.Run("the join does not depend on arm order", func(t *testing.T) {
		forward := mergeMovedPlaces(armMoving(inner, 10), armMoving(whole, 20))
		backward := mergeMovedPlaces(armMoving(whole, 20), armMoving(inner, 10))
		if len(forward) != len(backward) {
			t.Fatalf("arm order changed the join size: %d vs %d", len(forward), len(backward))
		}
		// Keys AND spans: the reported site is part of the join's answer, and
		// comparing only keys would miss a collapse that kept whichever span
		// happened to be inserted last.
		for place, span := range forward {
			other, ok := backward[place]
			if !ok {
				t.Fatalf("arm order changed the joined set: %v missing when reversed", place)
			}
			if other != span {
				t.Fatalf("arm order changed the reported span for %v: %v vs %v", place, span, other)
			}
		}
	})

	t.Run("a collapsed entry points at the move that made it whole", func(t *testing.T) {
		// Deliberately NOT the earliest span. The entry means "`o` went whole",
		// and the span has to point where that happened: sending a reader to
		// the earlier field move would show them a move of `o.inner`, which is
		// not the reason `o` is unreadable.
		union := mergeMovedPlaces(armMoving(inner, 10), armMoving(whole, 20))
		span, ok := union[whole]
		if !ok {
			t.Fatalf("the join did not collapse to the container")
		}
		if span.Start != 20 {
			t.Fatalf("collapsed entry points at %v; want the whole-move at 20", span)
		}
	})
}
