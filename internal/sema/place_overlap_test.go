package sema

import (
	"testing"

	"surge/internal/source"
	"surge/internal/symbols"
)

// placesOverlap answers "can these two names denote the same storage", which is
// the relation a place-keyed moved-set needs: with `o.inner` given away, `o` is
// no longer whole, while `o.label` is untouched.
//
// It reads only the interned key, deliberately. The earlier version decoded each
// key back into segments through the BorrowTable that interned it, so the answer
// depended on which table was asked — and a place stored in the moved-set
// outlives any single query. A place interned by one table and compared through
// another silently lost its path and read as a whole-binding place.
func TestPlacesOverlap(t *testing.T) {
	bt := NewBorrowTable()
	base := symbols.SymbolID(1)
	other := symbols.SymbolID(2)

	field := func(name uint32) PlaceSegment {
		return PlaceSegment{Kind: PlaceSegmentField, Name: source.StringID(name)}
	}
	at := func(b symbols.SymbolID, segs ...PlaceSegment) Place {
		return bt.CanonicalPlace(b, segs)
	}

	whole := at(base)
	inner := at(base, field(1))
	innerDeep := at(base, field(1), field(3))
	label := at(base, field(2))
	// Field ids 1 and 12 are the case the terminator exists for: without the
	// `;` after each segment, the key "f:1" is a textual prefix of "f:12" and
	// two unrelated fields would be reported as overlapping.
	twelve := at(base, field(12))
	indexed := at(base, field(1), PlaceSegment{Kind: PlaceSegmentIndex})
	dereffed := at(base, field(1), PlaceSegment{Kind: PlaceSegmentDeref})

	cases := []struct {
		name string
		a, b Place
		want bool
	}{
		{"whole_covers_itself", whole, whole, true},
		{"whole_covers_field", whole, inner, true},
		{"field_covers_whole", inner, whole, true},
		{"field_covers_deeper", inner, innerDeep, true},
		{"siblings_are_disjoint", inner, label, false},
		{"deeper_vs_sibling_disjoint", innerDeep, label, false},
		{"prefix_of_id_is_not_prefix_of_path", inner, twelve, false},
		{"different_bases_never_overlap", whole, at(other), false},
		// Mixed kinds after a shared prefix: an index and a deref of the same
		// field are different storage, and the encoding has to keep them so.
		{"index_vs_deref_after_same_field", indexed, dereffed, false},
		{"field_covers_its_index", inner, indexed, true},
		{"index_vs_sibling_field", indexed, label, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := placesOverlap(tc.a, tc.b); got != tc.want {
				t.Fatalf("placesOverlap(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// The moved-set stores places that outlive the table which interned them, so
// the same path interned by a DIFFERENT table has to compare equal and overlap
// the same way. This is what makes it safe to key a long-lived map by Place.
func TestPlaceKeysAreTableIndependent(t *testing.T) {
	base := symbols.SymbolID(1)
	seg := []PlaceSegment{{Kind: PlaceSegmentField, Name: source.StringID(7)}}

	first := NewBorrowTable().CanonicalPlace(base, seg)
	second := NewBorrowTable().CanonicalPlace(base, seg)

	if first != second {
		t.Fatalf("same path interned by two tables produced different places: %v vs %v", first, second)
	}
	if !placesOverlap(first, second) {
		t.Fatalf("a place did not overlap its own twin from another table")
	}
	// The whole-binding place must still cover it, which is the query every
	// symbol-shaped caller makes.
	if !placesOverlap(wholePlace(base), second) {
		t.Fatalf("whole-binding place did not cover a field interned elsewhere")
	}
}

// Whole-binding moves are the only ones reachable while the partial-move gate
// is up, so the moved-set has to answer for them exactly as the symbol-keyed
// map did — including that a field of a moved binding is NOT itself recorded.
func TestMovedSetTracksWholeBindings(t *testing.T) {
	tc := &typeChecker{}
	base := symbols.SymbolID(3)
	span := source.Span{Start: 10, End: 20}

	if tc.bindingMovedPlace(base) {
		t.Fatalf("an untouched binding reported as moved")
	}
	tc.markBindingMoved(base, span)
	if !tc.bindingMovedPlace(base) {
		t.Fatalf("a moved binding did not report as moved")
	}
	if got := tc.movedPlaces[wholePlace(base)]; got != span {
		t.Fatalf("moved span = %v, want %v", got, span)
	}

	// The first move wins: the diagnostic points at where the value LEFT.
	tc.markBindingMoved(base, source.Span{Start: 99, End: 100})
	if got := tc.movedPlaces[wholePlace(base)]; got != span {
		t.Fatalf("a later move overwrote the first move's span: %v", got)
	}

	tc.clearBindingMoved(base)
	if tc.bindingMovedPlace(base) {
		t.Fatalf("a cleared binding still reported as moved")
	}
}

// The join and snapshot machinery has to survive the key change, and these are
// the operations the branch walkers rely on: take a state, explore a branch,
// put it back, and union two branch states.
func TestMovedSetSnapshotRestoreAndMerge(t *testing.T) {
	bt := NewBorrowTable()
	tc := &typeChecker{}
	a := symbols.SymbolID(1)
	b := symbols.SymbolID(2)
	spanA := source.Span{Start: 1, End: 2}
	spanB := source.Span{Start: 3, End: 4}

	tc.markBindingMoved(a, spanA)
	before := tc.snapshotMovedPlaces()

	// A branch moves `b` as well; restoring must undo exactly that.
	tc.markBindingMoved(b, spanB)
	branch := tc.snapshotMovedPlaces()
	tc.restoreMovedPlaces(before)
	if tc.bindingMovedPlace(b) {
		t.Fatalf("restore did not undo the branch's move")
	}
	if !tc.bindingMovedPlace(a) {
		t.Fatalf("restore dropped a move made before the branch")
	}

	// The join is a UNION: moved on any reachable path means moved after.
	union := mergeMovedPlaces(before, branch)
	if _, ok := union[wholePlace(a)]; !ok {
		t.Fatalf("union lost a move present on both sides")
	}
	if _, ok := union[wholePlace(b)]; !ok {
		t.Fatalf("union lost a move present on one side")
	}
	// Snapshots must be independent copies, or a branch walk would mutate the
	// state it was supposed to be able to abandon.
	if len(before) != 1 {
		t.Fatalf("the pre-branch snapshot was mutated by later marks: %d entries", len(before))
	}

	// Clearing is exact: a projected place of the same base is untouched.
	field := bt.CanonicalPlace(a, []PlaceSegment{{Kind: PlaceSegmentField, Name: source.StringID(5)}})
	tc.markPlaceMoved(field, spanB)
	tc.clearBindingMoved(a)
	if !tc.placeMoved(field) {
		t.Fatalf("clearing the whole binding also cleared a projected place")
	}
}
