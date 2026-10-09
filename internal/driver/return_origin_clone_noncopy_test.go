package driver

import (
	"encoding/json"
	"slices"
	"testing"

	"surge/internal/sema"
	"surge/internal/types"
)

func TestAnalyzeDeferredNonCopyCloneUsesSelectedBody(t *testing.T) {
	const src = `
type Model = { text: string }

extern<Model> {
    pub fn __clone(self: &Model) -> Model {
        return Model { text = clone(self.text) };
    }
}

fn duplicate<T>(value: &T) -> T { return clone(value); }
fn probe(value: Model) -> Model { return duplicate(&value); }
`
	res := returnOriginStdlibFixture(t, src, false)
	if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
		t.Fatal(err)
	}
	inputs, err := collectReturnOriginUnits(res)
	if err != nil {
		t.Fatal(err)
	}
	rootKey, _ := checkReturnOriginCloneUnits(t, res, inputs.units)
	var duplicate sema.CallableCandidate
	for _, candidate := range res.Sema.CallableCandidates {
		if candidate.SourceKey == rootKey && candidate.Name == "duplicate" {
			duplicate = candidate
		}
	}
	if !duplicate.Symbol.IsValid() || len(duplicate.TemplateParams) != 1 {
		t.Fatal("PRECONDITION: missing original duplicate<T>")
	}
	var edge sema.DeferredCallableEdge
	for _, candidate := range res.Sema.InstantiationGraph.DeferredCallables() {
		if candidate.Kind == sema.DeferredCloneCall && candidate.Caller == duplicate.Symbol {
			if edge.UseID != "" {
				t.Fatal("PRECONDITION: duplicate clone edge")
			}
			edge = candidate
		}
	}
	if edge.UseID == "" || res.Sema.InstantiationClosure == nil {
		t.Fatal("PRECONDITION: missing finalized deferred clone")
	}
	var resolved sema.ResolvedDeferredCall
	for _, candidate := range res.Sema.InstantiationClosure.ResolvedDeferredCalls {
		if candidate.UseID == edge.UseID {
			if resolved.UseID != "" {
				t.Fatal("PRECONDITION: duplicate deferred clone outcome")
			}
			resolved = candidate
		}
	}
	if resolved.UseID == "" || resolved.Outcome != sema.DeferredCallableResolved || !resolved.Callee.IsValid() ||
		len(resolved.CalleeTemplateArgs) != 0 || len(resolved.CalleeParamTypes) != 1 || resolved.CalleeResultType != resolved.Receiver ||
		res.Sema.TypeInterner.IsCopy(resolved.Receiver) {
		t.Fatalf("PRECONDITION: selected user clone outcome changed: %+v", resolved)
	}
	self, ok := res.Sema.TypeInterner.Lookup(resolved.CalleeParamTypes[0])
	if !ok || self.Kind != types.KindReference || self.Mutable || self.Elem != resolved.Receiver {
		t.Fatalf("PRECONDITION: selected clone lost its shared receiver: %+v", resolved)
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	if err != nil || analysis == nil {
		t.Fatalf("return-origin analysis failed: %v", err)
	}
	for _, pending := range analysis.Pending {
		if pending.SourceKey == edge.Witness.SourceKey && pending.Span == edge.Witness.Site {
			t.Fatalf("selected non-Copy clone stayed unfinished: %+v", pending)
		}
	}
	summary := requireReturnOriginSummary(t, analysis, "duplicate")
	if summary.NoNormalReturn || !slices.Equal(summary.ParamSlots, []uint32{0}) {
		t.Fatalf("generic clone lost its conservative input-content summary: %+v", summary)
	}

	original := res.Sema.InstantiationClosure
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var changed sema.InstantiationClosure
	if err := json.Unmarshal(raw, &changed); err != nil {
		t.Fatal(err)
	}
	for i := range changed.ResolvedDeferredCalls {
		if changed.ResolvedDeferredCalls[i].UseID == edge.UseID {
			changed.ResolvedDeferredCalls[i].CalleeKey += "/foreign"
		}
	}
	res.Sema.InstantiationClosure = &changed
	t.Cleanup(func() { res.Sema.InstantiationClosure = original })
	broken, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	want := sema.ReturnOriginPending{SourceKey: edge.Witness.SourceKey, Span: edge.Witness.Site,
		Reason: "deferred clone needs its selected non-Copy body and effect transfer"}
	if err != nil || broken == nil || !slices.Contains(broken.Pending, want) {
		t.Fatalf("foreign selected body escaped the exact identity fence: analysis=%+v error=%v", broken, err)
	}
}

func TestAnalyzeDeferredNonCopyCloneUsesIntrinsicBodylessDeclaration(t *testing.T) {
	const src = "fn duplicate<T>(value: T) -> T { return clone(&value); }\n" +
		"fn probe(value: string) -> string { return duplicate(value); }\n"
	res := returnOriginStdlibFixture(t, src, false)
	if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
		t.Fatal(err)
	}
	inputs, err := collectReturnOriginUnits(res)
	if err != nil {
		t.Fatal(err)
	}
	rootKey, _ := checkReturnOriginCloneUnits(t, res, inputs.units)
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	if err != nil || analysis == nil {
		t.Fatalf("return-origin analysis failed: %v", err)
	}
	for _, pending := range analysis.Pending {
		if pending.SourceKey == rootKey {
			t.Fatalf("selected intrinsic string clone stayed unfinished: %+v", pending)
		}
	}
	summary := requireReturnOriginSummary(t, analysis, "duplicate")
	if summary.Unknown || summary.NoNormalReturn || !slices.Equal(summary.ParamSlots, []uint32{0}) {
		t.Fatalf("intrinsic clone lost its conservative input-content summary: %+v", summary)
	}
}
