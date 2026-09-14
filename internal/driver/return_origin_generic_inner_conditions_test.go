package driver

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/types"
)

// These strict refusals preserve the original owned relay's real current
// caller. Unrelated symbolic-content Pending never satisfies either oracle.
func TestAnalyzeGenericInnerBorrowedStateAuthority(t *testing.T) {
	for _, name := range []string{"missing_inner_use", "rebound_inner_caller"} {
		t.Run(name, func(t *testing.T) {
			admitted := false
			t.Cleanup(func() {
				if !admitted {
					t.Log("PRECONDITION: intact source/current authority not established; mutation is not semantic evidence")
				}
			})
			var tc genericP0Case
			for _, candidate := range genericP0Cases() {
				if candidate.name == "owned_relay" {
					tc = candidate
				}
			}
			const digest = "f8264078da9ad8c3c8e89efeabe7c3453fa9b3892638b98590f28336f5a6b75f"
			if tc.digest != digest || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.text))) != digest {
				t.Fatal("PRECONDITION: admitted owned relay source changed")
			}
			logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_inner_source", "case": name, "source": tc.text, "sha256": digest})
			f := originalGenericSignatureFixture(t, tc.text, false, false)
			run := originalGenericSignatureCandidate(t, f.owner, f.authority, f.unit, "run")
			relay := originalGenericSignatureCandidate(t, f.owner, f.authority, f.unit, "relay")
			genericP0Graph(t, tc, f)
			genericP0Generator(t, f.owner, run)
			original := f.authority.InstantiationClosure
			raw, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			beforeHash := fmt.Sprintf("%x", sha256.Sum256(raw))
			index := -1
			for i, use := range original.UseSites {
				if use.Caller != (sema.InstanceKey{}) {
					if index != -1 {
						t.Fatal("PRECONDITION: expected one current inner relay call")
					}
					index = i
				}
			}
			if index < 0 {
				t.Fatal("PRECONDITION: missing original current inner use")
			}
			use := original.UseSites[index]
			callee, present := original.Lookup(use.Callee)
			caller, callerPresent := original.Lookup(use.Caller)
			inner := source.Span{File: f.owner.File.ID, Start: 85, End: 96}
			root := source.Span{File: f.owner.File.ID, Start: 166, End: 187}
			if !present || !callerPresent || use.CalleeTemplate != run.Symbol || use.CallerTemplate != relay.Symbol ||
				use.Site != inner || use.SourceKey != f.unit.SourceKey || callee.Witness.Site != root || callee.Witness.SourceKey != f.unit.SourceKey ||
				!slices.Equal(use.CallerTemplateArgs, []types.TypeID{f.authority.TypeInterner.Builtins().String}) ||
				!slices.Equal(use.CallerTemplateArgs, caller.TemplateArgs) || !slices.Equal(use.TemplateArgs, callee.TemplateArgs) {
				t.Fatal("PRECONDITION: exact inner call/root witness or concrete caller binding changed")
			}
			want := sema.ReturnOriginPending{SourceKey: f.unit.SourceKey, Span: inner, Reason: "generic call lacks its finalized concrete use"}
			if name == "rebound_inner_caller" {
				want.Span, want.Reason = inner, "generic caller: generic use disagrees with its finalized callee instance"
			}
			before, beforeErr := sema.AnalyzeReturnOrigins(t.Context(), f.authority, f.inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_inner_intact", "case": name, "analysis": before,
				"analysis_error": errorReturnOriginCallText(beforeErr), "original_closure_sha256": beforeHash, "original_use": use, "expected_pending": want})
			if beforeErr != nil || before == nil || slices.Contains(before.Pending, want) {
				t.Fatal("PRECONDITION: intact authority failed analysis or already reports this exact corruption")
			}
			var closure sema.InstantiationClosure
			if err := json.Unmarshal(raw, &closure); err != nil {
				t.Fatal(err)
			}
			if name == "missing_inner_use" {
				// Keep the real caller/outer root, but remove both the inner use
				// and callee instance so the old orphan-instance guard cannot pass.
				closure.UseSites = slices.Delete(closure.UseSites, index, index+1)
				calleeIndex := slices.IndexFunc(closure.Instances, func(instance sema.InstantiationInstance) bool { return instance.Key == use.Callee })
				if calleeIndex < 0 || use.Caller == use.Callee {
					t.Fatal("PRECONDITION: distinct inner callee instance missing")
				}
				closure.Instances = slices.Delete(closure.Instances, calleeIndex, calleeIndex+1)
				if len(closure.Instances) != 1 || closure.Instances[0].Key != use.Caller || len(closure.UseSites) != 1 ||
					closure.UseSites[0].Caller != (sema.InstanceKey{}) || closure.UseSites[0].Callee != use.Caller || closure.UseSites[0].Site != root {
					t.Fatal("PRECONDITION: mutation did not retain the original outer use/current caller")
				}
			} else {
				replacement := f.authority.TypeInterner.Builtins().Int64
				typ, ok := f.authority.TypeInterner.Lookup(replacement)
				if !ok || typ.Kind != types.KindInt || typ.Width != types.Width64 || replacement == use.CallerTemplateArgs[0] {
					t.Fatal("PRECONDITION: caller mutation needs the existing incompatible int64 descriptor")
				}
				closure.UseSites[index].CallerTemplateArgs = []types.TypeID{replacement}
			}
			changed := *f.authority
			changed.InstantiationClosure = &closure
			units := slices.Clone(f.inputs.units)
			units[0].Sema = &changed
			admitted = true
			after, afterErr := sema.AnalyzeReturnOrigins(t.Context(), &changed, units)
			retained, marshalErr := json.Marshal(original)
			afterHash := fmt.Sprintf("%x", sha256.Sum256(retained))
			logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_inner_mutated", "case": name, "admission_complete": true,
				"changed_closure": closure, "analysis": after, "analysis_error": errorReturnOriginCallText(afterErr), "expected_pending": want,
				"original_closure_sha256_before": beforeHash, "original_closure_sha256_after": afterHash, "diagnostics": f.owner.Bag.Items()})
			if marshalErr != nil || !slices.Equal(raw, retained) || beforeHash != afterHash {
				t.Fatal("original closure changed during detached authority analysis")
			}
			if afterErr != nil || after == nil || after.Complete() || !slices.Contains(after.Pending, want) {
				t.Fatalf("missing exact inner-authority refusal %+v; unrelated Pending is not proof (error=%v)", want, afterErr)
			}
		})
	}
}
