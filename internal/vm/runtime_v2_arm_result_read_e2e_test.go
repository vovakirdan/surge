package vm_test

import (
	"strings"
	"testing"
	"time"
)

// The arm result that is a bare element read of the payload the arm frees.
//
// `compare values.pop() { Some(inner) => inner[0]; ... }` yielded a POINTER,
// not a value: an indexed read stays a call returning `&T`, so the arm's block
// carried the payload's drop and exited, and the load the consumer wraps around
// the block then read storage `array_free_base_storage` had already returned.
// Natively that printed the contents of a freed block AND EXITED 0, which is the
// worst shape this suite can be asked to catch — every gate that compares exit
// codes reports success.
//
// The behavioural fixture records the answers. This records the thing an answer
// cannot: that the read happens before the free. The two are not redundant,
// because a freed block often still holds the right bytes — the fixture agreed
// on both lanes for the arithmetic spelling while the bare one was reading
// freed memory.
const runtimeV2ArmResultReadSource = `
type Holder = { xs: uint64[] };

@entrypoint
fn main() -> int {
    let mut values: uint64[][] = [[6:uint64]];
    let bare: uint64 = compare values.pop() { Some(inner) => inner[0]; _ => 0:uint64; };

    let mut nested: uint64[][][] = [[[14:uint64]]];
    let deep: uint64 = compare nested.pop() { Some(inner) => inner[0][0]; _ => 0:uint64; };

    let mut holders: Holder[] = [Holder { xs = [21:uint64] }];
    let from_struct: uint64 = compare holders.pop() { Some(inner) => inner.xs[0]; _ => 0:uint64; };

    // The untyped let is here on purpose: it infers a reference, so it reaches
    // the same lazy read by a different route and was broken by the same cause.
    let mut again: uint64[][] = [[7:uint64]];
    let bound: uint64 = compare again.pop() { Some(inner) => { let v = inner[0]; ret v; }; _ => 0:uint64; };

    if bare == 6:uint64 && deep == 14:uint64 && from_struct == 21:uint64 && bound == 7:uint64 {
        print("arm-result-read-witness");
        return 0;
    }
    return 1;
}
`

func TestRuntimeV2ArmResultReadHappensBeforeTheArmFrees(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2ArmResultReadSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	// Checked before the answers: reading freed storage is the defect, and the
	// value read out of it is right often enough that asserting only the answer
	// would have passed while the read was invalid.
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("an arm result read the payload after the arm freed it\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("arm-result read e2e failed (program exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "arm-result-read-witness") {
		t.Fatalf("arm-result read e2e missing completion marker; stdout=%q", stdout)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	if bytesLost != 0 || blocksLost != 0 {
		t.Fatalf(
			"the arm's payload was not reclaimed: got %d bytes in %d blocks definitely lost, want strict zero\nstderr:\n%s",
			bytesLost, blocksLost, stderr,
		)
	}
}
