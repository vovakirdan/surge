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

	src := "import foo::{}; // surge fix should replace '::{}' with ''\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "empty_import_group.sg")
	if writeErr := os.WriteFile(path, []byte(src), 0o600); writeErr != nil {
		t.Fatalf("write file: %v", writeErr)
	}

	res, err := DiagnoseWithOptions(context.Background(), path, &opts)
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

	res, err := DiagnoseWithOptions(context.Background(), path, &opts)
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
