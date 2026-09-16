package driver

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"surge/internal/sema"
	"surge/internal/types"
)

// A root call of core HasLength's __len on &T is not admitted (SEM3005), so the
// contract, its implementation and the generic caller are all local.
const deferredMethodAuthoritySource = "contract Sized<T> {\n    fn __size(self: &T) -> uint;\n}\ntype Parcel = { n: uint };\nextern<Parcel> {\n    fn __size(self: &Parcel) -> uint {\n        return 1:uint;\n    }\n}\nfn size<T: Sized<T>>(x: &T, n: uint) -> uint {\n    return n;\n    return x.__size();\n}\nfn probe(p: &Parcel) -> uint {\n    return size(p, 1:uint);\n}\nfn hold(v: Option<&string>) -> nothing {\n    return nothing;\n}\n"

// Existing Pending cannot prove that a particular deferred method authority
// record was checked. Each mutation needs its own exact source-site refusal at
// the unvisited root call, which body flow never reaches.
func TestAnalyzeTypedDeferredMethodAuthority(t *testing.T) {
	site := originSpan{241, 251, "x.__size()"}
	for _, tc := range []struct{ name, reason string }{
		{"missing_original_use", "deferred method lacks its original typed use"},
		{"missing_resolved_outcome", "deferred method lacks its finalized outcome"},
		{"duplicate_resolution", "deferred method has duplicate finalized outcomes"},
		{"receiver_binding", "deferred method outcome disagrees with its caller binding"},
		{"effectful_params", "deferred method may change reference-bearing or callable contents"},
		{"orphan_outcome", "deferred method outcome lacks its unique original edge"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkOriginSource(t, deferredMethodAuthoritySource, "3eebe68b8fde9ac7589595fe5dcee09a56f910a771329ee8f308ede86f393a5f", site)
			res := returnOriginStdlibFixture(t, deferredMethodAuthoritySource, false)
			closureErr := FinalizeInstantiationClosure(t.Context(), res, 64)
			inputs, unitsErr := collectReturnOriginUnits(res)
			if closureErr != nil || unitsErr != nil || len(inputs.units) != 11 || res.Sema.InstantiationIdentity == nil || res.Sema.InstantiationClosure == nil {
				t.Fatalf("PRECONDITION: missing full typed input or finalized authority: closure=%v units=%v", closureErr, unitsErr)
			}
			checkReturnOriginStdlibBags(t, res, false)
			rootKey, _ := checkReturnOriginCloneUnits(t, res, inputs.units)
			ref, edge, outcome, optional := deferredMethodAuthority(t, res, rootKey, site)
			want := sema.ReturnOriginPending{SourceKey: rootKey, Span: edge.Witness.Site, Reason: tc.reason}
			original, originalUses := res.Sema.InstantiationClosure, res.Sema.DeferredCallableUses
			snapshot := func(closure *sema.InstantiationClosure, uses map[sema.DeferredUseRef]sema.DeferredUseID) []byte {
				raw, err := json.Marshal(map[string]any{"closure": closure, "uses": slices.Sorted(maps.Values(uses)),
					"deferred": res.Sema.InstantiationGraph.DeferredCallables(), "expr_types": res.Sema.ExprTypes})
				if err != nil {
					t.Fatal(err)
				}
				return raw
			}
			originalRaw := snapshot(original, originalUses)
			before, beforeErr := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "before": before, "before_error": errorReturnOriginCallText(beforeErr)})
			if !slices.Equal(originalRaw, snapshot(original, originalUses)) {
				t.Fatal("intact analysis mutated original authority or typed maps")
			}
			if beforeErr != nil || before == nil || slices.Contains(before.Pending, want) {
				t.Fatal("PRECONDITION: intact input failed analysis or already reported the selected corruption")
			}
			raw, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var changed sema.InstantiationClosure
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			res.Sema.InstantiationClosure = &changed
			res.Sema.DeferredCallableUses = maps.Clone(originalUses)
			switch tc.name {
			case "missing_original_use":
				delete(res.Sema.DeferredCallableUses, ref)
			case "missing_resolved_outcome":
				changed.ResolvedDeferredCalls = slices.Delete(changed.ResolvedDeferredCalls, outcome, outcome+1)
			case "duplicate_resolution":
				changed.ResolvedDeferredCalls = append(changed.ResolvedDeferredCalls, sema.CloneResolvedDeferredCallForConsumer(&original.ResolvedDeferredCalls[outcome]))
			case "receiver_binding":
				changed.ResolvedDeferredCalls[outcome].Receiver = optional
			case "effectful_params":
				changed.ResolvedDeferredCalls[outcome].CalleeParamTypes[0] = optional
			case "orphan_outcome":
				changed.ResolvedDeferredCalls[outcome].UseID = "origin-unknown/0:0/1/0"
			}
			changedRaw := snapshot(&changed, res.Sema.DeferredCallableUses)
			after, afterErr := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "expected_pending": want, "mutated_input": json.RawMessage(changedRaw),
				"after": after, "after_error": errorReturnOriginCallText(afterErr)})
			if !slices.Equal(originalRaw, snapshot(original, originalUses)) || !slices.Equal(changedRaw, snapshot(&changed, res.Sema.DeferredCallableUses)) {
				t.Fatal("corrupted-input analysis mutated the original or detached input snapshot")
			}
			if afterErr != nil || after == nil || after.Complete() || !slices.Contains(after.Pending, want) {
				t.Fatalf("missing exact deferred method authority refusal %+v; unrelated Pending is not proof: analysis=%+v error=%v", want, after, afterErr)
			}
		})
	}
}

