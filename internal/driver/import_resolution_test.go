package driver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
)

func TestDiagnoseReportsWrongRelativeImportToExplicitModuleSameName(t *testing.T) {
	stdlibRoot := detectStdlibRootFrom(".")
	if stdlibRoot == "" {
		t.Skip("stdlib root not found")
	}
	t.Setenv("SURGE_STDLIB", stdlibRoot)

	root, err := os.MkdirTemp(".", "issue67-")
	if err != nil {
		t.Fatalf("mkdir temp project: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(root)
	})

	writeTestFile(t, filepath.Join(root, "actual", "chess", "chess.sg"), `
pragma module::chess;

pub fn initial_game() -> int {
    return 42;
}
`)
	mainPath := filepath.Join(root, "caller", "main.sg")
	writeTestFile(t, mainPath, `
import ./actual/chess::initial_game;

fn use_game() -> int {
    return initial_game();
}
`)
	okPath := filepath.Join(root, "ok.sg")
	writeTestFile(t, okPath, `
import chess::initial_game;

fn use_game() -> int {
    return initial_game();
}
`)
	wrongAbsolutePath := filepath.Join(root, "wrong_absolute.sg")
	writeTestFile(t, wrongAbsolutePath, `
import missing_parent/chess::initial_game;

fn use_game() -> int {
    return initial_game();
}
`)
	resetExplicitModuleDirCacheForTest()

	opts := DiagnoseOptions{Stage: DiagnoseStageSema, MaxDiagnostics: 32}
	res, err := DiagnoseWithOptions(t.Context(), mainPath, &opts)
	if err != nil {
		t.Fatalf("DiagnoseWithOptions error: %v", err)
	}
	if !hasDiagCode(res.Bag, diag.ProjWrongModuleNameInImport) {
		t.Fatalf("expected PRJ5010 for wrong relative import, got %v", bagMessages(res.Bag))
	}

	okRes, err := DiagnoseWithOptions(t.Context(), okPath, &opts)
	if err != nil {
		t.Fatalf("DiagnoseWithOptions ok import error: %v", err)
	}
	if hasDiagCode(okRes.Bag, diag.ProjWrongModuleNameInImport) {
		t.Fatalf("unexpected PRJ5010 for explicit-name import, got %v", bagMessages(okRes.Bag))
	}

	wrongAbsoluteRes, err := DiagnoseWithOptions(t.Context(), wrongAbsolutePath, &opts)
	if err != nil {
		t.Fatalf("DiagnoseWithOptions wrong absolute import error: %v", err)
	}
	if !hasDiagCode(wrongAbsoluteRes.Bag, diag.ProjWrongModuleNameInImport) {
		t.Fatalf("expected PRJ5010 for wrong absolute import, got %v", bagMessages(wrongAbsoluteRes.Bag))
	}
}

func TestDiagnoseSuppressesWrongImportForInconsistentExplicitModule(t *testing.T) {
	stdlibRoot := detectStdlibRootFrom(".")
	if stdlibRoot == "" {
		t.Skip("stdlib root not found")
	}
	t.Setenv("SURGE_STDLIB", stdlibRoot)

	root, err := os.MkdirTemp(".", "issue67-inconsistent-")
	if err != nil {
		t.Fatalf("mkdir temp project: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(root)
	})

	writeTestFile(t, filepath.Join(root, "actual", "chess", "chess.sg"), `
pragma module::chess;

pub fn initial_game() -> int {
    return 42;
}
`)
	writeTestFile(t, filepath.Join(root, "actual", "chess", "extra.sg"), `
pragma module;

pub fn extra() -> int {
    return 1;
}
`)
	mainPath := filepath.Join(root, "caller", "main.sg")
	writeTestFile(t, mainPath, `
import missing_parent/chess::initial_game;

fn use_game() -> int {
    return initial_game();
}
`)
	resetExplicitModuleDirCacheForTest()

	opts := DiagnoseOptions{Stage: DiagnoseStageSema, MaxDiagnostics: 32}
	res, err := DiagnoseWithOptions(t.Context(), mainPath, &opts)
	if err != nil {
		t.Fatalf("DiagnoseWithOptions error: %v", err)
	}
	if !hasDiagCode(res.Bag, diag.ProjDependencyFailed) {
		t.Fatalf("expected dependency failure diagnostic, got %v", bagMessages(res.Bag))
	}
	if hasDiagCode(res.Bag, diag.ProjWrongModuleNameInImport) {
		t.Fatalf("unexpected PRJ5010 cascade for inconsistent module, got %v", bagMessages(res.Bag))
	}
}

func writeTestFile(t *testing.T, path, src string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(src)+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func resetExplicitModuleDirCacheForTest() {
	explicitModuleDirCache.mu.Lock()
	defer explicitModuleDirCache.mu.Unlock()
	explicitModuleDirCache.byBase = nil
	explicitModuleDirCache.scanned = nil
}
