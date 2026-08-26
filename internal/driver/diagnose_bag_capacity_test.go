package driver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Options built without a MaxDiagnostics are the common shape of a test
// helper, and they used to size the bag at ZERO: every diagnostic was dropped
// on Add, the caller read a clean compile, and a refused program went on into
// HIR merge where the missing finalization surfaced as an internal compiler
// error instead of the refusal. An unsized bag holds the default now.
func TestDiagnoseUnsizedOptionsStillReportErrors(t *testing.T) {
	t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
	path := filepath.Join(t.TempDir(), "refused.sg")
	src := "@entrypoint\nfn main() -> int {\n    return nope;\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	opts := DiagnoseOptions{Stage: DiagnoseStageSema}
	res, err := DiagnoseWithOptions(context.Background(), path, &opts)
	if err != nil {
		t.Fatalf("diagnose failed: %v", err)
	}
	if res == nil || res.Bag == nil {
		t.Fatal("no diagnose result")
	}
	if !res.Bag.HasErrors() {
		t.Fatalf("an unsized bag dropped the refusal: %d diagnostics held, cap %d", len(res.Bag.Items()), res.Bag.Cap())
	}
	if res.Bag.Cap() != DefaultMaxDiagnostics {
		t.Fatalf("unsized bag cap = %d, want the default %d", res.Bag.Cap(), DefaultMaxDiagnostics)
	}
}
