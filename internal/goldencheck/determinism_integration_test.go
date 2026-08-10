//go:build golden
// +build golden

// This one keeps the tag for a reason that is not "it is red".
//
// Check's first act is to refuse a corpus with uncommitted changes under
// testdata/golden, because two serialized regenerations can only be compared
// against a reviewed starting point. A pre-commit hook runs on exactly the
// state that refusal describes, so inside `make check` this test cannot pass on
// any commit that touches a fixture — it would forbid the commits it exists to
// protect. `make golden-check` runs the same Check against a committed tree,
// which is where the question can actually be asked.
//
// The remaining work is to give it a target that guarantees a clean corpus and
// reaches it from CI, rather than leaving it addressable only by a tag nothing
// sets. Tracked with RV2-DEBT-173.

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
