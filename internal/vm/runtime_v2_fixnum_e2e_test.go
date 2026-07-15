package vm_test

import (
	"strings"
	"testing"
)

// Small ints are represented inline (fixnum): values in the inline range
// (int +-2^62, uint 0..2^63-1) carry their payload in the tagged word and
// never touch the heap, so integer arithmetic is allocation-balanced. These
// e2e rows pin that end to end on the native backend.

// A hot integer loop must not grow the heap in proportion to its iteration
// count. Two windows run the same loop body at 100 and 100_000 iterations; a
// per-iteration leak would make the second window's live-block growth scale
// with the count, so the two growths must match. (Literal re-materialization
// still allocates transiently per iteration, but every block is freed within
// the iteration, so net live growth is a small constant independent of N.)
func TestRuntimeV2FixnumHotLoopHeapBalanced(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `fn run_loop(n: int) -> int {
    let mut i: int = 0;
    let mut acc: int = 0;
    while i < n {
        acc = acc + i;
        i = i + 1;
    }
    return acc;
}

@entrypoint
fn main() -> int {
    // Warm up so one-time lazy initialization is not counted in the windows.
    let warm: int = run_loop(100);
    if warm != 4950 { return 1; }

    let a0: HeapStats = rt_heap_stats();
    let r_small: int = run_loop(100);
    let a1: HeapStats = rt_heap_stats();
    let r_big: int = run_loop(100000);
    let a2: HeapStats = rt_heap_stats();

    if r_small != 4950 { return 2; }
    if r_big != 4999950000 { return 3; }

    let small_growth: uint = a1.live_blocks - a0.live_blocks;
    let big_growth: uint = a2.live_blocks - a1.live_blocks;
    // A per-iteration leak would make big_growth scale with the 1000x larger
    // iteration count; inline fixnums keep both growths a small constant.
    if big_growth != small_growth { return 4; }

    // The loop window is alloc/free consistent: every block the literal-parse
    // churn allocates is freed within the same iteration, so the net of allocs
    // over frees equals the net live growth (the trailing snapshot struct),
    // never a per-iteration residue.
    let big_allocs: uint = a2.alloc_count - a1.alloc_count;
    let big_frees: uint = a2.free_count - a1.free_count;
    if big_allocs - big_frees != big_growth { return 5; }

    return 0;
}
`

	outputPath := buildLLVMProgramFromSource(t, source)
	stdout, stderr, code := runBinary(t, outputPath)
	if code != 0 {
		t.Fatalf("fixnum hot-loop heap balance failed with exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

// Correctness across the inline<->heap boundary: values straddling +-2^62 and
// 2^63-1, the big-int multiplication tail, sign edges, and bitwise ops.
func TestRuntimeV2FixnumBoundaryValues(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `@entrypoint
fn main() {
    let a = 4611686018427387903;      // 2^62 - 1 (max inline int)
    let b = a + 1;                     // 2^62 -> heap
    print(a to string);
    print(b to string);
    print((b - 1) to string);         // back to inline
    print((0 - 4611686018427387904) to string); // -2^62 (min inline)
    print((0 - 4611686018427387905) to string); // -2^62-1 -> heap
    print((a * a) to string);          // ~2^124, heap tail
    print((a * a - a * a) to string);  // 0
    print(((0 - 17) % 5) to string);   // -2
    print(((0 - 17) / 5) to string);   // -3
    print((6 ^ 3) to string);          // 5
    let u: uint = 9223372036854775807:uint; // 2^63-1 (max inline uint)
    print(u to string);
    print((u + 1:uint) to string);     // 2^63 -> heap uint
}
`

	outputPath := buildLLVMProgramFromSource(t, source)
	stdout, stderr, code := runBinary(t, outputPath)
	if code != 0 {
		t.Fatalf("fixnum boundary run failed with exit=%d\nstderr:\n%s", code, stderr)
	}
	want := []string{
		"4611686018427387903",
		"4611686018427387904",
		"4611686018427387903",
		"-4611686018427387904",
		"-4611686018427387905",
		"21267647932558653957237540927630737409",
		"0",
		"-2",
		"-3",
		"5",
		"9223372036854775807",
		"9223372036854775808",
	}
	got := strings.Fields(strings.TrimSpace(stdout))
	if len(got) != len(want) {
		t.Fatalf("output line count: want %d, got %d\nstdout:\n%s", len(want), len(got), stdout)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d: want %q, got %q\nfull stdout:\n%s", i, want[i], got[i], stdout)
		}
	}
}
