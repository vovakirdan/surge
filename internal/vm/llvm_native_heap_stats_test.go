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

func TestLLVMNativeBufferedChannelAllocatesSingleBlock(t *testing.T) {
	ensureLLVMToolchain(t)

	// The public LLVM path includes fixed HeapStats/native wrapper allocations around
	// make_channel. With the native buffer co-allocated into rt_channel, capacity=1
	// should add three more allocations than capacity=0; a separate buffer would add four.
	source := `@entrypoint
fn main() -> int {
    let s0: HeapStats = rt_heap_stats();
    let ch0 = make_channel::<int>(0:uint);
    let s1: HeapStats = rt_heap_stats();
    let ch1 = make_channel::<int>(1:uint);
    let s2: HeapStats = rt_heap_stats();
    let unbuffered_delta = s1.alloc_count - s0.alloc_count;
    let buffered_delta = s2.alloc_count - s1.alloc_count;
    let expected_buffered_delta = unbuffered_delta + 3:uint;
    if buffered_delta != expected_buffered_delta { return 1; }
    if ch0.try_send(1) { return 2; }
    let sent1 = ch1.try_send(42);
    if !sent1 { return 3; }
    compare ch1.try_recv() {
        Some(v) => {
            if v != 42 { return 4; }
        }
        nothing => {
            return 5;
        }
    };
    return 0;
}
`

	outputPath := buildLLVMProgramFromSource(t, source)
	stdout, stderr, code := runBinary(t, outputPath)
	if code != 0 {
		t.Fatalf("buffered channel allocation smoke failed with exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}
