package driver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A module path is a directory layout relative to the base dir, and `stdlib/...`
// is not a reserved namespace. A user file laid out as stdlib/time/time.sg
// OUTSIDE the stdlib root has the real module's path and source key; its
// Duration must not receive the stdlib/time certificate. The real module,
// imported beside such a directory, still does.
func TestReturnOriginStdlibTimeDurationForgedModulePath(t *testing.T) {
	repo := repoRootFromDriverTest(t)
	real, err := os.ReadFile(filepath.Join(repo, "stdlib", "time", "time.sg"))
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	forgedDir := filepath.Join(project, "stdlib", "time")
	appDir := filepath.Join(project, "app")
	for _, dir := range []string{forgedDir, appDir} {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			t.Fatal(mkErr)
		}
	}
	forged := string(real) + "\npub fn leak() -> Duration {\n    return monotonic_now();\n}\n"
	if writeErr := os.WriteFile(filepath.Join(forgedDir, "time.sg"), []byte(forged), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	app := "import stdlib/time as time;\n\nfn measure() -> time.Duration {\n    return time.monotonic_now();\n}\n"
	if writeErr := os.WriteFile(filepath.Join(appDir, "main.sg"), []byte(app), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("SURGE_STDLIB", repo)
	diagnose := func(path string) error {
		_, diagErr := DiagnoseWithOptions(context.Background(), path,
			&DiagnoseOptions{Stage: DiagnoseStageAll, BaseDir: project, MaxDiagnostics: 64})
		return diagErr
	}
	t.Run("forged_root_outside_stdlib", func(t *testing.T) {
		var unfinished *returnOriginUnfinishedError
		if diagErr := diagnose(filepath.Join(forgedDir, "time.sg")); !errors.As(diagErr, &unfinished) {
			t.Fatalf("a Duration laid out as stdlib/time outside the stdlib root must stay refused, got %v", diagErr)
		}
		opaque := 0
		for _, row := range unfinished.Pending {
			if row.SourceKey == "stdlib/time/time.sg" && row.Reason == originOpaqueStateRefusal {
				opaque++
			}
		}
		if opaque == 0 {
			t.Fatalf("the forged Duration lost its opaque-result row: %+v", unfinished.Pending)
		}
	})
	t.Run("import_beside_forged_directory", func(t *testing.T) {
		if diagErr := diagnose(filepath.Join(appDir, "main.sg")); diagErr != nil {
			if strings.Contains(diagErr.Error(), originOpaqueStateRefusal) {
				t.Fatalf("the real stdlib/time Duration lost its certificate beside a forged directory: %v", diagErr)
			}
			t.Fatalf("the importing program did not finish: %v", diagErr)
		}
	})
}