// deferredMethodAuthority finds the one root method edge at the frozen site,
// its typed use, its one resolved size<Parcel> outcome and hold's Option<&string>.
func deferredMethodAuthority(t *testing.T, res *DiagnoseResult, rootKey string, site originSpan) (sema.DeferredUseRef, sema.DeferredCallableEdge, int, types.TypeID) {
	t.Helper()
	var edges []sema.DeferredCallableEdge
	for _, edge := range res.Sema.InstantiationGraph.DeferredCallables() {
		if edge.Kind == sema.DeferredMethodCall && edge.Witness.SourceKey == rootKey && int(edge.Witness.Site.Start) == site.start && int(edge.Witness.Site.End) == site.end {
			edges = append(edges, edge)
		}
	}
	if len(edges) != 1 {
		t.Fatalf("PRECONDITION: %d root method edges at %q", len(edges), site.snippet)
	}
	edge := edges[0]
	var ref sema.DeferredUseRef
	refs := 0
	for key, use := range res.Sema.DeferredCallableUses {
		if key.Kind == sema.DeferredMethodCall && use == edge.UseID {
			ref, refs = key, refs+1
		}
	}
	outcome, outcomes := -1, 0
	for i, call := range res.Sema.InstantiationClosure.ResolvedDeferredCalls {
		if call.Kind == sema.DeferredMethodCall && call.UseID == edge.UseID {
			outcome, outcomes = i, outcomes+1
		}
	}
	var optional types.TypeID
	for _, candidate := range res.Sema.CallableCandidates {
		if candidate.Name == "hold" && candidate.SourceKey == rootKey && len(candidate.ParamTypes) == 1 {
			optional = candidate.ParamTypes[0]
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"edge": edge, "use_ref": ref, "uses": refs, "outcome_index": outcome, "outcomes": outcomes, "hold_param": optional})
	if refs != 1 || outcomes != 1 || optional == types.NoTypeID || res.Builder.Exprs.Get(ref.Expr) == nil || res.Builder.Exprs.Get(ref.Expr).Span != edge.Witness.Site {
		t.Fatal("PRECONDITION: the root method edge lacks its unique typed use, outcome or hold formal")
	}
	call := res.Sema.InstantiationClosure.ResolvedDeferredCalls[outcome]
	if call.Outcome != sema.DeferredCallableResolved || !call.Callee.IsValid() || len(call.CalleeParamTypes) != 1 || call.SourceKey != rootKey ||
		call.Site != edge.Witness.Site || call.StaticReceiver || len(call.Args) != 0 {
		t.Fatal("PRECONDITION: the size<Parcel> method outcome is not one resolved runtime-receiver implementation")
	}
	return ref, edge, outcome, optional
}
