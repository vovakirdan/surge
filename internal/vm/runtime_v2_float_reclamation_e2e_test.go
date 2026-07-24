package vm_test

import (
	"strings"
	"testing"
	"time"
)

// Reclamation witness for arbitrary-precision `float`.
//
// `float` has no inline form: every value is a heap block and every operation
// allocates one. Before the block carried a reference count nothing reclaimed
// them, so the leak grew with the work done — the addition loop below leaked
// 4,800 bytes in 200 blocks directly plus 7,228 indirectly in the mantissas
// those blocks own, and a longer loop leaked proportionally more.
//
// The program covers every shape that creates or hands on a reference:
//
//   - a loop that reassigns a binding, so the overwritten value must be
//     released before the new one lands;
//   - a struct literal built from literals, then read back field by field —
//     the container owns its fields, and a field read has to take its own
//     reference or the temp's release would be a second one;
//   - an array of floats walked by `for ... in`, i.e. element reads;
//   - a function that returns a fresh value, and one that returns its own
//     parameter — the parameter is BORROWED, so returning it has to mint a
//     reference rather than hand on one it never had;
//   - a division whose result is a long mantissa, so a leak shows up as bytes
//     as well as blocks.
//
// The gate is strict zero. Anything else means a reference was created without
// a matching release or vice versa.
const runtimeV2FloatReclamationSource = `
type Pair = { a: float, b: float };

fn fresh() -> float {
    let made: float = 2.5;
    return made;
}

fn echo(v: float) -> float {
    return v;
}

@entrypoint
fn main() -> int {
    let mut acc: float = 0.0;
    let mut i: int = 0;
    while i < 50 {
        acc = acc + 1.5;
        i = i + 1;
    }

    let p: Pair = Pair { a: 3.25, b: 4.75 };
    let sum: float = p.a + p.b;

    let xs: float[] = [1.5, 2.5, 3.5];
    let mut walked: float = 0.0;
    for x: float in xs {
        walked = walked + x;
    }

    let made: float = fresh();
    let echoed: float = echo(made);
    let ratio: float = 1.0 / 3.0;

    if acc > 0.0 && sum > 0.0 && walked > 0.0 && echoed > 0.0 && ratio > 0.0 {
        print("float-reclamation-witness");
        return 0;
    }
    return 1;
}
`

func TestRuntimeV2FloatReclamationValgrindZero(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2FloatReclamationSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a real memcheck error (invalid free / use-after-free / uninitialized read)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("float reclamation e2e failed (program exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "float-reclamation-witness") {
		t.Fatalf("float reclamation e2e missing completion marker; stdout=%q", stdout)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	if bytesLost != 0 || blocksLost != 0 {
		t.Fatalf(
			"float reclamation regressed: got %d bytes in %d blocks definitely lost, want strict zero\nstderr:\n%s",
			bytesLost, blocksLost, stderr,
		)
	}
}
