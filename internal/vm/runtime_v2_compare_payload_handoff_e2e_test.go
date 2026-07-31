//go:build !golden

package vm_test

import (
	"strings"
	"testing"
	"time"
)

// An arm that answers with its OWN payload binding hands that value to the
// compare, and the compare's reader is the one that decides when it dies.
//
// The arm owns the payload it bound — nobody else does, so it earns a release
// at the arm's end. That release is right until the arm ANSWERS with it: from
// then on the value belongs to whatever receives the compare's result, and
// freeing it where the arm ends frees it while the receiver is still reading.
// A CONSUMING receiver used to take the obligation back, which covered
// `let out = compare ...` and nothing else — a BORROWED result never consumes,
// so the arm's release stood and the borrower read freed memory.
//
// The rows are the four places a compare's value can go, and each reads the
// value AFTER the compare is over, which is the only way an early release shows
// up as anything but a leak. The counter-check is the leak column: an arm that
// stops freeing a value nothing else frees would trade this defect for the
// other one.
const runtimeV2ComparePayloadHandoffSource = `
tag Payload(string);
tag Empty();
type Slot = Payload(string) | Empty;

// Built at runtime rather than written as a literal, so every payload is its
// own block and a missing release cannot hide behind a shared one.
fn build(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

fn mk(i: int) -> Slot {
    if i > 100 {
        return Empty();
    }
    return Payload(build("v-"));
}

// The probe DEREFERENCES. One that only returns a constant reads every shape
// here as clean — that is how this defect stayed invisible for a session.
fn peek(x: &string) -> int {
    return len(x) to int;
}

// Borrowed: the result is handed to a callee that only looks at it, so nothing
// consumes it and the arm's own release is the one that used to fire.
fn borrowed(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        acc = acc + peek(compare mk(i) {
            Payload(s) => s;
            Empty() => "";
        });
        i = i + 1;
    }
    return acc;
}

// Consumed: the binding is the new owner and must be the only one.
fn consumed(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let out = compare mk(i) {
            Payload(s) => s;
            Empty() => "";
        };
        acc = acc + peek(&out);
        i = i + 1;
    }
    return acc;
}

// Discarded: nobody receives it, so the statement's own reclamation is all
// there is.
fn discarded(n: int) -> int {
    let mut i = 0;
    while i < n {
        compare mk(i) {
            Payload(s) => s;
            Empty() => "";
        };
        i = i + 1;
    }
    return 0;
}

// Returned: the value leaves the function that built it, and the caller reads
// it afterwards.
fn pick(i: int) -> string {
    return compare mk(i) {
        Payload(s) => s;
        Empty() => "";
    };
}

fn returned(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let out = pick(i);
        acc = acc + peek(&out);
        i = i + 1;
    }
    return acc;
}

// An arm that BUILDS from its payload instead of answering with it: the payload
// stays the arm's to free, and the built value is the compare's. Both must
// happen, exactly once each.
fn derived(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let out = compare mk(i) {
            Payload(s) => s + "!";
            Empty() => "";
        };
        acc = acc + peek(&out);
        i = i + 1;
    }
    return acc;
}

@entrypoint
fn main() -> int {
    if borrowed(16) != 96 {
        print("a borrowed compare result lost its payload");
        return 1;
    }
    if consumed(16) != 96 {
        print("a consumed compare result lost its payload");
        return 2;
    }
    if discarded(16) != 0 {
        print("a discarded compare went wrong");
        return 3;
    }
    if returned(16) != 96 {
        print("a returned compare result lost its payload");
        return 4;
    }
    if derived(16) != 112 {
        print("a derived compare result went wrong");
        return 5;
    }
    print("compare-payload-ok");
    return 0;
}
`

func TestRuntimeV2ComparePayloadOutlivesItsArm(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2ComparePayloadHandoffSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("a compare arm's payload hit a memcheck error (invalid read / invalid free)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("compare-payload probe failed (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "compare-payload-ok") {
		t.Fatalf("compare-payload probe missing completion marker; stdout=%q", stdout)
	}
	// The other half of the same contract: moving a release later must not lose
	// it. Strict zero, so a payload nobody frees fails this row too.
	lostBytes, lostBlocks, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("compare payload: %v\nstderr:\n%s", err, stderr)
	}
	if lostBytes != 0 || lostBlocks != 0 {
		t.Fatalf("compare payload leaked %d bytes in %d blocks; want strict zero\nstderr:\n%s",
			lostBytes, lostBlocks, stderr)
	}
}

func TestComparePayloadSurvivesTheHeapSanitizer(t *testing.T) {
	res := runProgramFromSource(t, runtimeV2ComparePayloadHandoffSource, runOptions{})
	if res.exitCode != 0 {
		t.Fatalf("compare-payload probe failed (exit=%d)\nstderr:\n%s", res.exitCode, res.stderr)
	}
	if strings.TrimSpace(res.stderr) != "" {
		t.Fatalf("compare-payload probe reported a runtime error:\n%s", res.stderr)
	}
}
