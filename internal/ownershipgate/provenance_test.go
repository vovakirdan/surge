package ownershipgate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"surge/internal/buildpipeline"
	"surge/internal/mir"
	"surge/internal/ownershipgate"
)

func importedMutexPollProvenance(t *testing.T, root, fixture string) ownershipgate.FindingKey {
	t.Helper()
	target := fixture
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, filepath.FromSlash(fixture))
	}
	result, err := buildpipeline.Compile(context.Background(), &buildpipeline.CompileRequest{
		TargetPath:     target,
		BaseDir:        root,
		MaxDiagnostics: 500,
		Analysis:       true,
	})
	if err != nil {
		t.Fatalf("analysis compile %s: %v", fixture, err)
	}
	if result.MIR == nil || result.Diagnose == nil || result.Diagnose.Sema == nil || result.Diagnose.FileSet == nil {
		t.Fatalf("analysis compile %s returned incomplete MIR context", fixture)
	}

	var imported *mir.Func
	for _, id := range result.MIR.SortedFuncIDs() {
		candidate := result.MIR.Funcs[id]
		if candidate != nil && candidate.Name == "mutex_lock_task$poll" {
			imported = candidate
			break
		}
	}
	if imported == nil || len(imported.Locals) == 0 {
		t.Fatalf("analysis compile %s has no imported mutex poll function", fixture)
	}
	raw := mir.OwnershipFinding{
		Function:          imported.Name,
		Local:             0,
		LocalName:         imported.Locals[0].Name,
		DefSite:           "provenance",
		ConsumingSite:     "provenance",
		ConsumingPosition: "arg[0]",
		ConsumingKind:     mir.OwnershipSinkCallArg,
		Span:              imported.Span,
	}
	key, normalizeErr := ownershipgate.NormalizeFinding(root, result.Diagnose.FileSet, &raw)
	if normalizeErr != nil {
		t.Fatalf("normalize imported function provenance from %s: %v", fixture, normalizeErr)
	}
	return key
}

func TestImportedOwnershipFindingProvenanceDedupesAcrossRoots(t *testing.T) {
	root := ownershipRepoRoot(t)
	t.Setenv("SURGE_STDLIB", root)
	writeRoot := func(name, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write ownership root: %v", err)
		}
		return path
	}
	firstRoot := writeRoot("first.sg", `
@entrypoint fn main() -> int {
    let m = Mutex.new();
    let _ = m.lock();
    m.unlock();
    return 0;
}
`)
	secondRoot := writeRoot("second.sg", `
@entrypoint fn main() -> int {
    let m = Mutex.new();
    let _ = m.lock();
    let marker = 1;
    let _ = marker;
    m.unlock();
    return 0;
}
`)
	first := importedMutexPollProvenance(t, root, firstRoot)
	second := importedMutexPollProvenance(t, root, secondRoot)
	for _, finding := range []ownershipgate.FindingKey{first, second} {
		if finding.Source != "core/sync.sg" {
			t.Fatalf("imported poll source = %q, want core/sync.sg: %s", finding.Source, finding)
		}
		if finding.StartLine != 14 {
			t.Fatalf("imported poll line = %d, want mutex_lock_task declaration line 14: %s", finding.StartLine, finding)
		}
	}
	if first != second {
		t.Fatalf("same imported findings changed with root fixture:\nfirst=%+v\nsecond=%+v", first, second)
	}
	combined := ownershipgate.DedupeFindings([]ownershipgate.FindingKey{first, second})
	if len(combined) != 1 {
		t.Fatalf("imported provenance did not dedupe across roots: combined=%d", len(combined))
	}
	t.Logf("imported provenance representative: %s", first)
}

func TestSyntheticSurgeStartInheritsRealEntrypointProvenance(t *testing.T) {
	root := ownershipRepoRoot(t)
	t.Setenv("SURGE_STDLIB", root)
	fixture := "testdata/golden/vm_tuples/tuple_literals.sg"
	result, err := buildpipeline.Compile(context.Background(), &buildpipeline.CompileRequest{
		TargetPath:     filepath.Join(root, filepath.FromSlash(fixture)),
		BaseDir:        root,
		MaxDiagnostics: 500,
		Analysis:       true,
	})
	if err != nil {
		t.Fatalf("analysis compile %s: %v", fixture, err)
	}
	if result.MIR == nil || result.Diagnose == nil || result.Diagnose.FileSet == nil {
		t.Fatalf("analysis compile %s returned incomplete MIR context", fixture)
	}

	var start *mir.Func
	for _, id := range result.MIR.SortedFuncIDs() {
		candidate := result.MIR.Funcs[id]
		if candidate != nil && candidate.Name == "__surge_start" {
			start = candidate
			break
		}
	}
	if start == nil {
		t.Fatal("compiled entrypoint fixture has no __surge_start")
	}
	if len(start.Locals) == 0 {
		t.Fatal("compiled __surge_start has no synthetic locals")
	}
	raw := mir.OwnershipFinding{
		Function:          start.Name,
		Local:             0,
		LocalName:         start.Locals[0].Name,
		DefSite:           "integration",
		ConsumingSite:     "integration",
		ConsumingPosition: "arg[0]",
		ConsumingKind:     mir.OwnershipSinkCallArg,
		Span:              start.Span,
	}
	key, err := ownershipgate.NormalizeFinding(root, result.Diagnose.FileSet, &raw)
	if err != nil {
		t.Fatalf("normalize real __surge_start provenance: %v", err)
	}
	if key.Source != fixture {
		t.Fatalf("__surge_start source = %q, want %q", key.Source, fixture)
	}
	if key.StartLine < 3 || key.StartLine > 4 {
		t.Fatalf("__surge_start line = %d, want entrypoint declaration at line 3 or 4", key.StartLine)
	}
}
