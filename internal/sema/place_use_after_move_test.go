package sema

import (
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// movedPlaceCovering is where the per-place answer lives, so this is where the
// contract rows are pinned. The partial-move gate keeps these states
// unreachable from source for now — a program cannot move `o.inner` out — so
// the analysis is exercised directly rather than through a program.
//
// Overlap is the relation, and it has to answer in BOTH directions: a container
// is unusable once part of it has gone, and a sibling field is untouched.
func TestMovedPlaceCovering(t *testing.T) {
	bt := NewBorrowTable()
	o := symbols.SymbolID(1)
	field := func(name uint32) PlaceSegment {
		return PlaceSegment{Kind: PlaceSegmentField, Name: source.StringID(name)}
	}
	at := func(segs ...PlaceSegment) Place { return bt.CanonicalPlace(o, segs) }

	inner := at(field(1))
	label := at(field(2))
	innerDeep := at(field(1), field(3))
	innerOther := at(field(1), field(4))
	whole := wholePlace(o)

	t.Run("a moved field covers itself, its container and what is under it", func(t *testing.T) {
		tc := &typeChecker{}
		tc.markPlaceMoved(inner, source.Span{Start: 10, End: 15})

		for _, read := range []struct {
			name  string
			place Place
		}{
			{"the field itself", inner},
			{"the container as a whole", whole},
			{"a field under the moved one", innerDeep},
		} {
			if _, _, found := tc.movedPlaceCovering(read.place); !found {
				t.Errorf("reading %s was allowed after `o.inner` moved", read.name)
			}
		}

		// The row that separates a real implementation from one that simply
		// invalidates the whole binding: a SIBLING is still there.
		if _, _, found := tc.movedPlaceCovering(label); found {
			t.Errorf("reading a sibling field was rejected after `o.inner` moved")
		}
	})

	t.Run("a nested move leaves both its sibling and the outer sibling readable", func(t *testing.T) {
		tc := &typeChecker{}
		tc.markPlaceMoved(innerDeep, source.Span{Start: 10, End: 15})

		if _, _, found := tc.movedPlaceCovering(innerOther); found {
			t.Errorf("`o.inner.other` was rejected after only `o.inner.deep` moved")
		}
		if _, _, found := tc.movedPlaceCovering(label); found {
			t.Errorf("`o.label` was rejected after only `o.inner.deep` moved")
		}
		// ...while everything on the path to it is no longer whole.
		if _, _, found := tc.movedPlaceCovering(inner); !found {
			t.Errorf("`o.inner` was readable after `o.inner.deep` moved out of it")
		}
		if _, _, found := tc.movedPlaceCovering(whole); !found {
			t.Errorf("`o` was readable as a whole after `o.inner.deep` moved")
		}
	})

	t.Run("a whole move covers every place under it", func(t *testing.T) {
		tc := &typeChecker{}
		tc.markBindingMoved(o, source.Span{Start: 1, End: 2})

		for _, place := range []Place{whole, inner, label, innerDeep} {
			if _, _, found := tc.movedPlaceCovering(place); !found {
				t.Errorf("a place under a wholly moved binding read as live: %v", place)
			}
		}
	})

	t.Run("an untouched binding covers nothing", func(t *testing.T) {
		tc := &typeChecker{}
		if _, _, found := tc.movedPlaceCovering(whole); found {
			t.Errorf("an untouched binding reported as moved")
		}
	})
}

// The reported site has to be the same on every run. The moved-set is a map, so
// a naive scan returns whichever overlap the runtime happened to yield first,
// and two moved places covering one read is the normal case once fields move
// independently.
func TestMovedPlaceCoveringIsDeterministic(t *testing.T) {
	bt := NewBorrowTable()
	o := symbols.SymbolID(1)
	field := func(name uint32) PlaceSegment {
		return PlaceSegment{Kind: PlaceSegmentField, Name: source.StringID(name)}
	}

	early := source.Span{Start: 5, End: 6}
	late := source.Span{Start: 50, End: 60}

	for attempt := range 64 {
		tc := &typeChecker{}
		// Two different fields moved at different points; reading the whole
		// container is covered by both.
		tc.markPlaceMoved(bt.CanonicalPlace(o, []PlaceSegment{field(1)}), late)
		tc.markPlaceMoved(bt.CanonicalPlace(o, []PlaceSegment{field(2)}), early)

		_, span, found := tc.movedPlaceCovering(wholePlace(o))
		if !found {
			t.Fatalf("the container was readable although two of its fields had moved")
		}
		if span != early {
			t.Fatalf("attempt %d reported the later move (%v); the earliest is the stable choice", attempt, span)
		}
	}
}

// An EXACT move of the place being read wins over a wider one that also covers
// it: that is the span naming what the reader actually asked for.
func TestMovedPlaceCoveringPrefersTheExactMove(t *testing.T) {
	bt := NewBorrowTable()
	o := symbols.SymbolID(1)
	inner := bt.CanonicalPlace(o, []PlaceSegment{{Kind: PlaceSegmentField, Name: source.StringID(1)}})

	tc := &typeChecker{}
	wholeSpan := source.Span{Start: 1, End: 2}
	innerSpan := source.Span{Start: 30, End: 31}
	tc.markBindingMoved(o, wholeSpan)
	tc.markPlaceMoved(inner, innerSpan)

	got, span, found := tc.movedPlaceCovering(inner)
	if !found || got != inner || span != innerSpan {
		t.Fatalf("reading `o.inner` reported %v at %v; want the exact move at %v", got, span, innerSpan)
	}
}

// Reading a FIELD of a wholly moved binding must still be an error, and this is
// the one existing behaviour whose reporting path changed: the base of a
// projection is no longer checked as a value read, so the projection has to ask
// on its behalf. No corpus program writes this shape, so the corpus staying
// identical proves nothing about it.
func TestFieldReadOfWhollyMovedBindingIsRejected(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Foo = { bar: string, n: int }
fn take(f: Foo) -> int { return 1; }
fn f(x: Foo) -> int {
	let _ = take(x);
	return x.n;
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !hasCode(semaBag, diag.SemaUseAfterMove) {
		t.Fatalf("reading a field of a moved binding was accepted: %s", diagnosticsSummary(semaBag))
	}
}

// The other half of that change: an index operand is an ordinary value read
// that happens to sit inside a projection, and suppressing checks for the base
// chain must not reach it.
func TestIndexOperandInsideProjectionIsStillChecked(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Holder = { items: int[] }
fn take(s: string) -> int { return 1; }
fn f(h: Holder, k: string) -> int {
	let _ = take(k);
	return h.items[k.__len() to int];
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !hasCode(semaBag, diag.SemaUseAfterMove) {
		t.Fatalf("a moved value used as an index operand was accepted: %s", diagnosticsSummary(semaBag))
	}
}

// A method call's receiver goes through member typing but names a METHOD, not
// a field. The place-aware path must not claim `x.describe` was moved — the
// readable answer is that `x` was — and this pins that the call path keeps the
// binding-level wording.
func TestMethodCallOnMovedBindingReportsTheBinding(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Foo = { bar: string, n: int }
extern<Foo> {
    fn describe(self: &Foo) -> int { return self.n; }
}
fn take(f: Foo) -> int { return 1; }
fn f(x: Foo) -> int {
	let _ = take(x);
	return x.describe();
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !hasCode(semaBag, diag.SemaUseAfterMove) {
		t.Fatalf("a method call on a moved binding was accepted: %s", diagnosticsSummary(semaBag))
	}
	if got := diagnosticsSummary(semaBag); !strings.Contains(got, "'x'") {
		t.Fatalf("expected the diagnostic to name the binding `x`, got: %s", got)
	}
}

// A dereference is a projection and participates in the same relation. There is
// no program-level test to write for it while the partial-move gate is up — a
// deref place cannot be moved out of from source — so the relation is checked
// where it is decided.
func TestMovedPlaceCoveringHandlesDeref(t *testing.T) {
	bt := NewBorrowTable()
	p := symbols.SymbolID(1)
	deref := bt.CanonicalPlace(p, []PlaceSegment{{Kind: PlaceSegmentDeref}})
	derefField := bt.CanonicalPlace(p, []PlaceSegment{
		{Kind: PlaceSegmentDeref},
		{Kind: PlaceSegmentField, Name: source.StringID(1)},
	})
	otherField := bt.CanonicalPlace(p, []PlaceSegment{
		{Kind: PlaceSegmentDeref},
		{Kind: PlaceSegmentField, Name: source.StringID(2)},
	})

	tc := &typeChecker{}
	tc.markPlaceMoved(derefField, source.Span{Start: 1, End: 2})

	if _, _, found := tc.movedPlaceCovering(deref); !found {
		t.Errorf("`*p` read as whole after `(*p).field` moved out of it")
	}
	if _, _, found := tc.movedPlaceCovering(wholePlace(p)); !found {
		t.Errorf("`p` read as whole after a place under it moved")
	}
	if _, _, found := tc.movedPlaceCovering(otherField); found {
		t.Errorf("a sibling under the same deref was rejected")
	}
}

// The tie-break has to be TOTAL, not merely usually-stable. Two moved places
// covering one read, at spans that tie exactly, must still resolve to the same
// candidate on every run — spans do tie, because synthesized reads share them.
func TestMovedPlaceCoveringTieBreakIsTotal(t *testing.T) {
	bt := NewBorrowTable()
	o := symbols.SymbolID(1)
	field := func(name uint32) PlaceSegment {
		return PlaceSegment{Kind: PlaceSegmentField, Name: source.StringID(name)}
	}
	same := source.Span{Start: 7, End: 8}

	var first Place
	for attempt := range 64 {
		tc := &typeChecker{}
		// Same span, same path length: only the path key can separate them.
		tc.markPlaceMoved(bt.CanonicalPlace(o, []PlaceSegment{field(9)}), same)
		tc.markPlaceMoved(bt.CanonicalPlace(o, []PlaceSegment{field(4)}), same)

		got, _, found := tc.movedPlaceCovering(wholePlace(o))
		if !found {
			t.Fatalf("the container was readable although two fields had moved")
		}
		if attempt == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("attempt %d chose %v; attempt 0 chose %v — the order is not total", attempt, got, first)
		}
	}
}
