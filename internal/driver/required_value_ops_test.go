package driver

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"surge/internal/sema"
	"surge/internal/symbols"
)

// twoRoundRequiredOperations needs two rounds to settle, and the second round
// exists only because the first one changed what is reachable.
//
// main names Outer and nothing else, so round one requires Outer's clone. Only
// that makes carrier/outer.sg live, and Inner is named nowhere but inside the
// body it just reached, so round two is the first time Inner's clone can be
// required at all.
func twoRoundRequiredOperations() map[string]string {
	return map[string]string{
		"payload/inner.sg": `
pragma module::payload;
pub type Inner = { n: int };
extern<Inner> {
    pub fn __clone(self: &Inner) -> Inner {
        return Inner { n = self.n };
    }
}
`,
		"carrier/outer.sg": `
pragma module::carrier;
import payload::Inner;
pub type Outer = { n: int };
extern<Outer> {
    pub fn __clone(self: &Outer) -> Outer {
        let seed = Inner { n = self.n };
        return Outer { n = seed.n };
    }
}
`,
		"main.sg": `
pragma module::app;
import carrier::Outer;
@entrypoint fn main() -> int {
    let value: Outer = { n = 4 };
    return value.n;
}
`,
	}
}

func TestRequiredValueOperationsConvergeThroughASecondRound(t *testing.T) {
	result := authorityForRequiredOps(t, twoRoundRequiredOperations())
	required := requiredCloneBodyKeys(t, result.Sema)
	if len(required) != 2 {
		t.Fatalf("required clone implementations = %v, want both Outer's and Inner's", required)
	}
	if !namesReceiver(required, "Outer") || !namesReceiver(required, "Inner") {
		t.Fatalf("required clone implementations = %v, want one for Outer and one for Inner", required)
	}
	for _, key := range required {
		if !liveCloneImplementation(result.Sema, key) {
			t.Fatalf("required implementation %q never became reachable: %v", key, required)
		}
	}
}

// TestRequiredValueOperationsAreDerivedOnlyAtAnAuthoritySite pins the site
// identity rule against the same program that needs two rounds: told it is the
// whole program, the pass finds both operations; told it is not, it finds none
// and touches nothing.
func TestRequiredValueOperationsAreDerivedOnlyAtAnAuthoritySite(t *testing.T) {
	result := diagnoseRequiredOpsProject(t, twoRoundRequiredOperations())
	if err := mergeTypeAttrFactsFromRecords(result); err != nil {
		t.Fatalf("merge type attribute facts: %v", err)
	}
	if err := classifyCapabilities(result); err != nil {
		t.Fatalf("classify capabilities: %v", err)
	}
	if err := FinalizeInstantiationClosure(context.Background(), result, 64); err != nil {
		t.Fatalf("finalize closure: %v", err)
	}

	result.wholeProgramAuthority = false
	if err := reachRequiredValueOperations(result); err != nil {
		t.Fatalf("non-authority pass: %v", err)
	}
	if len(result.Sema.RequiredValueOpRoots) != 0 || result.Sema.ReachableBodyTypes != nil {
		t.Fatalf(
			"a non-authority site derived operations: roots=%v bodyTypes=%d",
			result.Sema.RequiredValueOpRoots, len(result.Sema.ReachableBodyTypes),
		)
	}

	result.wholeProgramAuthority = true
	if err := reachRequiredValueOperations(result); err != nil {
		t.Fatalf("authority pass: %v", err)
	}
	if len(result.Sema.RequiredValueOpRoots) != 2 {
		t.Fatalf("authority root inputs = %v, want both clone implementations", result.Sema.RequiredValueOpRoots)
	}
}

// TestRequiredValueOperationsReportTheirBudget exercises the bound itself. A
// program that never converges cannot be written down, but the loop is bounded
// by the accumulated demand rather than by rounds, so shrinking the budget puts
// the same check under the same pressure a divergent program would.
func TestRequiredValueOperationsReportTheirBudget(t *testing.T) {
	original := requiredValueOpBudget
	requiredValueOpBudget = 0
	t.Cleanup(func() { requiredValueOpBudget = original })

	result := diagnoseRequiredOpsProject(t, twoRoundRequiredOperations())
	if err := mergeTypeAttrFactsFromRecords(result); err != nil {
		t.Fatalf("merge type attribute facts: %v", err)
	}
	if err := classifyCapabilities(result); err != nil {
		t.Fatalf("classify capabilities: %v", err)
	}
	if err := FinalizeInstantiationClosure(context.Background(), result, 64); err != nil {
		t.Fatalf("finalize closure: %v", err)
	}
	result.wholeProgramAuthority = true

	err := reachRequiredValueOperations(result)
	if err == nil {
		t.Fatal("an exhausted budget was accepted")
	}
	if !strings.Contains(err.Error(), "budget") || !strings.Contains(err.Error(), "converge") {
		t.Fatalf("budget error does not say what ran out or what was expected: %v", err)
	}
}

