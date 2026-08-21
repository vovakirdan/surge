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
	// the channel constructor. The native buffer is co-allocated into the rt_channel block
	// (rt_channel_new makes exactly one rt_alloc, sizing it for the ring buffer),
	// so a buffered channel adds ZERO extra allocation BLOCKS over an unbuffered
	// one — only the size of that single block grows. The functional half below
	// (cap=0 send fails, cap=1 send succeeds) proves the buffer is really there.
	source := `@entrypoint
fn main() -> int {
    // Warm both channel paths first: the measured windows must contain
    // only per-channel costs, not one-time lazy initialization — WHERE
    // warm-up happens shifts with unrelated compiler changes (it moved
    // when scope-exit drop synthesis landed) and is not this test's
    // subject. The buffered-vs-unbuffered relation below is what's pinned.
    //
    // The buffered and unbuffered windows now allocate the SAME number of
    // blocks. The whole historical difference (+3, then +2) was literal
    // churn: the buffered window's capacity literal 1:uint was re-parsed into
    // a transient heap bignum on evaluation, while the unbuffered window's
    // 0:uint is the canonical zero that never allocates. RV2-DEBT-036 folds
    // an in-range literal to an inline word, so 1:uint no longer allocates
    // and the accounting difference collapses to zero — which is what "single
    // block" meant all along.
    let warm0 = Channel::<int>::new(0:uint);
    let warm1 = Channel::<int>::new(1:uint);
    let s0: HeapStats = rt_heap_stats();
    let ch0 = Channel::<int>::new(0:uint);
    let s1: HeapStats = rt_heap_stats();
    let ch1 = Channel::<int>::new(1:uint);
    let s2: HeapStats = rt_heap_stats();
    let unbuffered_delta = s1.alloc_count - s0.alloc_count;
    let buffered_delta = s2.alloc_count - s1.alloc_count;
    let expected_buffered_delta = unbuffered_delta;
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
