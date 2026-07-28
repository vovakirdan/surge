//go:build !golden

package vm_test

import (
	"testing"
)

// A BORROWING read of a value composite must not duplicate it.
//
// This lives apart from the copy contract on purpose, and the split is a
// finding rather than a filing convention. The contract asserts semantics, and
// on the VM over-duplication has NO semantic signature: places are abstract
// (`Location`, not a raw pointer) and a write goes through the place rather
// than through whatever a read produced, so cloning on every read returns the
// same answers — measured, `acc` is identical either way. What changes is the
// allocation traffic, so an allocation census is the only instrument that sees
// it, and a census is exactly what the contract file must not contain.
//
// The defect it guards against is the obvious wrong implementation of the
// type-directed copy: duplicating whenever a composite is READ instead of only
// when the read is CONSUMED. That version passes every semantic row and turns
// each field access into an allocation.
const runtimeV2CompositeBorrowCostSource = `
@copy type Pair = { a: int, b: int };

fn peek(r: &Pair) -> int {
    return r.a;
}

// The composite is read as a LOCAL here, not through a reference parameter.
// That distinction is the whole probe: a read through an already-borrowed
// parameter never produces a composite-typed value operand, so it cannot
// duplicate and cannot detect duplication either. The local read is the shape
// that goes through the operand kind under test.
fn borrow_reads(n: int) -> int {
    let p = Pair { a = 1, b = 2 };
    let mut acc = 0;
    let mut i = 0;
    while i < n {
        acc = acc + peek(&p) + p.b;
        i = i + 1;
    }
    return acc;
}

// The control loop: identical shape and identical arithmetic, with the two
// composite reads replaced by the constants they yield. An int is
// arbitrary-precision, so the arithmetic allocates on its own and would swamp
// the signal outright — the question is never "does this loop allocate" but
// "does READING the composite add anything on top".
fn no_reads(n: int) -> int {
    let mut acc = 0;
    let mut i = 0;
    while i < n {
        acc = acc + 1 + 2;
        i = i + 1;
    }
    return acc;
}

@entrypoint
fn main() -> int {
    let n = 201;

    let s1: HeapStats = rt_heap_stats();
    let with_reads = borrow_reads(n);
    let e1: HeapStats = rt_heap_stats();

    let s2: HeapStats = rt_heap_stats();
    let without_reads = no_reads(n);
    let e2: HeapStats = rt_heap_stats();

    if with_reads != 603 || without_reads != 603 {
        print("the two loops must compute the same thing");
        return 1;
    }

    let reading: uint = e1.alloc_count - s1.alloc_count;
    let plain: uint = e2.alloc_count - s2.alloc_count;
    if reading > plain {
        // The only difference between the loops is that one reads a borrowed
        // composite, so any extra allocation is that read duplicating it.
        print("a borrowing read allocates; it must not duplicate the composite");
        return 2;
    }

    print("composite-borrow-cost-ok");
    return 0;
}
`

func TestRuntimeV2CompositeBorrowReadDoesNotDuplicate(t *testing.T) {
	t.Setenv(backendEnvVar, backendVM)
	res := runProgramFromSource(t, runtimeV2CompositeBorrowCostSource, runOptions{})
	if res.exitCode != 0 {
		t.Fatalf("borrow-read cost census failed (code %d)\nstdout:\n%s\nstderr:\n%s",
			res.exitCode, res.stdout, res.stderr)
	}
}
