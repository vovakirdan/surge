package driver

import (
	"os"
	"path/filepath"
	"testing"

	"surge/internal/diag"
)

func TestEntrypointRefusesSourceCallsAndValues(t *testing.T) {
	for _, row := range []struct{ name, source string }{
		{"recursive", `@entrypoint fn main(depth: int = 0) -> int {
    if depth == 0 { return main(1); };
    checkpoint().await();
    return 0;
}`},
		{"call", `@entrypoint fn main() -> int { return 0; }
fn caller() -> int { return main(); }`},
		{"alias", `@entrypoint fn main() -> int { return 0; }
fn caller() -> int { let callback = main; return callback(); }`},
		{"argument", `@entrypoint fn main() -> int { return 0; }
fn apply(f: fn() -> int) -> int { return f(); }
fn caller() -> int { return apply(main); }`},
	} {
		t.Run(row.name, func(t *testing.T) {
			result, _ := diagnoseEntrypointContract(t, row.source)
			requireOnlyEntrypointValueError(t, result)
		})
	}
}

func TestEntrypointValueRuleKeepsOrdinaryCallables(t *testing.T) {
	for _, source := range []string{
		`fn main() -> int { return 7; }
fn caller() -> int { let callback = main; return callback(); }`,
		`@entrypoint async fn main() -> int { return 7; }
async fn caller() -> int { let _ = main().await(); return 0; }`,
		`fn shared() -> int { return 7; }
@entrypoint fn main() -> int { let callback = shared; return callback(); }`,
	} {
		result, _ := diagnoseEntrypointContract(t, source)
		for _, found := range result.Bag.Items() {
			if found.Severity >= diag.SevError {
				t.Fatalf("ordinary callable rejected: %s: %s", found.Code.ID(), found.Message)
			}
		}
	}
}

func TestEntrypointRefusesImportedCallsAndValues(t *testing.T) {
	for _, row := range []struct{ importClause, expression string }{
		{"import lib;", "lib.launch()"},
		{"import lib;", "{ let callback = lib.launch; ret callback(); }"},
		{"import lib::{launch};", "launch()"},
		{"import lib::{launch};", "{ let callback = launch; ret callback(); }"},
	} {
		t.Run(row.importClause+row.expression, func(t *testing.T) {
			t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "lib"), 0o700); err != nil {
				t.Fatal(err)
			}
			for name, source := range map[string]string{
				"lib/lib.sg": `pragma module::lib;
@entrypoint
pub fn launch() -> int { return 0; }
`,
				"main.sg": row.importClause + " fn caller() -> int { return " + row.expression + "; }",
			} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			result, err := DiagnoseWithOptions(t.Context(), filepath.Join(root, "main.sg"), &DiagnoseOptions{
				Stage: DiagnoseStageSema, BaseDir: root, MaxDiagnostics: 64,
			})
			if err != nil {
				t.Fatal(err)
			}
			requireOnlyEntrypointValueError(t, result)
		})
	}
}

func requireOnlyEntrypointValueError(t *testing.T, result *DiagnoseResult) {
	t.Helper()
	for _, found := range result.Bag.Items() {
		if found.Severity >= diag.SevError {
			t.Logf("%s: %s", found.Code.ID(), found.Message)
		}
	}
	d := requireEntrypointDiagnostic(t, result, diag.SemaEntrypointValue)
	requireEntrypointHelp(t, d, "move reusable logic")
	count := 0
	for _, found := range result.Bag.Items() {
		if found.Severity >= diag.SevError {
			count++
			if found.Code != diag.SemaEntrypointValue {
				t.Fatalf("unexpected diagnostic: %s", found.Message)
			}
		}
	}
	if count != 1 {
		t.Fatalf("got %d errors, want one entrypoint diagnostic", count)
	}
}
