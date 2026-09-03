package vm_test

import (
	"strings"
	"testing"
	"time"
)

// RV2-DEBT-054's leaf row: a droppable `let` re-declared on every pass of a
// `while` body is reclaimed on every pass. The debt measured 2000 × 24 bytes
// lost on the LLVM backend for exactly this shape (an array literal declared
// inside a while body, no for-loop involved); the row pins the strict zero
// the re-measure of 2026-09-03 read.
const runtimeV2WhileBodyLocalReclaimSource = `
@entrypoint
fn main() -> int {
    let mut outer: int = 0;
    let mut total: int = 0;
    while outer < 2000 {
        let arr: int[] = [1, 2, 3];
        total = total + (arr.__len() to int);
        outer = outer + 1;
    }
    if total != 6000 {
        return 1;
    }
    print("while-body-local-reclaim-ok");
    return 0;
}
`

func TestRuntimeV2WhileBodyLocalReclaimed(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2WhileBodyLocalReclaimSource, nil)
	env := envWithStdlib(repoRoot(t))
	env = overrideEnvVar(env, "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a memcheck error\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 || !strings.Contains(stdout, "while-body-local-reclaim-ok") {
		t.Fatalf("while-body program failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	if bytesLost != 0 || blocksLost != 0 {
		t.Fatalf("definitely lost: %d bytes in %d blocks, want 0 in 0 (RV2-DEBT-054 read 2000 x 24)\nstderr:\n%s",
			bytesLost, blocksLost, stderr)
	}
}
