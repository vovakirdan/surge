package driver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/fix"
)

// The rule this diagnostic enforces is new, and the author has a real choice to
// make: the declaration they own is the one that has to say `pub`. So the
// diagnostic carries the edit rather than only describing it, and the edit has
// to land on the `fn` keyword of a declaration in a DIFFERENT file from the one
// the error is reported in — which is exactly where an anchor taken from the
// method-name span would have written `fn pub __clone`.
func TestCloneInvisibleWinnerFixWritesPubAndCompilesClean(t *testing.T) {
	root := writeCloneProject(t, cloneInvisibleWinnerProject(`
    let value: Box = { value: 7 };
    let copy = clone(&value);
`))
	result := diagnoseCloneProjectAt(t, root)
	items := cloneDiagnosticsWithCode(result.Bag, diag.SemaCloneHookNotVisible)
	if len(items) != 1 {
		t.Fatalf("visibility diagnostics = %d, want one: %s", len(items), formatCloneDiagnostics(result.Bag))
	}
	if len(items[0].Fixes) != 1 {
		t.Fatalf("SEM3186 carries %d fixes, want the `pub` edit: %+v", len(items[0].Fixes), items[0].Fixes)
	}
	materialized, err := diag.MaterializeFixes(diag.FixBuildContext{FileSet: result.FileSet}, items[0].Fixes)
	if err != nil {
		t.Fatalf("materialize fix: %v", err)
	}
	if len(materialized) != 1 || len(materialized[0].Edits) != 1 {
		t.Fatalf("materialized fixes = %+v", materialized)
	}
	if materialized[0].Applicability != diag.FixApplicabilityAlwaysSafe || !materialized[0].IsPreferred {
		t.Fatalf("the `pub` edit must be always-safe and preferred: %+v", materialized[0])
	}
	edit := materialized[0].Edits[0]
	// An exact guard is what makes the anchor safe: a stale span refuses the
	// edit instead of inserting `pub ` into the middle of something else.
	if edit.OldText != "fn" || edit.NewText != "pub fn" {
		t.Fatalf("guarded declaration edit = %+v", edit)
	}
	if edit.Span.File == items[0].Primary.File {
		t.Fatal("the edit landed in the file that reports the error, not in the one that declares __clone")
	}

	if _, applyErr := fix.Apply(result.FileSet, result.Bag.Items(), fix.ApplyOptions{Mode: fix.ApplyModeAll}); applyErr != nil {
		t.Fatalf("apply fix: %v", applyErr)
	}
	declaration, err := os.ReadFile(filepath.Join(root, "model", "value.sg"))
	if err != nil {
		t.Fatalf("read fixed declaration: %v", err)
	}
	if !strings.Contains(string(declaration), "pub fn __clone(self: &Box) -> Box {") {
		t.Fatalf("declaration after fix:\n%s", declaration)
	}

	rediagnosed := diagnoseCloneProjectAt(t, root)
	if rediagnosed.Bag != nil && len(rediagnosed.Bag.Items()) != 0 {
		t.Fatalf("program still diagnoses after the fix:\n%s", formatCloneDiagnostics(rediagnosed.Bag))
	}
}

// A file-private winner is not fixed by `pub`, so nothing is attached: the two
// ways out (move the declaration, export the file) are a choice the compiler
// cannot make, and a heuristic edit would be worse than the note alone.
func TestCloneFilePrivateWinnerOffersNoMechanicalEdit(t *testing.T) {
	bag := diagnoseCloneProjectDiagnostics(t, cloneFilePrivateWinnerProject())
	items := cloneDiagnosticsWithCode(bag, diag.SemaCloneHookNotVisible)
	if len(items) != 1 {
		t.Fatalf("visibility diagnostics = %d, want one: %s", len(items), formatCloneDiagnostics(bag))
	}
	if len(items[0].Fixes) != 0 {
		t.Fatalf("file-private winner offered an edit: %+v", items[0].Fixes)
	}
	help := false
	for _, entry := range items[0].Help {
		if strings.Contains(entry.Msg, "file-private") {
			help = true
		}
	}
	if !help {
		t.Fatalf("file-private winner lost its help: notes=%+v help=%+v", items[0].Notes, items[0].Help)
	}
}

// cloneFilePrivateWinnerProject keeps Box's canonical implementation in a file
// its user cannot see into, in the same module. `pub` would not help.
func cloneFilePrivateWinnerProject() map[string]string {
	return map[string]string{
		"model/value.sg": `
pragma module::model;
pub type Box = { value: int }
`,
		"model/hook.sg": `
pragma module::model;
extern<Box> {
    @hidden
    fn __clone(self: &Box) -> Box {
        return { value: self.value };
    }
}
`,
		"main.sg": `
pragma module::app;
import model::Box;
@entrypoint fn main() -> int {
    let value: Box = { value: 7 };
    let copy = clone(&value);
    return 0;
}
`,
	}
}

// diagnoseCloneProjectAt recompiles an already-written project, so a caller can
// diagnose the same tree again after editing it on disk.
func diagnoseCloneProjectAt(t *testing.T, root string) *DiagnoseResult {
	t.Helper()
	result, err := DiagnoseWithOptions(context.Background(), filepath.Join(root, "main.sg"), &DiagnoseOptions{
		Stage: DiagnoseStageAll, BaseDir: root, MaxDiagnostics: 64,
	})
	if err != nil {
		t.Fatalf("diagnose clone project: %v", err)
	}
	if result == nil || result.Bag == nil {
		t.Fatal("clone project produced no diagnostics bag")
	}
	return result
}

func diagnoseCloneProjectDiagnostics(t *testing.T, files map[string]string) *diag.Bag {
	t.Helper()
	return diagnoseCloneProjectAt(t, writeCloneProject(t, files)).Bag
}
