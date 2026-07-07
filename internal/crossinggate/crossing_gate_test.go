package crossinggate

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/driver"
)

// expectDiagRe extracts the expected diagnostic code from a negative fixture's
// `// EXPECT-DIAG: <CODE>` header line.
var expectDiagRe = regexp.MustCompile(`EXPECT-DIAG:\s*([A-Z]+[0-9]+)`)

// repoRoot resolves the repository root from this test file's location so the
// fixtures can be found regardless of the working directory `go test` uses.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("crossinggate: cannot locate test source file")
	}
	// file = <root>/internal/crossinggate/crossing_gate_test.go
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// diagnose runs parse + semantic analysis on a single fixture and returns the
// emitted diagnostics. Epic 11 execution scope is parse + sema only.
func diagnose(t *testing.T, path string) []*diag.Diagnostic {
	t.Helper()
	res, err := driver.Diagnose(context.Background(), path, driver.DiagnoseStageSema, 200)
	if err != nil {
		t.Fatalf("crossinggate: diagnose %s: %v", filepath.Base(path), err)
	}
	if res == nil || res.Bag == nil {
		return nil
	}
	return res.Bag.Items()
}

// expectedCode reads the `// EXPECT-DIAG:` annotation from a negative fixture.
func expectedCode(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("crossinggate: read %s: %v", filepath.Base(path), err)
	}
	m := expectDiagRe.FindSubmatch(data)
	if m == nil {
		t.Fatalf("crossinggate: %s has no EXPECT-DIAG annotation", filepath.Base(path))
	}
	return string(m[1])
}

// summarize renders the emitted diagnostics for failure messages.
func summarize(diags []*diag.Diagnostic) string {
	if len(diags) == 0 {
		return "(no diagnostics)"
	}
	parts := make([]string, 0, len(diags))
	for _, d := range diags {
		parts = append(parts, d.Code.ID())
	}
	return strings.Join(parts, ", ")
}

// runFixtures drives one block's fixtures. It is called only after the caller
// has confirmed that block's gate is enabled.
func runFixtures(t *testing.T, block string) {
	// Match the golden runner: resolve core/stdlib from the repo root.
	t.Setenv("SURGE_STDLIB", repoRoot(t))
	base := filepath.Join(repoRoot(t), "testdata", "golden", "crossing", block)

	validFiles, err := filepath.Glob(filepath.Join(base, "valid", "*.sg"))
	if err != nil {
		t.Fatalf("crossinggate: glob valid: %v", err)
	}
	if len(validFiles) == 0 {
		t.Fatalf("crossinggate: no valid fixtures under %s", base)
	}
	for _, f := range validFiles {
		t.Run("valid/"+filepath.Base(f), func(t *testing.T) {
			for _, d := range diagnose(t, f) {
				if d.Severity == diag.SevError {
					t.Errorf("positive fixture produced error %s: %s", d.Code.ID(), d.Message)
				}
			}
		})
	}

	invalidFiles, err := filepath.Glob(filepath.Join(base, "invalid", "*.sg"))
	if err != nil {
		t.Fatalf("crossinggate: glob invalid: %v", err)
	}
	if len(invalidFiles) == 0 {
		t.Fatalf("crossinggate: no invalid fixtures under %s", base)
	}
	for _, f := range invalidFiles {
		t.Run("invalid/"+filepath.Base(f), func(t *testing.T) {
			want := expectedCode(t, f)
			diags := diagnose(t, f)
			for _, d := range diags {
				if d.Code.ID() == want {
					return
				}
			}
			t.Errorf("negative fixture: expected diagnostic %s, got %s", want, summarize(diags))
		})
	}
}

const gateSkip = "Epic 11 %s gate disabled: fixtures staged, awaiting parser/sema implementation"

func TestEpic11Block1Far(t *testing.T) {
	if !Block1Enabled {
		t.Skipf(gateSkip, "block01")
	}
	runFixtures(t, "block01")
}

func TestEpic11Block2On(t *testing.T) {
	if !Block2Enabled {
		t.Skipf(gateSkip, "block02")
	}
	runFixtures(t, "block02")
}

func TestEpic11Block3SpawnOn(t *testing.T) {
	if !Block3Enabled {
		t.Skipf(gateSkip, "block03")
	}
	runFixtures(t, "block03")
}

func TestEpic11Block4Contracts(t *testing.T) {
	if !Block4Enabled {
		t.Skipf(gateSkip, "block04")
	}
	runFixtures(t, "block04")
}
