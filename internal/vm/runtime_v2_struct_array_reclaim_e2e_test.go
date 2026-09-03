package vm_test

import (
	"strings"
	"testing"
	"time"
)

// RV2-DEBT-056's leaf row: an array of struct values, built from a literal
// and read through a borrow, reclaims its element boxes exactly once when it
// drops. The debt measured 48 bytes in 3 blocks lost on the LLVM backend for
// this exact shape (a three-element `Foo[]` with a string field), constant in
// how often the array was read; the row pins the strict zero the re-measure
// of 2026-09-03 read, so the struct[] shape no longer needs a differential to
// cancel it.
const runtimeV2StructArrayReclaimSource = `
type Foo = { name: string, n: int }

fn sum_foo_n(a: &Foo[]) -> int {
    let n: int = a.__len() to int;
    let mut total: int = 0;
    let mut i: int = 0;
    while i < n {
        total = total + a[i].n;
        i = i + 1;
    }
    return total;
}

@entrypoint
fn main() -> int {
    let foos: Foo[] = [
        Foo { name = "alpha", n = 1 },
        Foo { name = "beta", n = 2 },
        Foo { name = "gamma", n = 3 },
    ];
    let total: int = sum_foo_n(&foos);
    if total != 6 {
        return 1;
    }
    print("struct-array-reclaim-ok");
    return 0;
}
`

func TestRuntimeV2StructArrayElementsReclaimed(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2StructArrayReclaimSource, nil)
	env := envWithStdlib(repoRoot(t))
	env = overrideEnvVar(env, "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 90*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a memcheck error\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 || !strings.Contains(stdout, "struct-array-reclaim-ok") {
		t.Fatalf("struct array program failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	if bytesLost != 0 || blocksLost != 0 {
		t.Fatalf("definitely lost: %d bytes in %d blocks, want 0 in 0 (RV2-DEBT-056 read 48 in 3)\nstderr:\n%s",
			bytesLost, blocksLost, stderr)
	}
}
