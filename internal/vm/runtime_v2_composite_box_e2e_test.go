package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A boxed composite owns its BOX even when none of its fields owns heap.
//
// Drop emission used to reach the box only when the type had a heap-owning
// field, with one hand-written exception for tag unions ("every discarded union
// temp leaks its box"). Structs and tuples had no such exception, so every one
// of them leaked one block per construction — `type R = { a: int, b: int }` lost
// 16 bytes each time it was built, forever, in a loop.
//
// The box is the same object in all three cases, so the exception is now
// unconditional. This program builds one of each shape in a loop and demands
// strict zero.
//
// Two shapes are deliberately absent, both still leaking one box each and both
// recorded in the ledger rather than asserted here:
//
//   - a `@copy` struct. A Copy value is duplicated by copying the box POINTER,
//     so two bindings alias one box and dropping both would be a double free.
//     Giving them value semantics needs unboxing or a clone on copy.
//   - a composite NESTED inside another (`{ inner: Pair, ... }`). The glue skips
//     a field whose type owns no heap, so the inner box is never freed. Closing
//     it means making `typeOwnsHeap` true for boxed composites generally, which
//     turns on drop glue for nearly every type at once — worth doing as its own
//     change, with the Copy aliasing question answered first.
const runtimeV2CompositeBoxSource = `
type Pair = { a: int, b: int };
type Nested = { inner: Pair, label: int };

fn mkPair() -> Pair { return Pair { a: 1, b: 2 }; }
fn mkNested() -> Nested { return Nested { inner: Pair { a: 3, b: 4 }, label: 5 }; }

@entrypoint
fn main() -> int {
    let mut total: int = 0;
    let mut k: int = 0;
    while k < 32 {
        let p: Pair = mkPair();
        total = total + p.a + p.b;

        let t: (int, int) = (6, 7);
        total = total + t.0 + t.1;

        k = k + 1;
    }
    if total != 32 * 16 {
        print("composite-box-wrong-total");
        return 1;
    }
    print("composite-box-witness");
    return 0;
}
`

func TestRuntimeV2CompositeBoxIsReclaimed(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CompositeBoxSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a real memcheck error (invalid free / use-after-free / uninitialized read)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("composite box e2e failed (program exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	// The total is checked inside the program: a box freed too early reads as
	// garbage, and an allocation census alone would not notice.
	if !strings.Contains(stdout, "composite-box-witness") {
		t.Fatalf("composite box e2e missing completion marker; stdout=%q", stdout)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	if bytesLost != 0 || blocksLost != 0 {
		t.Fatalf(
			"composite boxes leaked: %d bytes in %d blocks, want strict zero\nstderr:\n%s",
			bytesLost, blocksLost, stderr,
		)
	}
}
