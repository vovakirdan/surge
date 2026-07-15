package vm_test

import (
	"testing"
)

// Integer `for i in a..=b` in a for-head desugars to a while loop on both
// backends. Before this fix the loop-head range was wrapped in an owned
// temporary that hid its shape from the numeric fast path, so it fell into the
// generic iterator protocol — which the LLVM backend mis-lowered (silently
// wrong before inline ints, a crash after). This row pins the desugar: every
// assertion below runs on the native backend and returns 0 only when the
// integer range iterates exactly like the VM.
func TestRuntimeV2RangeForIntegerHead(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `@entrypoint
fn main() -> int {
    // inclusive
    let mut a: int = 0;
    for i in 1..=5 { a = a + i; }
    if a != 15 { return 1; }

    // exclusive
    let mut b: int = 0;
    for i in 1..5 { b = b + i; }
    if b != 10 { return 2; }

    // empty (exclusive, start == end)
    let mut c: int = 0;
    for i in 5..5 { c = c + 1; }
    if c != 0 { return 3; }

    // uint range
    let mut d: uint = 0:uint;
    for u in 1:uint..=3:uint { d = d + u; }
    if d != 6:uint { return 4; }

    // continue skips the accumulate but still advances; break stops early
    let mut e: int = 0;
    for i in 1..=10 {
        if i == 3 { continue; }
        if i == 6 { break; }
        e = e + i;
    }
    if e != 1 + 2 + 4 + 5 { return 5; }

    // a variable upper bound (not a literal) takes the same path
    let hi: int = 5;
    let mut n: int = 0;
    for i in 1..=hi { n = n + 1; }
    if n != 5 { return 6; }

    // typed loop variable takes the same path
    let mut f: int = 0;
    for i: int in 1..=4 { f = f + i; }
    if f != 10 { return 7; }

    return 0;
}
`

	outputPath := buildLLVMProgramFromSource(t, source)
	stdout, stderr, code := runBinary(t, outputPath)
	if code != 0 {
		t.Fatalf("integer range-for head failed with exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

// A Range<int> VALUE (not a literal in the for-head) iterates via the generic
// iterator protocol. The LLVM backend used to mis-read the SurgeRange as an
// array cursor and dereference its start bound as a data pointer; the unified
// tagged iterator makes a stored/passed/returned range step correctly, exactly
// like the VM.
func TestRuntimeV2RangeForStoredValue(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `fn make_range(n: int) -> Range<int> {
    return 0..n;
}

@entrypoint
fn main() -> int {
    // stored inclusive
    let r1 = 2..=5;
    let mut a: int = 0;
    for x in r1 { a = a + x; }
    if a != 14 { return 1; }   // 2+3+4+5

    // stored exclusive
    let r2 = 2..5;
    let mut b: int = 0;
    for x in r2 { b = b + x; }
    if b != 9 { return 2; }    // 2+3+4

    // returned from a function
    let mut c: int = 0;
    for x in make_range(4) { c = c + x; }
    if c != 6 { return 3; }    // 0+1+2+3

    // stored uint
    let r3: Range<uint> = 1:uint..=3:uint;
    let mut d: uint = 0:uint;
    for x in r3 { d = d + x; }
    if d != 6:uint { return 4; }

    // empty stored range
    let r4 = 5..5;
    let mut e: int = 0;
    for x in r4 { e = e + 1; }
    if e != 0 { return 5; }

    // array iteration still works through the same protocol
    let arr: int[] = [10, 20, 30];
    let mut f: int = 0;
    for x in arr { f = f + x; }
    if f != 60 { return 6; }

    return 0;
}
`

	outputPath := buildLLVMProgramFromSource(t, source)
	stdout, stderr, code := runBinary(t, outputPath)
	if code != 0 {
		t.Fatalf("stored range-for value failed with exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}
