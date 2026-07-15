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
