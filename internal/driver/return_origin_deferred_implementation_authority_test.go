package driver

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

const implementationAuthorityLenSource = "fn probe() -> uint {\n    let arr: int[] = [1, 2, 3];\n    let len_arr = len(arr);\n    return len_arr;\n}\nfn hold(v: &string) -> nothing {\n    return nothing;\n}\n"

// __to is the conversion operator and cannot be called as a contract method on a generic &T,
// so the argument leaf uses a user generic implementation that takes one by-value argument.
const implementationAuthorityArgSource = "contract Countable<T> {\n    fn count(self: &T, extra: uint) -> uint;\n}\ntype Bag<T> = { items: T[] };\nextern<Bag<T>> {\n    fn count(self: &Bag<T>, extra: uint) -> uint {\n        return extra;\n    }\n}\nfn total<T: Countable<T>>(x: &T) -> uint {\n    return x.count(2:uint);\n}\nfn probe(b: &Bag<int>) -> uint {\n    return total(b);\n}\nfn hold(v: &string) -> nothing {\n    return nothing;\n}\n"

// Existing Pending cannot prove that a particular finalized outcome was pinned. Each
// mutation of the outcome needs its own exact refusal at the routed use's own site.
func TestAnalyzeTypedDeferredImplementationAuthority(t *testing.T) {
	const disagrees = "deferred implementation use disagrees with its finalized outcome"
	const mismatched = "deferred implementation use disagrees with its finalized receiver or arguments"
	for _, tc := range []struct {
		name, reason, text, digest string
		argument                   bool
	}{
		{"outcome_template_args", disagrees, implementationAuthorityLenSource, "a4638890877b7b4a80ddeb96db0fb5529498aa65cddba2983218fe5801269386", false},
		{"outcome_callee", disagrees, implementationAuthorityLenSource, "a4638890877b7b4a80ddeb96db0fb5529498aa65cddba2983218fe5801269386", false},
		{"missing_outcome", "deferred implementation use lacks its finalized outcome", implementationAuthorityLenSource, "a4638890877b7b4a80ddeb96db0fb5529498aa65cddba2983218fe5801269386", false},
		{"outcome_param_types", disagrees, implementationAuthorityLenSource, "a4638890877b7b4a80ddeb96db0fb5529498aa65cddba2983218fe5801269386", false},
		{"outcome_result_type", disagrees, implementationAuthorityLenSource, "a4638890877b7b4a80ddeb96db0fb5529498aa65cddba2983218fe5801269386", false},
		{"outcome_receiver", mismatched, implementationAuthorityLenSource, "a4638890877b7b4a80ddeb96db0fb5529498aa65cddba2983218fe5801269386", false},
		// R3.1 C4, re-frozen by R3.2: a routed use that carries an argument.
		{"outcome_argument", mismatched, implementationAuthorityArgSource, "f6ddff4670dc1ef3a080ad9e6106c1de15af0ce3c813c0ce689b46c040163d72", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			site := originSpan{71, 79, "len(arr)"}
			if tc.argument {
				site = originSpan{253, 268, "x.count(2:uint)"}
			}
			checkOriginSource(t, tc.text, tc.digest, site)
			res := returnOriginStdlibFixture(t, tc.text, false)
			closureErr := FinalizeInstantiationClosure(t.Context(), res, 64)
			inputs, unitsErr := collectReturnOriginUnits(res)
			if closureErr != nil || unitsErr != nil || len(inputs.units) != 11 || res.Sema.InstantiationIdentity == nil || res.Sema.InstantiationClosure == nil {
				t.Fatalf("PRECONDITION: missing full typed input or finalized authority: closure=%v units=%v", closureErr, unitsErr)
			}
			checkReturnOriginStdlibBags(t, res, false)
			rootKey, _ := checkReturnOriginCloneUnits(t, res, inputs.units)
			useKey, useSpan := rootKey, site
			if !tc.argument {
				useKey, useSpan = "core/base.sg", originSpan{2225, 2237, "self.__len()"}
			}
			outcome, optional, lenSymbol := deferredImplementationAuthority(t, res, inputs, rootKey, useKey, useSpan)
			want := sema.ReturnOriginPending{SourceKey: useKey, Span: res.Sema.InstantiationClosure.ResolvedDeferredCalls[outcome].Site, Reason: tc.reason}
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
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "expected_pending": want, "before": before, "before_error": errorReturnOriginCallText(beforeErr)})
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
			case "outcome_template_args":
				changed.ResolvedDeferredCalls[outcome].CalleeTemplateArgs = []types.TypeID{res.Sema.TypeInterner.Builtins().String}
			case "outcome_callee":
				changed.ResolvedDeferredCalls[outcome].Callee = lenSymbol
			case "missing_outcome":
				changed.ResolvedDeferredCalls = slices.Delete(changed.ResolvedDeferredCalls, outcome, outcome+1)
			case "outcome_param_types":
				changed.ResolvedDeferredCalls[outcome].CalleeParamTypes[0] = optional
			case "outcome_result_type":
				changed.ResolvedDeferredCalls[outcome].CalleeResultType = res.Sema.TypeInterner.Builtins().String
			case "outcome_receiver":
				changed.ResolvedDeferredCalls[outcome].Receiver = optional
			case "outcome_argument":
				changed.ResolvedDeferredCalls[outcome].Args[0] = optional
			}
			changedRaw := snapshot(&changed, res.Sema.DeferredCallableUses)
			after, afterErr := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "mutated_input": json.RawMessage(changedRaw),
				"after": after, "after_error": errorReturnOriginCallText(afterErr)})
			if !slices.Equal(originalRaw, snapshot(original, originalUses)) || !slices.Equal(changedRaw, snapshot(&changed, res.Sema.DeferredCallableUses)) {
				t.Fatal("corrupted-input analysis mutated the original or detached input snapshot")
			}
			if afterErr != nil || after == nil || after.Complete() || !slices.Contains(after.Pending, want) {
				t.Fatalf("missing exact deferred implementation use refusal %+v; unrelated Pending is not proof: analysis=%+v error=%v", want, after, afterErr)
			}
		})
	}
}

