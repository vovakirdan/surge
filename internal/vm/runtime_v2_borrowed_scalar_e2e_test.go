//go:build !golden

package vm_test

import (
	"strings"
	"testing"
	"time"
)

// Reading a value THROUGH a borrow must leave the owner's value alone, and
// copying one OUT of a container must leave the copy with an owner.
//
// `float` is where the two questions meet. It is a Copy type at the surface and
// a reference-counted block underneath, so a read through a reference copies a
// handle the owner knows nothing about. The temporary that receives it is
// registered for a release the moment it exists, so without a reference of its
// own it gives the OWNER's away: a callee that merely COMPARED its `&float`
// parameter freed the caller's value. The other direction costs the opposite —
// a copy that takes a reference while the binding holding it is treated as an
// alias never releases it, which is a leak per evaluation.
//
// Every row therefore reads the source AFTER the borrow is done with it, and
// the leak total is asserted at strict zero, because these two failures show up
// in different columns of the same report.
const runtimeV2BorrowedScalarSource = `
type Box = { value: float };

// The whole defect, in the smallest shape that has it: a callee that only
// LOOKS at its borrowed argument.
fn compares(v: &float) -> int {
    if v > 0.0 {
        return 1;
    }
    return 0;
}

fn computes(v: &float) -> float {
    return v + 1.0;
}

fn renders(v: &float) -> string {
    return v to string;
}

// A borrow handed on to another borrower: the second read must not release
// what the first one did not own either.
fn forwards(v: &float) -> float {
    return derefs(v);
}

fn derefs(v: &float) -> float {
    return *v;
}

// The deref feeds a by-value parameter, which is the third way the value can
// leave a borrow.
fn by_value(v: float) -> float {
    return v;
}

fn passes(v: &float) -> float {
    return by_value(*v);
}

// The deref is BOUND. The binding is a real owner — it took a reference of its
// own — so it has to release it, and a binding treated as an alias here is the
// leak half of the same contract.
fn binds(v: &float) -> int {
    let c = *v;
    if c > 1.0 {
        return 1;
    }
    return 0;
}

fn binds_and_returns(v: &float) -> float {
    let c = *v;
    return c;
}

// A field read is the same copy without any borrow in sight, and it is where
// the leak was measured first.
fn reads_a_field(n: int) -> int {
    let mut i = 0;
    let mut hits = 0;
    while i < n {
        let b = Box { value = 2.5 };
        let f = b.value;
        if f == 2.5 {
            hits = hits + 1;
        }
        i = i + 1;
    }
    return hits;
}

// Repetition, because a reference lost once is a use-after-free while a spare
// one is only loud in a loop.
fn borrows_repeatedly(v: &float, n: int) -> int {
    let mut i = 0;
    let mut hits = 0;
    while i < n {
        hits = hits + compares(v);
        i = i + 1;
    }
    return hits;
}

@entrypoint
fn main() -> int {
    let a: float = 5.5;
    if compares(&a) != 1 {
        print("comparing a borrowed value went wrong");
        return 1;
    }
    if computes(&a) != 6.5 {
        print("computing with a borrowed value went wrong");
        return 2;
    }
    let rendered = renders(&a);
    if len(rendered) == 0 {
        print("rendering a borrowed value went wrong");
        return 3;
    }
    if forwards(&a) != 5.5 {
        print("forwarding a borrow went wrong");
        return 4;
    }
    if passes(&a) != 5.5 {
        print("passing a borrowed value by value went wrong");
        return 5;
    }
    if binds(&a) != 1 {
        print("binding a borrowed value went wrong");
        return 6;
    }
    if binds_and_returns(&a) != 5.5 {
        print("returning a bound borrow went wrong");
        return 7;
    }
    if reads_a_field(8) != 8 {
        print("reading a field went wrong");
        return 8;
    }
    if borrows_repeatedly(&a, 8) != 8 {
        print("repeated borrowing went wrong");
        return 9;
    }
    // The owner is read LAST, and its value is the proof: every row above ran
    // without taking the block out from under it.
    if a != 5.5 {
        print("the owner's value did not survive its borrows");
        return 10;
    }
    print("borrowed-scalar-ok");
    return 0;
}
`

func TestRuntimeV2BorrowedScalarSurvivesItsReaders(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2BorrowedScalarSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("reading a borrowed scalar hit a memcheck error (invalid read / invalid free)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("borrowed-scalar probe failed (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "borrowed-scalar-ok") {
		t.Fatalf("borrowed-scalar probe missing completion marker; stdout=%q", stdout)
	}
	// Strict zero, and it is the half that a use-after-free fix can silently
	// trade for: a read that stops stealing a reference has to take its own.
	lostBytes, lostBlocks, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("borrowed scalar: %v\nstderr:\n%s", err, stderr)
	}
	if lostBytes != 0 || lostBlocks != 0 {
		t.Fatalf("borrowed scalar leaked %d bytes in %d blocks; want strict zero\nstderr:\n%s",
			lostBytes, lostBlocks, stderr)
	}
}

// The interpreter half: it refuses a read of a released slot instead of nulling
// the handle, so it names an over-release that the native backend can only show
// as a corrupted value.
func TestBorrowedScalarSurvivesTheHeapSanitizer(t *testing.T) {
	res := runProgramFromSource(t, runtimeV2BorrowedScalarSource, runOptions{})
	if res.exitCode != 0 {
		t.Fatalf("borrowed-scalar probe failed (exit=%d)\nstderr:\n%s", res.exitCode, res.stderr)
	}
	if strings.TrimSpace(res.stderr) != "" {
		t.Fatalf("borrowed-scalar probe reported a runtime error:\n%s", res.stderr)
	}
}
