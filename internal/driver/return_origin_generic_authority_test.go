package driver

import (
	"crypto/sha256"
	"encoding/json"
	"slices"
	"testing"

	"surge/internal/sema"
	"surge/internal/types"
)

// Mutate only finalized authority from the already admitted mixed-use source.
// A generic-content Pending is not evidence that missing authority was checked.
func TestAnalyzeTypedGenericReturnOriginAuthority(t *testing.T) {
	const src = "fn identity<T>(@return_source value: T) -> T { return value; }\n" +
		"fn owned_probe(value: int64) -> int64 { return identity(value); }\n" +
		"fn borrowed_probe(value: &string) -> &string { return identity::<&string>(value); }\n"
	for _, tc := range []struct{ name, reason string }{
		{"missing_use", "generic call lacks its finalized concrete use"},
		{"missing_instance", "generic use lacks its finalized callee instance"},
		{"rebound_args", "generic use disagrees with its finalized callee instance"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("RETURN_ORIGIN_GENERIC_AUTHORITY_SOURCE sha256=%x source=%q", sha256.Sum256([]byte(src)), src)
			res := returnOriginTypedFixtureWithEscapeEvidence(t, src, true)
			if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
				t.Fatalf("PRECONDITION: real closure finalization: %v", err)
			}
			inputs, err := collectReturnOriginUnits(res)
			if err != nil || len(inputs.units) != 1 || res.Sema.InstantiationClosure == nil || res.Sema.InstantiationIdentity == nil {
				t.Fatalf("PRECONDITION: real owning unit/closure missing: %v", err)
			}
			var identity sema.CallableCandidate
			var candidates []sema.CallableCandidate
			for _, candidate := range res.Sema.CallableCandidates {
				if candidate.Source.File == res.File.ID && candidate.SourceKey == inputs.units[0].SourceKey {
					candidates = append(candidates, candidate)
					if candidate.Name == "identity" {
						identity = candidate
					}
				}
			}
			if len(candidates) != 3 || len(identity.TemplateParams) != 1 {
				t.Fatal("PRECONDITION: frozen three-function source lost its actual typed candidates")
			}
			checkReturnOriginGenericInstances(t, res, identity, candidates)
			checkReturnOriginGenericPromise(t, res, identity)
			original := res.Sema.InstantiationClosure
			raw, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var mutated sema.InstantiationClosure
			if err := json.Unmarshal(raw, &mutated); err != nil {
				t.Fatal(err)
			}
			borrowed, owned := -1, -1
			for i, use := range original.UseSites {
				if use.CalleeTemplate == identity.Symbol && len(use.TemplateArgs) == 1 {
					typ, _ := res.Sema.TypeInterner.Lookup(use.TemplateArgs[0])
					if typ.Kind == types.KindReference {
						borrowed = i
					} else {
						owned = i
					}
				}
			}
			if borrowed < 0 || owned < 0 || len(original.UseSites) != 2 || len(original.Instances) != 2 {
				t.Fatal("PRECONDITION: real mixed source did not retain its two distinct uses/instances")
			}
			use := original.UseSites[borrowed]
			want := sema.ReturnOriginPending{SourceKey: use.SourceKey, Span: use.Site, Reason: tc.reason}
			before, beforeErr := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "before": before, "before_error": errorReturnOriginCallText(beforeErr),
				"original_closure": original, "original_roots": res.Sema.InstantiationGraph.Roots(), "typed_calls": res.Sema.ExprTypes})
			if beforeErr != nil || before == nil || slices.Contains(before.Pending, want) {
				t.Fatal("PRECONDITION: intact authority failed analysis or already reported the selected corruption")
			}
			switch tc.name {
			case "missing_use":
				mutated.UseSites = slices.Delete(mutated.UseSites, borrowed, borrowed+1)
			case "missing_instance":
				index := slices.IndexFunc(mutated.Instances, func(instance sema.InstantiationInstance) bool { return instance.Key == use.Callee })
				if index < 0 {
					t.Fatal("PRECONDITION: borrowed use has no original instance")
				}
				mutated.Instances = slices.Delete(mutated.Instances, index, index+1)
			case "rebound_args":
				mutated.UseSites[borrowed].TemplateArgs = slices.Clone(original.UseSites[owned].TemplateArgs)
			}
			res.Sema.InstantiationClosure = &mutated
			after, afterErr := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "mutated_closure": mutated, "expected_pending": want,
				"after": after, "after_error": errorReturnOriginCallText(afterErr), "typed_diagnostics": res.Bag.Items()})
			retained, err := json.Marshal(original)
			if err != nil || !slices.Equal(raw, retained) {
				t.Fatal("original immutable closure was modified")
			}
			if afterErr != nil || after == nil || after.Complete() || !slices.Contains(after.Pending, want) {
				t.Fatalf("missing exact generic authority refusal %v; unrelated generic Pending is not proof: analysis=%+v error=%v", want, after, afterErr)
			}
		})
	}
}
