package sema

import (
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// Assigning INTO a place reinitializes it: the store puts a value back, so the
// place and everything under it are live again, and with nothing else moved the
// container is whole once more.
//
// The failing shape is the one that decides the representation. If the
// CONTAINER went whole and the program assigns a field, that is not a
// reinitialization — there is no storage to assign into, and the state it would
// ask for ("`o` moved except `o.inner`") is one an antichain cannot hold.
// Rejecting it is what keeps the moved-set able to stay an antichain.
func TestRevivePlace(t *testing.T) {
	bt := NewBorrowTable()
	o := symbols.SymbolID(1)
	field := func(name uint32) PlaceSegment {
		return PlaceSegment{Kind: PlaceSegmentField, Name: source.StringID(name)}
	}
	at := func(segs ...PlaceSegment) Place { return bt.CanonicalPlace(o, segs) }

	inner := at(field(1))
	innerDeep := at(field(1), field(3))
	label := at(field(2))
	whole := wholePlace(o)
	span := source.Span{Start: 1, End: 2}

	t.Run("reviving the moved field makes the container whole again", func(t *testing.T) {
		tc := &typeChecker{}
		tc.markPlaceMoved(inner, span)

		if _, _, ok := tc.revivePlace(inner); !ok {
			t.Fatalf("reviving the very place that moved was refused")
		}
		if len(tc.movedPlaces) != 0 {
			t.Fatalf("expected nothing moved after the revive, got %v", tc.movedPlaces)
		}
		if _, _, found := tc.movedPlaceCovering(whole); found {
			t.Fatalf("`o` still read as moved after its only moved field came back")
		}
	})

	t.Run("reviving a container brings back what it covers", func(t *testing.T) {
		tc := &typeChecker{}
		tc.markPlaceMoved(innerDeep, span)

		if _, _, ok := tc.revivePlace(inner); !ok {
			t.Fatalf("reviving `o.inner` was refused although only a place under it had moved")
		}
		if _, _, found := tc.movedPlaceCovering(innerDeep); found {
			t.Fatalf("`o.inner.deep` still read as moved after `o.inner` was reassigned")
		}
	})

	t.Run("a sibling's move is untouched by the revive", func(t *testing.T) {
		tc := &typeChecker{}
		tc.markPlaceMoved(inner, span)
		tc.markPlaceMoved(label, span)

		if _, _, ok := tc.revivePlace(inner); !ok {
			t.Fatalf("reviving `o.inner` was refused")
		}
		if _, _, found := tc.movedPlaceCovering(label); !found {
			t.Fatalf("reviving `o.inner` also revived `o.label`")
		}
		// `o` is still not whole: one of its fields is still gone.
		if _, _, found := tc.movedPlaceCovering(whole); !found {
			t.Fatalf("`o` read as whole although `o.label` is still moved")
		}
	})

	t.Run("assigning into a wholly moved value is refused", func(t *testing.T) {
		tc := &typeChecker{}
		tc.markPlaceMoved(whole, span)

		blockedBy, blockedSpan, ok := tc.revivePlace(inner)
		if ok {
			t.Fatalf("assigning `o.inner` was allowed although `o` had gone whole")
		}
		if blockedBy != whole || blockedSpan != span {
			t.Fatalf("blocked by %v at %v; want the whole move %v at %v", blockedBy, blockedSpan, whole, span)
		}
		// ...and the refusal must not have disturbed the state.
		if len(tc.movedPlaces) != 1 {
			t.Fatalf("a refused revive changed the moved-set: %v", tc.movedPlaces)
		}
	})
}

// The source-level half: assigning into a value that has gone whole is
// rejected, and it is rejected for the right reason rather than by some
// unrelated type error.
func TestAssignIntoMovedValueIsRejected(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Inner = { tail: string }
type Outer = { inner: Inner, label: int }
fn take(o: Outer) -> int { return o.label; }
fn mkinner() -> Inner { return Inner { tail = "t" }; }
fn f(o: Outer) -> int {
	let _ = take(o);
	o.inner = mkinner();
	return 0;
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !hasCode(semaBag, diag.SemaUseAfterMove) {
		t.Fatalf("assigning into a moved value was accepted: %s", diagnosticsSummary(semaBag))
	}
	if got := diagnosticsSummary(semaBag); !strings.Contains(got, "nothing to assign into") {
		t.Fatalf("expected the assign-into-moved wording, got: %s", got)
	}
}

// Assigning a field of a value that is NOT moved has to stay ordinary. A revive
// that refused too eagerly would break every struct field write in the corpus,
// which is exactly the kind of thing the corpus gate would catch — but this
// states it as a rule rather than leaving it to be inferred.
func TestFieldAssignmentOfLiveValueIsAccepted(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Inner = { tail: string }
type Outer = { inner: Inner, label: int }
fn mkinner() -> Inner { return Inner { tail = "t" }; }
fn f(o: Outer) -> int {
	o.inner = mkinner();
	o.label = 3;
	return o.label;
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if semaBag.HasErrors() {
		t.Fatalf("an ordinary field assignment was rejected: %s", diagnosticsSummary(semaBag))
	}
}

// Dropping one field explicitly releases that field and leaves the rest of the
// binding readable. It is a move into nothing, so it answers like one: the
// sibling survives, and the dropped place does not.
func TestProjectedDropReleasesOneFieldAndKeepsTheRest(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Inner = { tail: string }
type Outer = { inner: Inner, label: int }
fn f(o: Outer) -> int {
	@drop o.inner;
	return o.label;
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if semaBag.HasErrors() {
		t.Fatalf("dropping one field and reading its sibling was rejected: %s", diagnosticsSummary(semaBag))
	}
	// The refusal this replaced used to claim the target was not addressable,
	// which sent the reader looking for the wrong problem.
	if got := diagnosticsSummary(semaBag); strings.Contains(got, "must be a binding") {
		t.Fatalf("projected drop still reports the misleading addressability error: %s", got)
	}
}

// The negative control beside it: what was dropped is gone. Without this the
// row above passes for an implementation that accepts the drop and then forgets
// it happened.
func TestProjectedDropEmptiesThePlaceItNames(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Inner = { tail: string }
type Outer = { inner: Inner, label: int }
fn take(i: own Inner) -> int { return 1; }
fn f(o: Outer) -> int {
	@drop o.inner;
	return take(own o.inner);
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !hasCode(semaBag, diag.SemaUseAfterMove) {
		t.Fatalf("a field was still readable after being dropped: %s", diagnosticsSummary(semaBag))
	}
}