// deferredImplementationAuthority returns the routed outcome's index, hold's &string formal
// and the core len declaration, after pinning the outcome's own generic-implementation shape.
func deferredImplementationAuthority(t *testing.T, res *DiagnoseResult, inputs returnOriginInputs, rootKey, useKey string, span originSpan) (int, types.TypeID, symbols.SymbolID) {
	t.Helper()
	if useKey != rootKey {
		content := ""
		for _, unit := range inputs.units {
			if unit.SourceKey == useKey {
				content = string(res.FileSet.Get(unit.Builder.Files.Get(unit.FileID).Span.File).Content)
			}
		}
		if span.end > len(content) || content[span.start:span.end] != span.snippet {
			t.Fatalf("PRECONDITION: %s %d:%d is not %q", useKey, span.start, span.end, span.snippet)
		}
	}
	outcome, outcomes := -1, 0
	for i, call := range res.Sema.InstantiationClosure.ResolvedDeferredCalls {
		if call.Kind == sema.DeferredMethodCall && call.SourceKey == useKey && int(call.Site.Start) == span.start && int(call.Site.End) == span.end {
			outcome, outcomes = i, outcomes+1
		}
	}
	var optional types.TypeID
	var lenSymbol symbols.SymbolID
	lens := 0
	for _, candidate := range res.Sema.CallableCandidates {
		if candidate.Name == "hold" && candidate.SourceKey == rootKey && len(candidate.ParamTypes) == 1 {
			optional = candidate.ParamTypes[0]
		}
		if candidate.Name == "len" && candidate.HasSelf && len(candidate.TemplateParams) == 1 {
			lenSymbol, lens = candidate.Symbol, lens+1
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"outcome_index": outcome, "outcomes": outcomes, "hold_param": optional,
		"len_candidates": lens, "use_key": useKey, "site": span})
	if outcomes != 1 || optional == types.NoTypeID || lens != 1 {
		t.Fatalf("PRECONDITION: %d routed outcomes at %s %d:%d, %d len declarations", outcomes, useKey, span.start, span.end, lens)
	}
	call := res.Sema.InstantiationClosure.ResolvedDeferredCalls[outcome]
	if call.Outcome != sema.DeferredCallableResolved || len(call.CalleeTemplateArgs) == 0 || len(call.CalleeParamTypes) == 0 || !call.Callee.IsValid() {
		t.Fatalf("PRECONDITION: the outcome at %s %d:%d is not a resolved generic implementation: %+v", useKey, span.start, span.end, call)
	}
	return outcome, optional, lenSymbol
}
