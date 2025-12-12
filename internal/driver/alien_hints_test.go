package driver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"surge/internal/diag"
)

func TestDiagnose_NoAlienHintsFlagSuppressesAlienDiagnostics(t *testing.T) {
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	projectRoot := filepath.Clean(filepath.Join(origWD, "..", ".."))
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	t.Setenv("SURGE_STDLIB", projectRoot)

	fixtures := []string{
		filepath.Join("testdata", "golden", "sema", "invalid", "alien_hints", "rust_impl_modifier.sg"),
		filepath.Join("testdata", "golden", "sema", "invalid", "alien_hints", "rust_attribute_hash.sg"),
		filepath.Join("testdata", "golden", "sema", "invalid", "alien_hints", "rust_println_macro.sg"),
		filepath.Join("testdata", "golden", "sema", "invalid", "alien_hints", "go_defer.sg"),
		filepath.Join("testdata", "golden", "sema", "invalid", "alien_hints", "ts_interface_extends.sg"),
		filepath.Join("testdata", "golden", "sema", "invalid", "alien_hints", "python_none_type.sg"),
	}

	optsEnabled := DiagnoseOptions{
		Stage:          DiagnoseStageAll,
		MaxDiagnostics: 50,
	}
	optsDisabled := optsEnabled
	optsDisabled.NoAlienHints = true

	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			resEnabled, err := DiagnoseWithOptions(context.Background(), fixture, optsEnabled)
			if err != nil {
				t.Fatalf("DiagnoseWithOptions(enabled) error: %v", err)
			}
			if resEnabled.Bag == nil || !resEnabled.Bag.HasErrors() {
				t.Fatalf("expected base errors for fixture, got none: %+v", resEnabled)
			}

			resDisabled, err := DiagnoseWithOptions(context.Background(), fixture, optsDisabled)
			if err != nil {
				t.Fatalf("DiagnoseWithOptions(disabled) error: %v", err)
			}
			if resDisabled.Bag == nil || !resDisabled.Bag.HasErrors() {
				t.Fatalf("expected base errors for fixture with --no-alien-hints, got none: %+v", resDisabled)
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
