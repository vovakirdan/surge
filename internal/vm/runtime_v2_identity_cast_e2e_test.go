//go:build !golden

package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A cast from a type to ITSELF hands the source value straight back, so the
// result is a second name for storage the source's owner still holds. Nothing
// may release it on the result's behalf.
//
// `float` is the shape that shows it: its heap block is reference-counted, and
// every temporary holding one is registered for a release when its region ends.
// A cast that produced a temporary but filled it with an alias therefore took a
// reference it never had — the owner's block was freed while the binding still
// pointed at it, and the next read of that binding read freed memory.
//
// The controls matter as much as the rows: a cast that really does convert
// (`int` to `float`) allocates and must keep its release, and the same code
// without a cast pins what the identity rows should cost. All three read their
// values AFTER the cast, which is the only way an over-release shows up as
// anything but a leak.
const runtimeV2IdentityCastSource = `
fn take(v: float) -> float {
    return v + 1.0;
}

// Discarded: nothing consumes the cast, so the statement's own reclamation is
// what decides whether the sources survive it.
fn discarded(flag: bool) -> int {
    let x: float = 1.5;
    let y: float = 2.5;
    flag ? (x to float) : (y to float);
    if x + y != 4.0 {
        return 1;
    }
    return 0;
}

// Consumed by a binding: the binding takes its own reference, and the source
// keeps the one it had.
fn consumed() -> int {
    let a: float = 3.5;
    let b = a to float;
    if a + b != 7.0 {
        return 2;
    }
    return 0;
}

// Handed to a callee, which is the third place a value's ownership can be
// decided.
fn as_argument() -> int {
    let a: float = 4.5;
    let r = take(a to float);
    if a + r != 10.0 {
        return 3;
    }
    return 0;
}

// A cast that CHANGES representation: it builds a value nothing else holds, and
// its release is the only one that value gets.
fn widening() -> int {
    let n: int = 7;
    let f = n to float;
    if f != 7.0 {
        return 4;
    }
    return 0;
}

// The same shape with no cast at all — the cost the identity rows must match.
fn no_cast() -> int {
    let a: float = 5.5;
    let b = a;
    if a + b != 11.0 {
        return 5;
    }
    return 0;
}

// The operand is a LITERAL, which allocates a block of its own. Here the cast
// hands on a value that really is fresh, so somebody must still release it —
// the difference is that the release belongs to the literal underneath, not to
// the cast. Consumed, discarded and forwarded through a branch, because those
// are the three places that answer "who releases it" differently.
fn literal_consumed() -> int {
    let x = 1.5 to float;
    if x != 1.5 {
        return 7;
    }
    return 0;
}

fn literal_discarded() -> int {
    2.5 to float;
    return 0;
}

fn literal_through_branch(flag: bool) -> int {
    let x = flag ? (1.5 to float) : (2.5 to float);
    if x != 1.5 {
        return 8;
    }
    return 0;
}

// A loop, because a lost reference is a use-after-free while a spare one is a
// leak, and only repetition makes the second one loud.
fn repeated(n: int) -> int {
    let mut i = 0;
    let mut acc: float = 0.0;
    while i < n {
        let a: float = 2.0;
        let b = a to float;
        acc = acc + b;
        i = i + 1;
    }
    if acc != 32.0 {
        return 6;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let discarded_code = discarded(true);
    if discarded_code != 0 {
        print("discarded identity cast released its source");
        return discarded_code;
    }
    let consumed_code = consumed();
    if consumed_code != 0 {
        print("consumed identity cast released its source");
        return consumed_code;
    }
    let argument_code = as_argument();
    if argument_code != 0 {
        print("identity cast as an argument released its source");
        return argument_code;
    }
    let widening_code = widening();
    if widening_code != 0 {
        print("widening cast lost its value");
        return widening_code;
    }
    let plain_code = no_cast();
    if plain_code != 0 {
        print("the uncast control computed the wrong value");
        return plain_code;
    }
    let literal_code = literal_consumed();
    if literal_code != 0 {
        print("a bound literal identity cast lost its value");
        return literal_code;
    }
    let literal_discarded_code = literal_discarded();
    if literal_discarded_code != 0 {
        print("a discarded literal identity cast computed the wrong value");
        return literal_discarded_code;
    }
    let literal_branch_code = literal_through_branch(true);
    if literal_branch_code != 0 {
        print("a literal identity cast through a branch lost its value");
        return literal_branch_code;
    }
    let repeated_code = repeated(16);
    if repeated_code != 0 {
        print("repeated identity cast computed the wrong value");
        return repeated_code;
    }
    print("identity-cast-ok");
    return 0;
}
`

// The same program on the interpreter, whose reclamation is CHECKED rather
// than merely performed: it refuses a read of a released slot instead of
// nulling the handle the way the native backend does. A release too many is
// invisible natively when nothing reads the slot again, and this is the row
// that sees it.
func TestIdentityCastSurvivesTheHeapSanitizer(t *testing.T) {
	// The exit code carries the verdict here: the interpreter's stdout is the
	// test process's own, so the completion marker is not readable back. Every
	// row returns its own non-zero code, and a released-slot read fails the run
	// outright.
	res := runProgramFromSource(t, runtimeV2IdentityCastSource, runOptions{})
	if res.exitCode != 0 {
		t.Fatalf("identity-cast probe failed (exit=%d)\nstderr:\n%s", res.exitCode, res.stderr)
	}
	if strings.TrimSpace(res.stderr) != "" {
		t.Fatalf("identity-cast probe reported a runtime error:\n%s", res.stderr)
	}
}

func TestRuntimeV2IdentityCastKeepsItsSourceOwned(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2IdentityCastSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("an identity cast hit a memcheck error (invalid read / invalid free)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("identity-cast probe failed (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "identity-cast-ok") {
		t.Fatalf("identity-cast probe missing completion marker; stdout=%q", stdout)
	}
	lostBytes, lostBlocks, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("identity cast: %v\nstderr:\n%s", err, stderr)
	}
	if lostBytes != 0 || lostBlocks != 0 {
		t.Fatalf("identity cast leaked %d bytes in %d blocks; want strict zero\nstderr:\n%s",
			lostBytes, lostBlocks, stderr)
	}
}
