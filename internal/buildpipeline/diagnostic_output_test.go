package buildpipeline

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/driver"
)

// TestBuildDiagnosticsCarryEveryChannel is the user-boundary proof for
// `surge build`.
//
// Constructing notes, help and fixes inside the compiler and dropping them at
// the boundary is not a friendly diagnostic — it is an internal data structure.
// This runs the same function the build command prints through, against a
// program that produces all four channels at once.
func TestBuildDiagnosticsCarryEveryChannel(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	dir := t.TempDir()
	path := filepath.Join(dir, "task_clone.sg")
	source := `
type Widget = { name: string }

async fn make_widget() -> Widget {
    return Widget { name = "w" };
}

@entrypoint
fn main() {
    let first = spawn make_widget();
    let second = first.clone();
    let a = first.await();
    let b = second.await();
}
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	res, err := driver.DiagnoseWithOptions(context.Background(), path, &driver.DiagnoseOptions{
		Stage: driver.DiagnoseStageAll, BaseDir: dir, MaxDiagnostics: 64,
	})
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	var out bytes.Buffer
	printBuildDiagnostics(&out, res)
	printed := out.String()
	if printed == "" {
		t.Fatal("a failing build printed nothing")
	}
	for _, want := range []string{
		// the code, so the author can look the rule up
		"error SEM3116",
		// the position, so they can find it
		"task_clone.sg:11:18",
		// the sentence
		"owes another independent `Widget`",
		// the explanation channel
		"note ",
		"no `__clone` declaration claims this type",
		// the actionable channel
		"help ",
		"consume this one by awaiting it",
	} {
		if !strings.Contains(printed, want) {
			t.Fatalf("build output is missing %q:\n%s", want, printed)
		}
	}
}

// TestBuildDiagnosticsPrintFixTitlesWithApplicability proves the fourth
// channel reaches the boundary too, and that it arrives labelled: a build does
// not edit sources, so the title plus its applicability is exactly what tells
// an author whether `surge fix` will apply this one automatically.
func TestBuildDiagnosticsPrintFixTitlesWithApplicability(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	dir := t.TempDir()
	path := filepath.Join(dir, "partial_move.sg")
	source := `
type Inner = { name: string }
type Outer = { inner: Inner }

fn consume(value: Inner) -> int {
    return 1;
}

@entrypoint
fn main() {
    let outer = Outer { inner = Inner { name = "n" } };
    let taken = consume(outer.inner);
}
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	res, err := driver.DiagnoseWithOptions(context.Background(), path, &driver.DiagnoseOptions{
		Stage: driver.DiagnoseStageAll, BaseDir: dir, MaxDiagnostics: 64,
	})
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	var out bytes.Buffer
	printBuildDiagnostics(&out, res)
	printed := out.String()
	if !strings.Contains(printed, "fix [ALWAYS_SAFE]") {
		t.Fatalf("the safe `own` edit did not reach the build boundary:\n%s", printed)
	}
}
