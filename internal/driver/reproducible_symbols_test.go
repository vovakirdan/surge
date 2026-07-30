package driver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"surge/internal/symbols"
)

// Compiling one unchanged source twice must hand out the same symbol ids.
//
// Symbol ids come from a bump allocator, so their values encode the order in
// which declarations were resolved. Several passes walk maps of exports while
// declaring symbols, and Go randomises map iteration, so an unsorted walk
// silently makes every id downstream of it depend on run order. Nothing
// crashes and no answer changes -- the same program is simply numbered
// differently each run, which breaks anything ordered by SymbolID, and shows
// up only as an intermittent golden diff that is easy to dismiss as flakiness.
//
// One process is enough to catch this: Go re-randomises each execution of a
// `range` over a map, so two compilations in the same process already disagree
// when a walk is unsorted.
func TestSameSourceGetsSameSymbolIDs(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	// The defect lives in the walk over CORE's exports, so the gate is only
	// meaningful with a stdlib to load.
	t.Setenv("SURGE_STDLIB", repoRoot)

	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.sg")
	src := `
fn pick(flag: bool, a: int, b: int) -> int {
	if flag {
		return a + b;
	}
	return a;
}

fn main() {
	let value = pick(true, 1, 2);
}
`
	if writeErr := os.WriteFile(mainPath, []byte(src), 0o600); writeErr != nil {
		t.Fatalf("write %s: %v", mainPath, writeErr)
	}

	first := diagnoseSymbolNames(t, mainPath)
	second := diagnoseSymbolNames(t, mainPath)

	if len(first) != len(second) {
		t.Fatalf("symbol count is not reproducible: first=%d second=%d", len(first), len(second))
	}
	for id := range first {
		if first[id] == second[id] {
			continue
		}
		t.Fatalf(
			"symbol ids are not reproducible: id %d is %q on the first compile and %q on the second; "+
				"a pass declares symbols while walking a map, so sort its keys before the walk",
			id+1, first[id], second[id],
		)
	}
}

// diagnoseSymbolNames compiles filePath and returns the symbol names in id order.
func diagnoseSymbolNames(t *testing.T, filePath string) []string {
	t.Helper()

	res, err := DiagnoseWithOptions(context.Background(), filePath, &DiagnoseOptions{
		Stage:          DiagnoseStageSema,
		MaxDiagnostics: 128,
	})
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if res == nil || res.Bag == nil || res.Symbols == nil {
		t.Fatalf("diagnose result missing symbol data")
	}
	if res.Bag.HasErrors() {
		t.Fatalf("diagnostics: %s", driverDiagnosticsSummary(res.Bag))
	}

	table := res.Symbols.Table
	if table == nil || table.Symbols == nil || table.Strings == nil {
		t.Fatalf("diagnose result missing symbol table")
	}
	if table.Symbols.Len() == 0 {
		t.Fatalf("symbol table is empty, the gate would pass vacuously")
	}

	names := make([]string, 0, table.Symbols.Len())
	for i := 1; i <= table.Symbols.Len(); i++ {
		sym := table.Symbols.Get(symbols.SymbolID(i)) //nolint:gosec // bounded by Symbols.Len
		if sym == nil {
			names = append(names, "")
			continue
		}
		name, _ := table.Strings.Lookup(sym.Name)
		names = append(names, name)
	}
	return names
}
