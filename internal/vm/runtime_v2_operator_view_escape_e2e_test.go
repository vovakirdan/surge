package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The shape the extended escape rule must NOT refuse, run rather than argued.
//
// `through_ref` returns a window over a REFERENCE parameter: the referent is the
// caller's and outlives the frame, so it is allowed -- and it is exactly the fact the
// new rule records about a function ("gives back a slice of what formal k points at").
// `main` then passes a local of its own and keeps the window in frame, which the rule
// must leave alone. If the predicate ever widens, this program stops compiling; if the
// runtime ever stops registering the window, valgrind sees it here.
const runtimeV2OperatorWindowSourceFmt = `
fn through_ref(xs: &uint64[4]) -> uint64[] {
    return xs[[1..3]];
}

@entrypoint
fn main() -> int {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut i: int = 0;
    let mut acc: uint64 = 0:uint64;
    while i < %d {
        let v = through_ref(&xs);
        acc = acc + v[0];
        i = i + 1;
    }
    if acc == %d:uint64 {
        print("operator-window-witness");
        return 0;
    }
    return 1;
}
`

func TestRuntimeV2OperatorViewOverReferenceParameterRuns(t *testing.T) {
	const windows = 8
	src := fmt.Sprintf(runtimeV2OperatorWindowSourceFmt, windows, windows*2)
	outputPath := buildRuntimeV2CrossingSource(t, src, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("a window over a reference parameter hit a memcheck error\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 || !strings.Contains(stdout, "operator-window-witness") {
		t.Fatalf("allowed window program failed (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	_, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	if blocksLost != 0 {
		t.Fatalf("a window over a reference parameter lost %d blocks, want 0\nstderr:\n%s", blocksLost, stderr)
	}
}