// TestRequiredValueOperationsAgreeAcrossBuildAndDiagnose is the parity gate.
// The two authority sites reach the same program by different routes, and an
// implementation required on one route only would make a build and an editor
// disagree about what a program contains.
//
// The comparison is per module, because that is the question each route
// answers: a build asks what the root module needs, and the record path asks
// the same of every module it finalizes. So the root module's answer is what
// has to match.
func TestRequiredValueOperationsAgreeAcrossBuildAndDiagnose(t *testing.T) {
	root := writeRequiredOpsProject(t, twoRoundRequiredOperations())

	building := diagnoseRequiredOpsProjectAt(t, root)
	if _, err := CombineHIRWithModules(context.Background(), building); err != nil {
		t.Fatalf("combine HIR: %v", err)
	}
	built := requiredCloneBodyKeys(t, building.Sema)
	if len(built) == 0 {
		t.Fatal("the build route required nothing, so parity would prove nothing")
	}

	_, results, err := DiagnoseDirWithOptions(context.Background(), root, &DiagnoseOptions{
		Stage: DiagnoseStageAll, MaxDiagnostics: 64, KeepArtifacts: true,
	}, 1)
	if err != nil {
		t.Fatalf("diagnose directory: %v", err)
	}
	rootModule := (*sema.Result)(nil)
	for i := range results {
		if filepath.Base(results[i].Path) == "main.sg" && results[i].Sema != nil {
			rootModule = results[i].Sema
		}
	}
	if rootModule == nil {
		t.Fatal("directory diagnose retained no result for the root module")
	}
	if diagnosed := requiredCloneBodyKeys(t, rootModule); !slices.Equal(built, diagnosed) {
		t.Fatalf("build and diagnose disagree on required implementations:\nbuild    %v\ndiagnose %v", built, diagnosed)
	}
}

// requiredCloneBodyKeys names the implementations one finalized result made
// reachable through a required operation, spelled by canonical body key so two
// compilations with different symbol numbering stay comparable.
func requiredCloneBodyKeys(t *testing.T, result *sema.Result) []string {
	t.Helper()
	keys := make([]string, 0, len(result.RequiredValueOpRoots))
	for symbol := range result.RequiredValueOpRoots {
		key := cloneBodyKeyForSymbol(result, symbol)
		if key == "" {
			t.Fatalf("required root %d names no callable in the merged catalog", symbol)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneBodyKeyForSymbol(result *sema.Result, symbol symbols.SymbolID) string {
	for i := range result.CallableCandidates {
		if result.CallableCandidates[i].Symbol == symbol {
			return result.CallableCandidates[i].BodyKey
		}
	}
	return ""
}

func liveCloneImplementation(result *sema.Result, bodyKey string) bool {
	for i := range result.CallableCandidates {
		candidate := &result.CallableCandidates[i]
		if candidate.BodyKey != bodyKey {
			continue
		}
		if slices.Contains(result.InstantiationClosure.LiveCallables, candidate.Symbol) {
			return true
		}
	}
	return false
}

func namesReceiver(bodyKeys []string, receiver string) bool {
	for _, key := range bodyKeys {
		if strings.Contains(key, "|"+receiver+"|") {
			return true
		}
	}
	return false
}

func authorityForRequiredOps(t *testing.T, files map[string]string) *DiagnoseResult {
	t.Helper()
	result := diagnoseRequiredOpsProject(t, files)
	if _, err := CombineHIRWithModules(context.Background(), result); err != nil {
		t.Fatalf("combine HIR: %v", err)
	}
	return result
}

func diagnoseRequiredOpsProject(t *testing.T, files map[string]string) *DiagnoseResult {
	t.Helper()
	return diagnoseRequiredOpsProjectAt(t, writeRequiredOpsProject(t, files))
}

func diagnoseRequiredOpsProjectAt(t *testing.T, root string) *DiagnoseResult {
	t.Helper()
	result, err := DiagnoseWithOptions(context.Background(), filepath.Join(root, "main.sg"), &DiagnoseOptions{
		Stage: DiagnoseStageSema, BaseDir: root, MaxDiagnostics: 64,
		EmitHIR: true, EmitInstantiations: true,
	})
	if err != nil {
		t.Fatalf("diagnose required-operation project: %v", err)
	}
	if result == nil || result.Bag == nil {
		t.Fatal("required-operation project produced no diagnostics bag")
	}
	if result.Bag.HasErrors() {
		for _, item := range result.Bag.Items() {
			t.Logf("diagnostic %s: %s", item.Code, item.Message)
		}
		t.Fatal("required-operation project diagnosed with errors")
	}
	return result
}

func writeRequiredOpsProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}
