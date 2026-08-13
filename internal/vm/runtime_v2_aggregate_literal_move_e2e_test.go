package vm_test

import (
	"strings"
	"testing"
	"time"
)

// The RUNTIME half of "an array or tuple literal takes the value it is given".
//
// The sema half — reading the binding afterwards is refused — is
// TestAggregateLiteralTakesItsOperand in internal/sema. This program never reads
// the binding again, so no diagnostic fires and nothing is under test except
// ownership: if the literal aliased instead of taking, the binding and the
// aggregate both free the same string at scope exit. Native arrays carry no
// refcount to arbitrate, so the second free is a real one.
//
// IT HAS TO LIVE ON THE NATIVE LANE. The identical program exits 0 with correct
// output on the VM whether or not the fix is present, so the behavioural corpus
// cannot witness this defect at all — which is why it went unpinned for three
// days after being fixed.
//
// Both aggregate kinds are here because they are two independent call sites in
// sema, and the defect arrived precisely because their sibling the struct
// literal was wired up and these two were not.
const runtimeV2AggregateLiteralMoveSource = `
@entrypoint
fn main() -> int {
    let a: string = "a string long enough to be heap allocated rather than inline";
    let xs: string[] = [a, "second"];

    let b: string = "another string long enough to be heap allocated rather than inline";
    let t: (string, int) = (b, 1);

    if rt_string_len(&xs[0]) > 0:uint && rt_string_len(&t.0) > 0:uint {
        print("aggregate-literal-move-witness");
        return 0;
    }
    return 1;
}
`

func TestRuntimeV2AggregateLiteralTakesItsOperand(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2AggregateLiteralMoveSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	// The invalid free is the assertion. A leak would mean the opposite defect —
	// nobody taking the value — and is worth failing on too, but the double free
	// is what this pins.
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("an aggregate literal left its operand with a second owner\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("aggregate-literal move e2e failed (program exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "aggregate-literal-move-witness") {
		t.Fatalf("aggregate-literal move e2e missing completion marker; stdout=%q", stdout)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	if bytesLost != 0 || blocksLost != 0 {
		t.Fatalf(
			"nobody took ownership of the literal's operand: %d bytes in %d blocks definitely lost\nstderr:\n%s",
			bytesLost, blocksLost, stderr,
		)
	}
}
