package crossinggate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"surge/internal/diag"
	"surge/internal/driver"
	workspacediag "surge/internal/driver/diagnose"
	"surge/internal/fix"
)

const validCrossingSource = `
fn double(x: int) -> int {
    return x * 2;
}

fn score(n: int) -> TaskResult<int> {
    return on pool {
        ret double(n);
    };
}
`

func writeCrossingFixture(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.sg")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func assertNoBackendUnavailableDiagnostics(t *testing.T, diags []*diag.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		switch d.Code {
		case diag.FutOnBackendUnavailable,
			diag.FutSpawnOnBackendUnavailable,
			diag.FutFarTaskAwaitBackendUnavailable,
			diag.FutFarTaskCancelBackendUnavailable:
			t.Fatalf("compile-only path emitted backend-unavailable diagnostic %s: %s", d.Code.ID(), d.Message)
		}
	}
}

func assertNoWorkspaceBackendUnavailableDiagnostics(t *testing.T, diags []workspacediag.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		switch d.Code {
		case "FUT7014", "FUT7015", "FUT7016", "FUT7017":
			t.Fatalf("workspace diagnostics emitted backend-unavailable diagnostic %s: %s", d.Code, d.Message)
		}
	}
}

func TestBackendUnavailableNegativeSpace(t *testing.T) {
	t.Setenv("SURGE_STDLIB", repoRoot(t))
	path := writeCrossingFixture(t, validCrossingSource)

	t.Run("driver diagnose", func(t *testing.T) {
		res, err := driver.Diagnose(context.Background(), path, driver.DiagnoseStageSema, 200)
		if err != nil {
			t.Fatalf("diagnose: %v", err)
		}
		if res == nil || res.Bag == nil {
			t.Fatal("diagnose result missing bag")
		}
		assertNoBackendUnavailableDiagnostics(t, res.Bag.Items())
	})

	t.Run("lsp workspace diagnose path", func(t *testing.T) {
		diags, err := workspacediag.DiagnoseWorkspace(context.Background(), &workspacediag.DiagnoseOptions{
			ProjectRoot:    path,
			BaseDir:        filepath.Dir(path),
			Stage:          driver.DiagnoseStageAll,
			MaxDiagnostics: 200,
		}, workspacediag.FileOverlay{})
		if err != nil {
			t.Fatalf("workspace diagnose: %v", err)
		}
		assertNoWorkspaceBackendUnavailableDiagnostics(t, diags)
	})

	t.Run("format path", func(t *testing.T) {
		results, err := driver.FormatPaths(context.Background(), []string{path}, driver.FormatOptions{
			Check:          true,
			MaxDiagnostics: 200,
		})
		if err != nil {
			t.Fatalf("format paths: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected one format result, got %d", len(results))
		}
		if results[0].Err != nil {
			t.Fatalf("format result error: %v", results[0].Err)
		}
	})

	t.Run("fix path", func(t *testing.T) {
		res, err := driver.DiagnoseWithOptions(context.Background(), path, &driver.DiagnoseOptions{
			Stage:          driver.DiagnoseStageAll,
			MaxDiagnostics: 200,
		})
		if err != nil {
			t.Fatalf("fix diagnose: %v", err)
		}
		if res == nil || res.Bag == nil || res.FileSet == nil {
			t.Fatal("fix diagnose result missing data")
		}
		assertNoBackendUnavailableDiagnostics(t, res.Bag.Items())
		_, applyErr := fix.Apply(res.FileSet, res.Bag.Items(), fix.ApplyOptions{Mode: fix.ApplyModeOnce})
		if !errors.Is(applyErr, fix.ErrNoFixes) {
			t.Fatalf("expected no applicable fixes, got %v", applyErr)
		}
	})
}
