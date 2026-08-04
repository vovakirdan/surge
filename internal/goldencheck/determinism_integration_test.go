//go:build golden
// +build golden

package goldencheck

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGoldenUpdateDeterminism(t *testing.T) {
	root := testRepoRoot(t)
	surge := filepath.Join(t.TempDir(), "surge")
	build := exec.Command("go", "build", "-o", surge, "./cmd/surge")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build surge: %v\n%s", err, output)
	}
	var output bytes.Buffer
	err := Check(context.Background(), &Options{
		RepoRoot:         root,
		ExpectationsPath: filepath.Join(root, "testdata", "golden.expectations.json"),
		Command:          []string{filepath.Join(root, "scripts", "golden_update.sh")},
		Env:              []string{"SURGE_BIN=" + surge},
		Runs:             2,
		Stdout:           &output,
		Stderr:           &output,
	})
	if err != nil {
		t.Fatalf("two serialized golden updates did not preserve the frozen corpus: %v\n%s", err, output.String())
	}
}
