//go:build !golden

package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A partially-moved value reclaims what it still holds, exactly once, and never
// what left it.
//
// This is the half no output assertion can reach, and the reason is the whole
// point of the census: every shape below printed the RIGHT answer while
// corrupting memory. A field taken out and then freed by both its new owner and
// its old container is a double free; a field taken out and freed by neither is
// a leak; both of them return the same number.
//
// Run under valgrind on the native backend. The VM's heap is counted and sweeps
// its frames at teardown, so it forgives an abandoned value that the native
// backend loses outright — the difference is not academic, it is how the
// residual-drop leak hid on one backend while leaking on the other.
const runtimeV2PartialMoveReclaimSource = `
tag Empty();
tag Bytes(byte[]);
type Payload = Empty() | Bytes(byte[]);

type Deep = { a: string, b: string };
type Mid = { deep: Deep, note: string };
type Top = { mid: Mid, label: string };

type Inner = { note: string };
type Holder = { status: int, body: Inner };
type Tagged = { status: int, body: Payload };
type Paired = { pair: (Inner, Inner), label: string };

fn sink_str(s: own string) -> int { return 1; }
fn sink_inner(i: own Inner) -> int { return 1; }
fn sink_bytes(b: own byte[]) -> int { return 2; }

// The flat shape: one field leaves, the sibling stays, the container's own
// storage goes last.
fn flat(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let h = Holder { status = 1, body = Inner { note = "long enough to reach the heap" } };
        acc = acc + sink_inner(own h.body);
        i = i + 1;
    }
    return acc;
}

// Three deep. A residual that stops walking at the first level abandons
// everything under it; one that keeps walking frees what already left.
fn nested(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let t = Top {
            mid = Mid {
                deep = Deep { a = "deep a long enough", b = "deep b long enough" },
                note = "mid note long enough"
            },
            label = "top label long enough"
        };
        acc = acc + sink_str(own t.mid.deep.a);
        i = i + 1;
    }
    return acc;
}

// Every place leaves: the container is left holding nothing but its own
// storage, which still has to go.
fn drained(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let h = Holder { status = 1, body = Inner { note = "drained note long enough" } };
        let taken = own h.body;
        acc = acc + sink_str(own taken.note);
        i = i + 1;
    }
    return acc;
}

// Dropping one place explicitly is a move into nothing, so the container owes
// the same residual it would owe after any other move.
fn explicit_drop(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let h = Holder { status = 3, body = Inner { note = "explicitly dropped note" } };
        @drop h.body;
        acc = acc + h.status;
        i = i + 1;
    }
    return acc;
}

// A UNION-typed field taken out and then destructured. The compare arms carry
// their own release, and they used to ALSO drop the container the scrutinee was
// taken from — on top of the residual drop at the exit. Double free on the
// native backend, use-after-free in the VM, and both printed the right answer.
//
// The arm hands its payload ON to a sink that takes ownership, and that is not
// decoration. A payload binding left to fall off the end of its arm is never
// dropped at all — measured identically on the tree before any of this work, so
// it belongs to the compare-arm release model rather than to partial moves
// (RV2-DEBT-052/075/078). Consuming it keeps this row measuring the double free
// it exists for instead of failing on a leak it does not own.
fn union_field(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let mut data: byte[] = [];
        data.push(1:byte);
        data.push(2:byte);
        let h = Tagged { status = 1, body = Bytes(data) };
        let body = own h.body;
        let got = compare body {
            Empty() => 0;
            Bytes(b) => sink_bytes(own b);
        };
        acc = acc + got + h.status;
        i = i + 1;
    }
    return acc;
}

// A field taken out of a TEMPORARY — a value with no name at all. Its
// statement-end release has to be narrowed the same way a binding's exit drop
// is, or it takes the extracted field with it: a segfault on the native
// backend before this, invalid reads and invalid frees under valgrind
// (RV2-DEBT-084). What makes the plan answerable is that a temporary cannot be
// named again, so everything that will ever be taken out of it was taken inside
// the statement that produced it.
fn from_temporary(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let e = mk_outer().body;
        acc = acc + sink_inner(own e);
        i = i + 1;
    }
    return acc;
}

fn mk_outer() -> Holder {
    return Holder { status = 1, body = Inner { note = "temporary note long enough" } };
}

// A TUPLE element taken out, with its sibling surviving. A tuple's parts CAN be
// listed — fixed arity, literal index — so its residual is enumerable exactly as
// a struct's is, and this row is what says the enumeration is right rather than
// merely accepted. It was refused alongside array elements at first, which left
// an ordinary read of a move-only tuple element with no spelling at all.
fn tuple_element(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let t: (string, string) = ("tuple first long enough", "tuple second long enough");
        acc = acc + sink_str(own t.0);
        i = i + 1;
    }
    return acc;
}

// A path that MIXES the two enumerable kinds, because the residual walk has to
// descend through both: a field of a tuple element of a field.
fn tuple_in_struct(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let h = Paired {
            pair = (Inner { note = "pair left long enough" }, Inner { note = "pair right long enough" }),
            label = "paired label long enough"
        };
        acc = acc + sink_str(own h.pair.0.note);
        i = i + 1;
    }
    return acc;
}

// A partial move on ONE branch only. The branch that did not move it owes a
// drop of that PLACE, and folding that obligation to the whole binding frees
// what the exit still owns.
fn one_sided(n: int, cond: bool) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let h = Holder { status = 2, body = Inner { note = "one sided note long enough" } };
        if cond {
            acc = acc + sink_inner(own h.body);
        }
        acc = acc + h.status;
        i = i + 1;
    }
    return acc;
}

@entrypoint
fn main() -> int {
    if flat(16) != 16 {
        print("flat computed the wrong value");
        return 1;
    }
    if nested(16) != 16 {
        print("nested computed the wrong value");
        return 2;
    }
    if drained(16) != 16 {
        print("drained computed the wrong value");
        return 3;
    }
    if explicit_drop(16) != 48 {
        print("explicit_drop computed the wrong value");
        return 4;
    }
    if tuple_element(16) != 16 {
        print("tuple_element computed the wrong value");
        return 9;
    }
    if tuple_in_struct(16) != 16 {
        print("tuple_in_struct computed the wrong value");
        return 10;
    }
    if from_temporary(16) != 16 {
        print("from_temporary computed the wrong value");
        return 8;
    }
    if union_field(16) != 48 {
        print("union_field computed the wrong value");
        return 5;
    }
    // Both branches, because only one of them exercises the one-sided
    // obligation and the other is its control.
    if one_sided(16, true) != 48 {
        print("one_sided(true) computed the wrong value");
        return 6;
    }
    if one_sided(16, false) != 32 {
        print("one_sided(false) computed the wrong value");
        return 7;
    }
    print("partial-move-reclaim-ok");
    return 0;
}
`

func TestRuntimeV2PartialMoveReclaimsOnlyWhatItStillHolds(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2PartialMoveReclaimSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("partial-move reclamation hit a memcheck error (invalid free / use-after-free)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("partial-move reclamation probe failed (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "partial-move-reclaim-ok") {
		t.Fatalf("partial-move reclamation probe missing completion marker; stdout=%q", stdout)
	}
	lostBytes, lostBlocks, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("partial-move reclamation: %v\nstderr:\n%s", err, stderr)
	}
	if lostBytes != 0 || lostBlocks != 0 {
		t.Fatalf("partial-move reclamation leaked %d bytes in %d blocks; want strict zero\nstderr:\n%s",
			lostBytes, lostBlocks, stderr)
	}
}
