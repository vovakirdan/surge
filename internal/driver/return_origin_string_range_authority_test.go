package driver

import (
	"crypto/sha256"
	"maps"
	"slices"
	"testing"

	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

// The real string target and Range<int> stay typed. Only the selected operation
// becomes the existing scalar overload, so native-family checks cannot mask a
// missing selected-range signature certificate.
func TestAnalyzeTypedStringRangeIndexAuthority(t *testing.T) {
	t.Logf("RETURN_ORIGIN_STRING_RANGE_SOURCE case=selected_scalar sha256=%x source=%q", sha256.Sum256([]byte(stringRangeEffectSource)), stringRangeEffectSource)
	res := returnOriginStdlibFixture(t, stringRangeEffectSource, false)
	closureErr := FinalizeInstantiationClosure(t.Context(), res, 64)
	inputs, unitsErr := collectReturnOriginUnits(res)
	logReturnOriginCallEvidence(t, map[string]any{"closure_error": errorReturnOriginCallText(closureErr),
		"unit_error": errorReturnOriginCallText(unitsErr), "closure": res.Sema.InstantiationClosure, "candidates": res.Sema.CallableCandidates})
	if closureErr != nil || unitsErr != nil || len(inputs.units) != 11 || string(res.File.Content) != stringRangeEffectSource {
		t.Fatal("PRECONDITION: frozen effect source or full finalized stdlib input is missing")
	}
	checkReturnOriginStdlibBags(t, res, false)
	checkReturnOriginCloneUnits(t, res, inputs.units)
	facts := checkReturnOriginRangeSites(t, res, inputs.units, "bound_effect_order")
	root := &inputs.units[facts.root]
	id := facts.indexes[0]
	in := res.Sema.TypeInterner
	var replacement symbols.SymbolID
	matches := 0
	for _, c := range res.Sema.CallableCandidates {
		if c.Name != "__index" || !c.Builtin || !c.Intrinsic || c.HasBody || !c.HasSelf || c.SourceKey != "builtin" || c.ModulePath != "core/intrinsics" ||
			c.ReceiverType != in.Builtins().String || len(c.ParamTypes) != 2 || c.ParamTypes[1] != in.Builtins().Int || c.ResultType != in.Builtins().Uint32 || len(c.TemplateParams) != 0 {
			continue
		}
		matches++
		aliases := root.Publication.LocalSymbols(c.Symbol)
		if len(root.Publication.RootToLocalSymbols) == 0 {
			aliases = []symbols.SymbolID{c.Symbol}
		}
		if len(aliases) != 1 || !aliases[0].IsValid() {
			t.Fatal("PRECONDITION: scalar overload has missing or ambiguous published local symbols")
		}
		replacement = aliases[0]
	}
	if matches != 1 {
		t.Fatal("PRECONDITION: replacement lacks one exact scalar string index candidate")
	}
	scalar := checkReturnOriginRangeCallable(t, res, inputs.units, *root, replacement)
	self, typed := in.Lookup(scalar.ParamTypes[0])
	if !typed || self.Kind != types.KindReference || self.Mutable || self.Elem != in.Builtins().String || scalar.Async || scalar.ReceiverTemplateArity != 0 {
		t.Fatal("PRECONDITION: original scalar index does not borrow exactly one string receiver")
	}
	original := root.Sema
	before := maps.Clone(original.IndexSymbols)
	if before[id] == replacement || !before[id].IsValid() {
		t.Fatal("PRECONDITION: original admitted range operation is not distinct from the scalar overload")
	}
	copy := *original
	copy.IndexSymbols = maps.Clone(original.IndexSymbols)
	copy.IndexSymbols[id] = replacement
	root.Sema = &copy
	effective := maps.Clone(copy.IndexSymbols)
	changed := 0
	for expr, sym := range before {
		if effective[expr] != sym {
			changed++
		}
	}
	if len(before) != len(effective) || changed != 1 || !maps.Equal(before, original.IndexSymbols) {
		t.Fatal("PRECONDITION: detached mutation changed more than its one selected operation")
	}
	want := sema.ReturnOriginPending{SourceKey: root.SourceKey, Span: root.Builder.Exprs.Get(id).Span,
		Reason: "selected string range index disagrees with its original signature"}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	logReturnOriginCallEvidence(t, map[string]any{"analysis": analysis, "analysis_error": errorReturnOriginCallText(err),
		"index": id, "scalar_candidate": scalar, "original_index_symbols": before, "detached_index_symbols": effective, "expected_pending": want})
	if !maps.Equal(before, original.IndexSymbols) || !maps.Equal(effective, copy.IndexSymbols) {
		t.Fatal("analysis mutated the original or detached selected-operation evidence")
	}
	if err != nil || analysis == nil {
		t.Fatalf("detached range authority analysis failed: %v", err)
	}
	if !slices.Contains(analysis.Pending, want) {
		t.Errorf("selected range signature mismatch lacks its exact refusal: %+v; actual=%+v", want, analysis.Pending)
	}
	summary := requireReturnOriginSummary(t, analysis, "probe")
	if summary.Source.File != res.File.ID || summary.NoNormalReturn || summary.Unknown || !slices.Equal(summary.ParamSlots, []uint32{2}) {
		t.Errorf("wrong selected signature erased ordered bound effects: %+v", summary)
	}
	if len(analysis.Diagnostics) != 0 {
		t.Errorf("detached metadata corruption became a source diagnostic: %+v", analysis.Diagnostics)
	}
}
