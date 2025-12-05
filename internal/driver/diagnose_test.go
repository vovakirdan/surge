package driver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"surge/internal/diag"
)

func TestDiagnose_NoDependencyErrorForCleanImport(t *testing.T) {
	opts := DiagnoseOptions{
		Stage:          DiagnoseStageSyntax,
		MaxDiagnostics: 10,
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	if err = os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	path := filepath.Join("testdata", "test_fixes", "import_fixes", "empty_import_group.sg")

	res, err := DiagnoseWithOptions(context.Background(), path, opts)
	if err != nil {
		t.Fatalf("DiagnoseWithOptions error: %v", err)
	}

	for _, d := range res.Bag.Items() {
		if d.Code == diag.ProjDependencyFailed {
			t.Fatalf("unexpected dependency failure diagnostic: %+v", d)
		}
	}
}

func TestDiagnoseReportsUnresolvedSymbol(t *testing.T) {
	// Set SURGE_STDLIB to current directory for stdlib access
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	// Go up two levels to project root
	projectRoot := filepath.Join(wd, "..", "..")
	t.Setenv("SURGE_STDLIB", projectRoot)

	src := `
        fn demo() -> int {
            return missing;
        }
    `

	dir := t.TempDir()
	path := filepath.Join(dir, "unresolved.sg")
	if writeErr := os.WriteFile(path, []byte(src), 0o600); writeErr != nil {
		t.Fatalf("write file: %v", writeErr)
	}

	opts := DiagnoseOptions{
		Stage:          DiagnoseStageAll,
		MaxDiagnostics: 8,
	}

	res, err := DiagnoseWithOptions(context.Background(), path, opts)
	if err != nil {
		t.Fatalf("DiagnoseWithOptions error: %v", err)
	}
	if res.Bag.Len() == 0 {
		t.Fatalf("expected diagnostics, got none")
	}

	found := false
	for _, d := range res.Bag.Items() {
		if d.Code == diag.SemaUnresolvedSymbol {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unresolved symbol diagnostic, got %+v", res.Bag.Items())
	}
}
