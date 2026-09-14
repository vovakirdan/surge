package driver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
)

// These call the private analyzer on actual public-pipeline typed artifacts.
// They do not claim the public driver already invokes it before publishing HIR.
func TestAnalyzeTypedReturnOrigins(t *testing.T) {
	for _, tc := range returnOriginPublicCases() {
		t.Run(tc.name, func(t *testing.T) {
			analysis := analyzeTypedReturnOriginSource(t, tc.src, tc.escapes)
			if !tc.escapes {
				if !analysis.Complete() || len(analysis.Diagnostics) != 0 {
					t.Fatalf("covered external source must have a complete clean proof: %+v", analysis)
				}
				return
			}
			if len(analysis.Diagnostics) == 0 {
				t.Fatal("new analyzer failed to diagnose escaping owner; an old sema diagnostic does not count")
			}
			for _, d := range analysis.Diagnostics {
				if d.Code != diag.SemaBorrowEscapesReturn || d.Severity != diag.SevError ||
					d.Primary.Start >= d.Primary.End || int(d.Primary.End) > len(tc.src) || len(d.Notes) == 0 || len(d.Help) == 0 {
					t.Fatalf("escape diagnostic lacks its source/owner/action: %+v", d)
				}
			}
		})
	}
}

func TestAnalyzeTypedReturnOriginsRecursiveSummaries(t *testing.T) {
	const src = `fn first(flag: bool, a: &string, b: &string) -> &string {
    if flag { return a; }
    return second(true, a, b);
}
fn second(flag: bool, a: &string, b: &string) -> &string {
    if flag { return b; }
    return first(true, a, b);
}
`
	analysis := analyzeTypedReturnOriginSource(t, src, false)
	if !analysis.Complete() || len(analysis.Diagnostics) != 0 {
		t.Fatalf("recursive body proof is incomplete: %+v", analysis)
	}
	for _, name := range []string{"first", "second"} {
		summary := requireReturnOriginSummary(t, analysis, name)
		if summary.NoNormalReturn || summary.Unknown || !slices.Equal(summary.ParamSlots, []uint32{1, 2}) {
			t.Fatalf("%s published provisional or incorrect recursive sources: %+v", name, summary)
		}
	}
}

func TestAnalyzeTypedReturnOriginsDefaultSlotDoesNotShift(t *testing.T) {
	const header = `fn choose(a: &string, b: &string, ignored: int = 1) -> &string { return b; }
fn read(value: &string) -> int { return 1; }
`
	for _, tc := range []struct {
		name string
		src  string
		bad  bool
	}{
		{"local_selected", header + `fn probe() -> int {
    let outside: string = "outside";
    let escaped: &string = {
        let owned: string = "owned";
        ret choose(b: &owned, a: &outside);
    };
    return read(escaped);
}
`, true},
		{"outer_selected", header + `fn probe() -> int {
    let outside: string = "outside";
    let escaped: &string = {
        let owned: string = "owned";
        ret choose(b: &outside, a: &owned);
    };
    return read(escaped);
}
`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			analysis := analyzeTypedReturnOriginSource(t, tc.src, tc.bad)
			if (len(analysis.Diagnostics) != 0) != tc.bad || (!tc.bad && !analysis.Complete()) {
				t.Fatalf("named arguments or trailing default changed source slot 1: %+v", analysis)
			}
		})
	}
}

func TestAnalyzeTypedReturnOriginsOpaqueCallStaysPending(t *testing.T) {
	const src = `fn invoke(f: fn(&string) -> &string, value: &string) -> &string {
    return f(value);
}
`
	analysis := analyzeTypedReturnOriginSource(t, src, false)
	if analysis.Complete() || len(analysis.Pending) == 0 {
		t.Fatal("opaque callable became safe before its declaration contract was finalized")
	}
	summary := requireReturnOriginSummary(t, analysis, "invoke")
	if !summary.Unknown || summary.NoNormalReturn {
		t.Fatalf("opaque result became RefFree or nonreturning: %+v", summary)
	}
}

func TestAnalyzeTypedReturnOriginsNoReturnIsNotRefFree(t *testing.T) {
	const src = `fn never(value: &string) -> &string { while true {} return value; }
fn empty() -> nothing { return; }
`
	analysis := analyzeTypedReturnOriginSource(t, src, false)
	if !analysis.Complete() || len(analysis.Diagnostics) != 0 {
		t.Fatalf("simple control outcomes were not proven: %+v", analysis)
	}
	never := requireReturnOriginSummary(t, analysis, "never")
	empty := requireReturnOriginSummary(t, analysis, "empty")
	if !never.NoNormalReturn || never.Unknown || empty.NoNormalReturn || empty.Unknown || len(empty.ParamSlots) != 0 {
		t.Fatalf("nonreturn and normal RefFree collapsed: never=%+v empty=%+v", never, empty)
	}
}

func analyzeTypedReturnOriginSource(t *testing.T, src string, allowOldEscape bool) *sema.ReturnOriginAnalysis {
	t.Helper()
	stdlib := detectStdlibRootFrom(".")
	if stdlib == "" {
		t.Fatal("failed to locate stdlib root")
	}
	t.Setenv("SURGE_STDLIB", stdlib)
	path := filepath.Join(t.TempDir(), "origin.sg")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := DiagnoseWithOptions(t.Context(), path, &DiagnoseOptions{
		Stage: DiagnoseStageAll, MaxDiagnostics: 64, IgnoreWarnings: true, KeepArtifacts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Bag == nil || res.Builder == nil || res.Symbols == nil || res.Sema == nil ||
		res.Sema.TypeInterner == nil || len(res.Sema.ExprTypes) == 0 {
		t.Fatal("source did not reach the public typed semantic pipeline")
	}
	for _, d := range res.Bag.Items() {
		if d.Severity == diag.SevError && (!allowOldEscape || d.Code != diag.SemaBorrowEscapesReturn) {
			t.Fatalf("unrelated existing refusal invalidates source proof: %+v", *d)
		}
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, []sema.ReturnOriginUnit{{
		Builder: res.Builder, FileID: res.FileID, Sema: res.Sema, Symbols: res.Symbols, SourceKey: path,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if analysis == nil || len(analysis.Summaries) == 0 {
		t.Fatal("analyzer produced no body evidence")
	}
	encoded, err := json.Marshal(analysis)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("RETURN_ORIGIN_ANALYSIS=%s", encoded)
	return analysis
}

func requireReturnOriginSummary(t *testing.T, analysis *sema.ReturnOriginAnalysis, name string) sema.ReturnOriginSummary {
	t.Helper()
	for _, summary := range analysis.Summaries {
		if summary.Name == name {
			return summary
		}
	}
	t.Fatalf("no body summary for %s", name)
	return sema.ReturnOriginSummary{}
}
