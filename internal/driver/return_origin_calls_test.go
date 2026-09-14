package driver

import (
	"crypto/sha256"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
)

const returnOriginWrongInputMessage = "returned reference may come from parameter 'b', which is not marked @return_source"

// Single-owning-unit parse/resolve/check proofs. The Map family separately
// exercises full stdlib units; these tests cannot certify that larger closure.
func TestAnalyzeTypedReturnOriginCallContracts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		src   string
		slots []uint32
	}{
		{"known_second_narrow", `fn choose(a: &string, b: &string) -> &string { return b; }
fn probe(value: &string) -> &string {
    return { let owned: string = "local"; ret choose(&owned, value); };
}
`, []uint32{0}},
		{"opaque_all_union_local", `fn probe(f: fn(&string, &string) -> &string, value: &string) -> &string {
    return { let owned: string = "local"; ret f(value, &owned); };
}
`, nil},
		{"opaque_explicit_first_external", `fn probe(f: fn(@return_source &string, &string) -> &string, value: &string) -> &string {
    return { let owned: string = "local"; ret f(value, &owned); };
}
`, []uint32{1}},
		{"opaque_explicit_first_local", `fn probe(f: fn(@return_source &string, &string) -> &string, value: &string) -> &string {
    return { let owned: string = "local"; ret f(&owned, value); };
}
`, nil},
		{"opaque_explicit_union_local", `fn probe(f: fn(@return_source &string, @return_source &string) -> &string, value: &string) -> &string {
    return { let owned: string = "local"; ret f(value, &owned); };
}
`, nil},
		{"marked_body_narrower", `fn choose(@return_source a: &string, @return_source b: &string) -> &string { return a; }
fn probe(value: &string) -> &string {
    return { let owned: string = "local"; ret choose(value, &owned); };
}
`, []uint32{0}},
		{"marked_body_wrong_input", `fn choose(@return_source a: &string, b: &string) -> &string { return b; }
`, nil},
		{"opaque_no_inputs_not_ref_free", `fn probe(f: fn() -> &string) -> &string { return f(); }
`, nil},
		{"bodyless_explicit_first_external", `fn choose(@return_source a: &string, b: &string) -> &string;
fn probe(value: &string) -> &string {
    return { let owned: string = "local"; ret choose(value, &owned); };
}
`, []uint32{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("RETURN_ORIGIN_CONTRACT_SOURCE case=%s sha256=%x source=%q", tc.name, sha256.Sum256([]byte(tc.src)), tc.src)
			res := returnOriginTypedFixtureWithEscapeEvidence(t, tc.src, true)
			inputs, err := collectReturnOriginUnits(res)
			if err != nil || len(inputs.units) != 1 {
				t.Fatalf("PRECONDITION: expected exactly the real source unit: units=%d error=%v", len(inputs.units), err)
			}
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "source": tc.src, "analysis": analysis,
				"error": errorReturnOriginCallText(err), "typed_diagnostics": res.Bag.Items(), "units": len(inputs.units),
				"declarations": res.Sema.ReturnSourceDeclarations, "instantiations": res.Sema.ReturnSourceInstantiations})
			if err != nil || analysis == nil || len(analysis.Summaries) == 0 {
				t.Fatalf("PRECONDITION: private analysis did not produce body evidence: %v", err)
			}
			if tc.name == "opaque_no_inputs_not_ref_free" {
				summary := requireReturnOriginSummary(t, analysis, "probe")
				if analysis.Complete() || len(analysis.Pending) == 0 || !summary.Unknown || summary.NoNormalReturn || len(analysis.Diagnostics) != 0 {
					t.Fatalf("unknown no-input borrowed result was converted to a safe result/refusal: %+v", analysis)
				}
				return
			}
			if tc.name == "marked_body_wrong_input" {
				start := strings.Index(tc.src, "return b;")
				marker := strings.Index(tc.src, "@return_source")
				if !analysis.Complete() || len(analysis.Diagnostics) != 1 {
					t.Fatalf("wrong-input promise needs one completed source diagnostic: %+v", analysis)
				}
				d := analysis.Diagnostics[0]
				if d.Code != diag.SemaError || d.Severity != diag.SevError || d.Message != returnOriginWrongInputMessage ||
					int(d.Primary.Start) != start || int(d.Primary.End) != start+len("return b;") || d.Primary.File != res.File.ID || len(d.Help) == 0 {
					t.Fatalf("wrong-input mismatch is not the frozen promise diagnostic: %+v", d)
				}
				noted := false
				for _, note := range d.Notes {
					noted = noted || (note.Span.File == res.File.ID && int(note.Span.Start) == marker && int(note.Span.End) == marker+len("@return_source"))
				}
				if !noted {
					t.Fatal("wrong-input diagnostic did not identify the marked promise")
				}
				return
			}
			escape := strings.HasSuffix(tc.name, "_local")
			if escape {
				if len(analysis.Diagnostics) == 0 {
					t.Fatal("call result lost the local owner at the normal block exit")
				}
				for _, d := range analysis.Diagnostics {
					requireReturnOriginCallEscape(t, tc.src, d)
				}
				return
			}
			if !analysis.Complete() || len(analysis.Diagnostics) != 0 {
				t.Fatalf("valid external source lacks a complete clean proof: %+v", analysis)
			}
			summary := requireReturnOriginSummary(t, analysis, "probe")
			if summary.Unknown || summary.NoNormalReturn || !slices.Equal(summary.ParamSlots, tc.slots) {
				t.Fatalf("call uses the wrong formal source set: %+v", summary)
			}
			if tc.name == "marked_body_narrower" {
				body := requireReturnOriginSummary(t, analysis, "choose")
				if body.Unknown || body.NoNormalReturn || !slices.Equal(body.ParamSlots, []uint32{0}) {
					t.Fatalf("known body was replaced by its wider declared upper bound: %+v", body)
				}
			}
		})
	}
}

func requireReturnOriginCallEscape(t *testing.T, src string, d diag.Diagnostic) {
	t.Helper()
	if d.Code != diag.SemaBorrowEscapesReturn || d.Severity != diag.SevError ||
		!strings.Contains(d.Message, "'owned'") || d.Primary.Start >= d.Primary.End || int(d.Primary.End) > len(src) ||
		len(d.Notes) == 0 || len(d.Help) == 0 {
		t.Fatalf("call escape lacks the real source/owner/action: %+v", d)
	}
}

func logReturnOriginCallEvidence(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("RETURN_ORIGIN_CALL_EVIDENCE=%s", data)
}

func errorReturnOriginCallText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
