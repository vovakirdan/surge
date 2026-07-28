//go:build !golden

package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A value composite is reclaimed: every box it allocates is freed, once.
//
// This is the half the earlier steps could not carry. Independence had to land
// on both backends before the ownership axes could move, because a backend that
// still shares a box while the axes say "owned" frees it twice — so through
// those steps the composite was correct and leaking, deliberately, and this
// census is what closes that window.
//
// Two shapes, because they fail differently and only one of them ever showed
// up in a census before:
//
//   - build/drop in a loop with NO copy: the plain leak, one box per
//     construction, which nothing freed because `IsCopy` was answering the
//     ownership question.
//   - build/COPY/drop in a loop: the copy allocates a second box, so a
//     reclamation that forgets the clone leaks half as loudly, and one that
//     frees the source twice corrupts instead.
//
// Run under valgrind on the native backend: the VM's heap is counted and sweeps
// its frames at teardown, so it forgives an abandoned temp that the native
// backend loses outright. That difference is not academic — it is exactly how
// the literal-temp leak hid on one backend while leaking on the other.
const runtimeV2CompositeReclaimSource = `
@copy type Pair = { a: int, b: int };
@copy type Inner = { x: int };
@copy type Outer = { inner: Inner, label: int };

fn build_only(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let p = Pair { a = 1, b = 2 };
        acc = acc + p.a;
        i = i + 1;
    }
    return acc;
}

fn build_and_copy(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let p = Pair { a = 1, b = 2 };
        let q = p;
        acc = acc + q.a;
        i = i + 1;
    }
    return acc;
}

fn build_nested(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let o = Outer { inner = Inner { x = 1 }, label = 2 };
        let c = o;
        acc = acc + c.inner.x;
        i = i + 1;
    }
    return acc;
}

// Row 3 of the frozen contract, and the one whose reclamation nothing measured
// until the freeze looked: the contract text claimed leak-freedom for a
// by-value argument because the callee-ownership leg fails SILENTLY, and the
// test asserting it ran without valgrind. It did leak, twice per call — the
// callee's return temp and the caller's call temp were both materializations
// that consumed as a duplicate instead of a transfer.
fn by_value(v: Pair) -> Pair {
    let mut w = v;
    w.a = 99;
    return w;
}

fn arg_and_return(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let p = Pair { a = 1, b = 2 };
        let r = by_value(p);
        acc = acc + r.a;
        i = i + 1;
    }
    return acc;
}

// Row 5: overwriting a binding frees the old value exactly once.
fn overwrite(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let mut ow = Pair { a = 1, b = 2 };
        ow = Pair { a = 3, b = 4 };
        acc = acc + ow.a;
        i = i + 1;
    }
    return acc;
}

// Row 7: two copies of one value, both dropped.
fn two_copies(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let base = Pair { a = 5, b = 6 };
        let left = base;
        let right = base;
        acc = acc + left.a + right.b;
        i = i + 1;
    }
    return acc;
}

@entrypoint
fn main() -> int {
    if build_only(16) != 16 {
        print("build_only computed the wrong value");
        return 1;
    }
    if build_and_copy(16) != 16 {
        print("build_and_copy computed the wrong value");
        return 2;
    }
    // The nested shape is the one whose INNER box a field walk used to skip,
    // because the inner composite's own fields own no heap.
    if build_nested(16) != 16 {
        print("build_nested computed the wrong value");
        return 3;
    }
    if arg_and_return(16) != 1584 {
        print("arg_and_return computed the wrong value");
        return 4;
    }
    if overwrite(16) != 48 {
        print("overwrite computed the wrong value");
        return 5;
    }
    if two_copies(16) != 176 {
        print("two_copies computed the wrong value");
        return 7;
    }
    print("composite-reclaim-ok");
    return 0;
}
`

func TestRuntimeV2CompositeIsReclaimed(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CompositeReclaimSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("composite reclamation hit a memcheck error (invalid free / use-after-free)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("composite reclamation probe failed (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "composite-reclaim-ok") {
		t.Fatalf("composite reclamation probe missing completion marker; stdout=%q", stdout)
	}
	lostBytes, lostBlocks, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("composite reclamation: %v\nstderr:\n%s", err, stderr)
	}
	if lostBytes != 0 || lostBlocks != 0 {
		t.Fatalf("composite reclamation leaked %d bytes in %d blocks; want strict zero\nstderr:\n%s",
			lostBytes, lostBlocks, stderr)
	}
}
