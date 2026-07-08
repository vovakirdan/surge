package driver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/symbols"
)

func repoRootFromDriverTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("driver: cannot locate test source file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestCrossingReadinessDebt024ModuleImportDoesNotRequireImportedEffects(t *testing.T) {
	t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))

	dir, err := os.MkdirTemp(".", "rv2-debt024-")
	if err != nil {
		t.Fatalf("mkdir temp project: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	remoteDir := filepath.Join(dir, "remote")
	if mkdirErr := os.MkdirAll(remoteDir, 0o755); mkdirErr != nil {
		t.Fatalf("mkdir remote module: %v", mkdirErr)
	}
	writeFile := func(path, body string) {
		t.Helper()
		if writeErr := os.WriteFile(path, []byte(body), 0o600); writeErr != nil {
			t.Fatalf("write %s: %v", path, writeErr)
		}
	}
	writeFile(filepath.Join(remoteDir, "remote.sg"), `
pragma module::remote;

pub fn remote_score() -> TaskResult<int> {
	return on pool {
		ret 7;
	};
}
`)
	mainPath := filepath.Join(dir, "main.sg")
	writeFile(mainPath, `
import remote::remote_score;

fn caller() -> TaskResult<int> {
	return remote_score();
}
`)
	resetExplicitModuleDirCacheForTest()

	res, err := DiagnoseWithOptions(context.Background(), mainPath, &DiagnoseOptions{
		Stage:          DiagnoseStageSema,
		MaxDiagnostics: 128,
	})
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if res == nil || res.Bag == nil || res.Sema == nil || res.Symbols == nil {
		t.Fatalf("diagnose result missing sema data")
	}
	if res.Bag.HasErrors() {
		t.Fatalf("diagnostics: %s", driverDiagnosticsSummary(res.Bag))
	}

	if len(res.Sema.CrossingLowering) != 0 {
		t.Fatalf("root caller should not synthesize lowering records for imported calls, got %d", len(res.Sema.CrossingLowering))
	}
	caller := requireDriverFunctionSymbolID(t, res.Symbols, "caller")
	if res.Sema.FunctionEffects[caller].MayCross {
		t.Fatalf("imported function effects should remain deferred for RV2-DEBT-024")
	}

	var remoteSema *sema.Result
	for _, rec := range res.moduleRecords {
		if rec == nil || rec.Meta == nil || !strings.HasSuffix(rec.Meta.Path, "/remote") {
			continue
		}
		for _, sr := range rec.Sema {
			remoteSema = sr
			break
		}
	}
	if remoteSema == nil {
		t.Fatalf("missing dependency sema result for remote module; records: %s", driverModuleRecordPaths(res))
	}
	if len(remoteSema.CrossingLowering) != 1 {
		t.Fatalf("remote module lowering records = %d, want 1", len(remoteSema.CrossingLowering))
	}
	if remoteSema.CrossingLowering[0].Kind != sema.CrossingLoweringOnPlacement {
		t.Fatalf("remote module record kind = %d, want on-placement", remoteSema.CrossingLowering[0].Kind)
	}
}

func driverDiagnosticsSummary(bag *diag.Bag) string {
	if bag == nil {
		return "<nil>"
	}
	items := bag.Items()
	if len(items) == 0 {
		return "<none>"
	}
	lines := make([]string, 0, len(items))
	for _, d := range items {
		lines = append(lines, fmt.Sprintf("[%s] %s", d.Code.ID(), d.Message))
	}
	return strings.Join(lines, "; ")
}

func driverModuleRecordPaths(res *DiagnoseResult) string {
	if res == nil || len(res.moduleRecords) == 0 {
		return "<none>"
	}
	paths := make([]string, 0, len(res.moduleRecords))
	for key, rec := range res.moduleRecords {
		meta := "<nil>"
		if rec != nil && rec.Meta != nil {
			meta = rec.Meta.Path
		}
		paths = append(paths, fmt.Sprintf("%s(meta=%s)", key, meta))
	}
	return strings.Join(paths, ", ")
}

func requireDriverFunctionSymbolID(t *testing.T, res *symbols.Result, name string) symbols.SymbolID {
	t.Helper()
	if res == nil || res.Table == nil || res.Table.Symbols == nil || res.Table.Strings == nil {
		t.Fatal("missing symbol result")
	}
	for i := 1; i <= res.Table.Symbols.Len(); i++ {
		id := symbols.SymbolID(i)
		sym := res.Table.Symbols.Get(id)
		if sym == nil || sym.Kind != symbols.SymbolFunction {
			continue
		}
		text, ok := res.Table.Strings.Lookup(sym.Name)
		if ok && text == name {
			return id
		}
	}
	t.Fatalf("missing function symbol %q", name)
	return symbols.NoSymbolID
}
