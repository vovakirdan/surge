package sema

import (
	"strings"
	"testing"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// The residual plan says what a PARTIALLY-MOVED binding still has to reclaim.
// The gate keeps these states out of reach of source, so the builder is
// exercised directly — and it is the piece that decides whether a residual drop
// is correct, so it is where the rows belong.
//
// Reading the plans: a step with no Shallow flag is a deep drop of that place;
// a Shallow step frees only that place's own storage. Order is post-order —
// contents before the container that holds them — and live siblings keep
// reverse declaration order, which is what the whole-value glue already does.
func TestResidualDropPlan(t *testing.T) {
	// type Inner = { deep: string, other: string }
	// type Outer = { inner: Inner, label: string }
	typesIn := types.NewInterner()
	typesIn.Strings = source.NewInterner()
	strID := typesIn.Builtins().String

	name := func(s string) source.StringID { return typesIn.Strings.Intern(s) }
	innerTy := typesIn.RegisterStruct(name("Inner"), source.Span{})
	typesIn.SetStructFields(innerTy, []types.StructField{
		{Name: name("deep"), Type: strID},
		{Name: name("other"), Type: strID},
	})
	outerTy := typesIn.RegisterStruct(name("Outer"), source.Span{})
	typesIn.SetStructFields(outerTy, []types.StructField{
		{Name: name("inner"), Type: innerTy},
		{Name: name("label"), Type: strID},
	})

	o := symbols.SymbolID(1)
	newChecker := func() *typeChecker {
		tc := &typeChecker{types: typesIn, borrow: NewBorrowTable()}
		tc.bindingTypes = map[symbols.SymbolID]types.TypeID{o: outerTy}
		return tc
	}
	at := func(tc *typeChecker, segs ...string) Place {
		path := make([]PlaceSegment, 0, len(segs))
		for _, s := range segs {
			path = append(path, PlaceSegment{Kind: PlaceSegmentField, Name: name(s)})
		}
		return tc.canonicalPlace(placeDescriptor{Base: o, Segments: path})
	}
	span := source.Span{Start: 1, End: 2}

	t.Run("nothing moved needs no plan", func(t *testing.T) {
		tc := newChecker()
		if steps := tc.residualDropPlan(o); steps != nil {
			t.Fatalf("an untouched binding got a residual plan: %v", steps)
		}
	})

	t.Run("wholly moved needs no plan", func(t *testing.T) {
		tc := newChecker()
		tc.markBindingMoved(o, span)
		if steps := tc.residualDropPlan(o); steps != nil {
			t.Fatalf("a wholly moved binding got a residual plan: %v", steps)
		}
	})

	t.Run("one field moved leaves the sibling and the container's own storage", func(t *testing.T) {
		tc := newChecker()
		tc.markPlaceMoved(at(tc, "inner"), span)

		got := renderPlan(t, tc.residualDropPlan(o), typesIn)
		want := []string{"drop o.label", "free o"}
		assertPlan(t, got, want)
	})

	t.Run("a nested move keeps the sibling under it", func(t *testing.T) {
		tc := newChecker()
		tc.markPlaceMoved(at(tc, "inner", "deep"), span)

		got := renderPlan(t, tc.residualDropPlan(o), typesIn)
		// `o.inner.other` survives and must be reclaimed before the box that
		// holds it; `o.label` survives too; both boxes go last, innermost first.
		want := []string{"drop o.label", "drop o.inner.other", "free o.inner", "free o"}
		assertPlan(t, got, want)
	})

	t.Run("every field moved still frees the container", func(t *testing.T) {
		tc := newChecker()
		tc.markPlaceMoved(at(tc, "inner"), span)
		tc.markPlaceMoved(at(tc, "label"), span)

		got := renderPlan(t, tc.residualDropPlan(o), typesIn)
		// Nothing left inside, but the container's own box is still this
		// binding's to release — forgetting it is a leak per value.
		want := []string{"free o"}
		assertPlan(t, got, want)
	})
}

// A plan that cannot be expressed must be refused rather than guessed. Emitting
// a shallow free for a container whose parts cannot be enumerated would abandon
// whatever is still live inside it.
func TestResidualDropPlanRefusesWhatItCannotEnumerate(t *testing.T) {
	typesIn := types.NewInterner()
	strID := typesIn.Builtins().String
	arrTy := typesIn.Intern(types.MakeArray(strID, 4))

	a := symbols.SymbolID(1)
	tc := &typeChecker{types: typesIn, borrow: NewBorrowTable()}
	tc.bindingTypes = map[symbols.SymbolID]types.TypeID{a: arrTy}

	elem := tc.canonicalPlace(placeDescriptor{
		Base:     a,
		Segments: []PlaceSegment{{Kind: PlaceSegmentIndex}},
	})
	tc.markPlaceMoved(elem, source.Span{Start: 1, End: 2})

	if steps := tc.residualDropPlan(a); steps != nil {
		t.Fatalf("a plan was produced for a shape whose parts cannot be enumerated: %v", steps)
	}
}

func renderPlan(t *testing.T, steps []DropStep, typesIn *types.Interner) []string {
	t.Helper()
	out := make([]string, 0, len(steps))
	for _, step := range steps {
		var b strings.Builder
		if step.Shallow {
			b.WriteString("free o")
		} else {
			b.WriteString("drop o")
		}
		for _, seg := range step.Path {
			b.WriteByte('.')
			b.WriteString(typesIn.Strings.MustLookup(seg.Name))
		}
		out = append(out, b.String())
	}
	return out
}

func assertPlan(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("plan = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("plan = %v, want %v", got, want)
		}
	}
}
