package driver

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"surge/internal/diag"
)

func TestDiagnose_NoAlienHintsFlagSuppressesAlienDiagnostics(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	projectRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))

	t.Setenv("SURGE_STDLIB", projectRoot)

	fixtures := []struct {
		Name       string
		Src        string
		WantErrors bool
	}{
		{
			Name: "rust_impl_modifier",
			Src: `impl fn demo() -> int {
    return 1;
}
`,
			WantErrors: true,
		},
		{
			Name: "rust_attribute_hash",
			Src: `#[align]
type Foo = { x: int };
`,
			WantErrors: true,
		},
		{
			Name: "rust_println_macro",
			Src: `fn main() -> nothing {
    println!("hi");
    return nothing;
}
`,
			WantErrors: true,
		},
		{
			Name: "go_defer",
			Src: `fn main() -> nothing {
    defer(foo());
    return nothing;
}
`,
			WantErrors: true,
		},
		{
			Name:       "ts_interface_extends",
			Src:        "interface Foo extends Bar { }\n",
			WantErrors: true,
		},
		{
			Name: "python_none_type",
			Src: `fn foo() -> None {
    return nothing;
}
`,
			WantErrors: true,
		},
		{
			Name: "python_none_alias",
			Src: `type None = nothing;

fn foo() -> None {
    let x: None = nothing;
    return x;
}
`,
			WantErrors: false,
		},
	}

	optsEnabled := DiagnoseOptions{
		Stage:          DiagnoseStageAll,
		MaxDiagnostics: 50,
	}
	optsDisabled := optsEnabled
	optsDisabled.NoAlienHints = true

	dir := t.TempDir()
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			path := filepath.Join(dir, fixture.Name+".sg")
			if err := os.WriteFile(path, []byte(fixture.Src), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			resEnabled, err := DiagnoseWithOptions(context.Background(), path, optsEnabled)
			if err != nil {
				t.Fatalf("DiagnoseWithOptions(enabled) error: %v", err)
			}
			if resEnabled.Bag == nil {
				t.Fatalf("missing diagnostic bag for enabled run: %+v", resEnabled)
			}
			if fixture.WantErrors && !resEnabled.Bag.HasErrors() {
				t.Fatalf("expected base errors for fixture, got none: %+v", resEnabled)
			}
			if !fixture.WantErrors && resEnabled.Bag.HasErrors() {
				t.Fatalf("expected no errors for valid fixture, got errors: %+v", resEnabled.Bag.Items())
			}

			resDisabled, err := DiagnoseWithOptions(context.Background(), path, optsDisabled)
			if err != nil {
				t.Fatalf("DiagnoseWithOptions(disabled) error: %v", err)
			}
			if resDisabled.Bag == nil {
				t.Fatalf("missing diagnostic bag for disabled run: %+v", resDisabled)
			}
			if fixture.WantErrors && !resDisabled.Bag.HasErrors() {
				t.Fatalf("expected base errors for fixture with --no-alien-hints, got none: %+v", resDisabled)
			}
			if !fixture.WantErrors && resDisabled.Bag.HasErrors() {
				t.Fatalf("expected no errors for valid fixture with --no-alien-hints, got errors: %+v", resDisabled.Bag.Items())
			}

			enabledAlienCount := countAlienHintDiagnostics(resEnabled.Bag.Items())
			if enabledAlienCount == 0 {
				t.Fatalf("expected ALN* diagnostics for enabled run, got none")
			}

			disabledAlienCount := countAlienHintDiagnostics(resDisabled.Bag.Items())
			if disabledAlienCount != 0 {
				t.Fatalf("expected no ALN* diagnostics with --no-alien-hints, got %d", disabledAlienCount)
			}

			baseFromEnabled := diag.FormatGoldenDiagnostics(filterOutAlienHintDiagnostics(resEnabled.Bag.Items()), resEnabled.FileSet, false)
			baseFromDisabled := diag.FormatGoldenDiagnostics(resDisabled.Bag.Items(), resDisabled.FileSet, false)
			if baseFromEnabled != baseFromDisabled {
				t.Fatalf("baseline diagnostics differ (enabled minus ALN vs disabled):\n--- enabled (filtered) ---\n%s\n--- disabled ---\n%s", baseFromEnabled, baseFromDisabled)
			}
		})
	}
}

func isAlienHintCode(code diag.Code) bool {
	ic := int(code)
	return ic >= 8000 && ic < 9000
}

func countAlienHintDiagnostics(items []*diag.Diagnostic) int {
	count := 0
	for _, d := range items {
		if d == nil {
			continue
		}
		if isAlienHintCode(d.Code) {
			count++
		}
	}
	return count
}

func filterOutAlienHintDiagnostics(items []*diag.Diagnostic) []*diag.Diagnostic {
	filtered := make([]*diag.Diagnostic, 0, len(items))
	for _, d := range items {
		if d == nil || isAlienHintCode(d.Code) {
			continue
		}
		filtered = append(filtered, d)
	}
	return filtered
}
