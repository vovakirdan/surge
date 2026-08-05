package driver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/source"
)

// finalizationOwnerModule exercises both projections one publication carries:
// the stdin entrypoint in b.sg needs a from_stdin binding, and its clone use
// needs a direct clone binding. Both bodies live in a.sg, so only b.sg owns the
// decisions and a.sg must come away with none.
var finalizationOwnerModule = map[string]string{
	"a.sg": `
pragma module::demo;
type Cell = { value: int };
extern<Cell> {
    pub fn from_stdin(_text: string) -> Erring<Cell, Error> {
        return Success(Cell { value = 29 });
    }
    pub fn __clone(self: &Cell) -> Cell {
        return Cell { value = self.value };
    }
}
`,
	"b.sg": `
pragma module::demo;
@entrypoint("stdin")
fn main(cell: Cell) -> int {
    let seed = Cell { value = cell.value };
    let copy = clone(&seed);
    return copy.value;
}
`,
}

func TestParallelPublicationSurvivesADriftedSourceFileID(t *testing.T) {
	fileSet, results := finalizationOwnerFixture(t)
	// The defect shape: the source-space FileID names a file the set never
	// held, while the AST file each result was checked against still names its
	// real source file. Publication used to key off the source-space field and
	// silently gave these results no decisions at all.
	absent := absentSourceFileID(fileSet)
	for i := range results {
		results[i].FileID = absent
	}

	if err := finalizeParallelFileResults(context.Background(), fileSet, results); err != nil {
		t.Fatalf("finalize parallel file results: %v", err)
	}

	owner := finalizationResult(t, results, "b.sg")
	if len(owner.Sema.EntrypointCallableBindings) != 1 {
		t.Fatalf("owner entrypoint bindings = %+v, want exactly one", owner.Sema.EntrypointCallableBindings)
	}
	binding := owner.Sema.EntrypointCallableBindings[0]
	if binding.SourceKey != "demo/b.sg" {
		t.Fatalf("entrypoint binding source key = %q, want %q", binding.SourceKey, "demo/b.sg")
	}
	if owner.Symbols.Table.Symbols.Get(binding.Callee) == nil {
		t.Fatalf("entrypoint callee %d escaped the owning file's symbol vocabulary", binding.Callee)
	}
	if len(owner.Sema.CloneSymbols) != 1 {
		t.Fatalf("owner clone symbols = %v, want exactly one", owner.Sema.CloneSymbols)
	}
	for use, callee := range owner.Sema.CloneSymbols {
		symbol := owner.Symbols.Table.Symbols.Get(callee)
		if symbol == nil {
			t.Fatalf("clone use %d bound callee %d outside the owning symbol vocabulary", use, callee)
		}
		if name := owner.Symbols.Table.Strings.MustLookup(symbol.Name); name != "__clone" {
			t.Fatalf("clone use %d bound %q, want __clone", use, name)
		}
	}

	// Publication is also what keeps decisions off the files that do not own
	// them; skipping it left the entrypoint binding on every file in the module.
	other := finalizationResult(t, results, "a.sg")
	if len(other.Sema.EntrypointCallableBindings) != 0 {
		t.Fatalf("non-owning file kept entrypoint bindings %+v", other.Sema.EntrypointCallableBindings)
	}
	if len(other.Sema.CloneSymbols) != 0 {
		t.Fatalf("non-owning file kept clone symbols %v", other.Sema.CloneSymbols)
	}
}

func TestPublicationRefusesAnUnknownOwningSourceIdentity(t *testing.T) {
	fileSet, results := finalizationOwnerFixture(t)
	unknown := source.NewFileSetWithBase(fileSet.BaseDir())

	err := finalizeParallelFileResults(context.Background(), unknown, results)
	if err == nil {
		t.Fatal("finalizing against a file set that never held the sources succeeded")
	}
	for _, want := range []string{"AST file", "source file", "does not hold"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %q", err, want)
		}
	}
	for i := range results {
		assertNothingPublished(t, &results[i])
	}

	// Publication itself is the backstop for callers that reach it carrying
	// neither the AST file nor the source file handle.
	owner := finalizationResult(t, results, "b.sg")
	orphan := &DiagnoseResult{FileSet: fileSet, Bag: owner.Bag, Symbols: owner.Symbols, Sema: owner.Sema}
	err = publishFinalizationDecisions(orphan)
	if err == nil {
		t.Fatal("publication accepted a result with no owning source identity")
	}
	for _, want := range []string{"AST id space", "source id space"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %q", err, want)
		}
	}
	assertNothingPublished(t, owner)
}

// finalizationOwnerFixture returns retained per-file results that have been
// diagnosed against the real module graph — the only way to reach a result that
// carries entrypoint and clone requests — and then rewound to their
// pre-publication state so the per-file finalization path can answer them.
func finalizationOwnerFixture(t *testing.T) (*source.FileSet, []DiagnoseDirResult) {
	t.Helper()
	root := t.TempDir()
	moduleDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module: %v", err)
	}
	for name, content := range finalizationOwnerModule {
		if err := os.WriteFile(filepath.Join(moduleDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	fileSet, results, err := DiagnoseDirWithOptions(context.Background(), root, &DiagnoseOptions{
		Stage: DiagnoseStageAll, MaxDiagnostics: 64, KeepArtifacts: true,
	}, 1)
	if err != nil {
		t.Fatalf("diagnose fixture: %v", err)
	}
	for i := range results {
		result := &results[i]
		if result.Bag != nil && result.Bag.HasErrors() {
			t.Fatalf("fixture %s diagnosed with errors: %s", result.Path, diagnosticSummary(result))
		}
		if result.Sema == nil || result.Symbols == nil || result.Builder == nil {
			t.Fatalf("fixture %s retained no artifacts", result.Path)
		}
		result.Sema.InstantiationIdentity = nil
		result.Sema.InstantiationClosure = nil
		result.Sema.EntrypointCallableBindings = nil
		result.Sema.CloneSymbols = nil
	}
	return fileSet, results
}

func finalizationResult(t *testing.T, results []DiagnoseDirResult, base string) *DiagnoseDirResult {
	t.Helper()
	for i := range results {
		if filepath.Base(results[i].Path) == base {
			return &results[i]
		}
	}
	t.Fatalf("no retained result for %s", base)
	return nil
}

func assertNothingPublished(t *testing.T, result *DiagnoseDirResult) {
	t.Helper()
	if len(result.Sema.EntrypointCallableBindings) != 0 {
		t.Fatalf("%s published entrypoint bindings %+v", result.Path, result.Sema.EntrypointCallableBindings)
	}
	if len(result.Sema.CloneSymbols) != 0 {
		t.Fatalf("%s published clone symbols %v", result.Path, result.Sema.CloneSymbols)
	}
}

func absentSourceFileID(fileSet *source.FileSet) source.FileID {
	id := source.FileID(0)
	for fileSet.HasFile(id) {
		id++
	}
	return id
}

func diagnosticSummary(result *DiagnoseDirResult) string {
	var out strings.Builder
	for _, item := range result.Bag.Items() {
		if out.Len() > 0 {
			out.WriteString("; ")
		}
		out.WriteString(item.Code.ID())
		out.WriteString(": ")
		out.WriteString(item.Message)
	}
	return out.String()
}
