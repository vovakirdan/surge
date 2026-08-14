package vm_test

import (
	"fmt"
	"testing"
	"time"
)

// A `Range` is a heap object and somebody has to free it.
//
// Both shapes are one `rt_alloc` block sized off the kind byte the object
// carries: 24 bytes for a pair of bounds, 40 for the array cursor
// `xs.__range()` builds. Neither was released. `emitInstrDrop` returned early
// for anything that was not refcounted, a string, a dynamic array or a far
// channel, so MIR's `drop` for `let r = 1..3` compiled to nothing; and a range
// MOVED into a runtime slice sink was never dropped at all, because the value
// had left the frame. Only the for-loop cursor was reclaimed, by its envelope
// release - which is why the leak was invisible to every program that iterates
// without naming its range.
//
// THE SINK CASE IS PINNED ELSEWHERE, by the absolute in
// TestRuntimeV2ArrayViewHeaderReclaimedPerSlice. What this file owns is the
// shapes that never reach a sink: a range a binding owns, a range passed by
// value into a function, and the array cursor named as an expression.
//
// THE BOUNDS ARE NOT PART OF THIS. `SurgeRange.start`/`end` are `void*` words
// that are fixnum-tagged integers or heap bignums, and a heap bignum int has no
// exported lifecycle in this runtime - `bi_free` is a static inline with no
// declaration in `rt.h`, and only `float` is refcounted. The programs below use
// small bounds on purpose, so what they measure is the range object and nothing
// else; a bignum-bounded range still leaks its two bound boxes and that belongs
// to the bignum-lifecycle row.
const runtimeV2RangeReclaimSourceFmt = `
type Holder = { r: Range<int>, n: int };

fn by_value(r: Range<int>) -> int {
    let mut n: int = 0;
    for k in r {
        n = n + k;
    }
    return n;
}

@entrypoint
fn main() -> int {
    let mut xs: int[] = [];
    xs.push(10);
    xs.push(20);
    let mut i: int = 0;
    let mut acc: int = 0;
    while i < %d {
        // A range the binding owns, iterated and then dropped at scope end.
        let r = 1..3;
        for k in r {
            acc = acc + k;
        }
        // A range moved into a by-value parameter.
        acc = acc + by_value(1..3);
        // The array cursor, named as an expression rather than implied.
        for v in xs.__range() {
            acc = acc + v;
        }
        // A range bound and never used at all: its drop is the only thing that
        // can free it, so it is the shape that fails first if the drop arm goes.
        let unused = 5..9;
        // A range held by a STRUCT MEMBER, which no drop instruction reaches -
        // generated glue releases it, and a struct whose only heap member is a
        // range used to answer "owns no heap" and get no glue at all.
        let h = Holder { r = 2..4, n = 1 };
        acc = acc + h.n;
        i = i + 1;
    }
    if acc == %d {
        print("range-reclaim-witness");
        return 0;
    }
    return 1;
}
`

func TestRuntimeV2RangeObjectReclaimed(t *testing.T) {
	// 1 + 2 from the owned range, 1 + 2 from the by-value one, 10 + 20 from the
	// cursor, 1 from the struct member: 37 per iteration.
	const perIteration = 37

	measure := func(iterations int) (bytes int, blocks int) {
		t.Helper()
		src := fmt.Sprintf(runtimeV2RangeReclaimSourceFmt, iterations, iterations*perIteration)
		outputPath := buildRuntimeV2CrossingSource(t, src, nil)
		env := envWithStdlib(repoRoot(t))
		stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
		if hasValgrindMemcheckError(stderr) {
			t.Fatalf(
				"a range program hit a memcheck error at %d iterations - freeing a range twice, "+
					"or reading one after it was freed, looks like this\nstdout:\n%s\nstderr:\n%s",
				iterations, stdout, stderr)
		}
		if exitCode != 0 {
			t.Fatalf("range program failed at %d iterations (exit=%d)\nstdout:\n%s\nstderr:\n%s",
				iterations, exitCode, stdout, stderr)
		}
		lostBytes, lostBlocks, err := parseValgrindDefinitelyLost(stderr)
		if err != nil {
			t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
		}
		return lostBytes, lostBlocks
	}

	bytes8, blocks8 := measure(8)
	bytes16, blocks16 := measure(16)

	if bytes8 != 0 || blocks8 != 0 || bytes16 != 0 || blocks16 != 0 {
		perIterBlocks := float64(blocks16-blocks8) / 8.0
		perIterBytes := float64(bytes16-bytes8) / 8.0
		t.Fatalf(
			"ranges leak: 8 iterations lost %d bytes in %d blocks, 16 lost %d in %d, want zero.\n"+
				"Per iteration that is %.2f blocks and %.2f bytes. FIVE ranges are built per "+
				"iteration, so 5 blocks/iteration means none of them is released; 24 bytes a "+
				"block is a bounds range and 40 is the array cursor, which says WHICH shape "+
				"stopped being freed.",
			bytes8, blocks8, bytes16, blocks16, perIterBlocks, perIterBytes,
		)
	}
}
