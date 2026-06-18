package vm_test

import (
	"testing"
)

func TestLLVMNativeHeapStats(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `@entrypoint
fn main() -> int {
    let s0: HeapStats = rt_heap_stats();
    let p = rt_alloc(32:uint, 1:uint);
    let s1: HeapStats = rt_heap_stats();
    if s1.alloc_count <= s0.alloc_count { return 1; }
    if s1.live_blocks <= s0.live_blocks { return 2; }
    if s1.live_bytes <= s0.live_bytes { return 3; }
    rt_free(p, 32:uint, 1:uint);
    let s2: HeapStats = rt_heap_stats();
    if s2.free_count <= s1.free_count { return 4; }
    return 0;
}
`

	outputPath := buildLLVMProgramFromSource(t, source)
	stdout, stderr, code := runBinary(t, outputPath)
	if code != 0 {
		t.Fatalf("heap stats smoke failed with exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}
