package vm_test

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// A range's two BOUND WORDS, end to end: who owns them, who gives them back,
// and what happens when one crosses a thread boundary.
//
// A `Range<T>` is a heap object holding `start` and `end`, and those words may
// be counted heap blocks. Nothing in the object used to say WHICH KIND they
// were -- one constructor family serves every element type, and the three
// arbitrary-precision scalars keep their counts at three different offsets --
// so nothing could release them, nothing could make them private, and the
// integer iteration path read a float bound as a SurgeBigInt. The object now
// carries a bound byte, the constructors take a reference with it, and
// rt_range_free gives it back.

// Every range below is EMPTY, and that is the measurement, not an accident.
//
// A range that yields nothing separates ownership of the bounds and cursor
// from ownership of each yielded value. The yielding float row below now also
// requires strict zero: generated pattern bindings carry lexical drops, so
// there is no remaining per-yield allowance to hide a missed release.
//
// What each shape reaches:
//
//   - the exclusive empty range and the reversed one build two float bounds,
//     retain them into the range, copy the range into a loop cursor which
//     retains them again, compare through the FLOAT arm, and give every
//     reference back at both objects' frees;
//   - `[a..b]` and its open-ended forms are the language's other range
//     spelling, lowered to the four `rt_range_int_*` names, which write the
//     `int` bound kind from the C side;
//   - the fixnum int and uint ranges are the negative control for the retain
//     itself, one per integer arm: their bounds are tagged words with no block
//     behind them, and every lifecycle entry point tests that tag before any
//     load, so a retain or a release that dereferenced first would fault here
//     rather than leak.
//
// Every bound here is a `float` block or a tagged integer word, and no heap
// INTEGER bound appears, which is a limit of the tree rather than of the walk:
// `int` and `uint` are not counted scalars yet, so a binding that holds a heap
// integer never releases it and the block is leaked by the binding whatever the
// range does with its own reference. `float` is the one bound kind whose whole
// lifecycle the language already accounts for, which is exactly why it is the
// kind this lane can measure to zero. That the byte says UINT for a uint range
// and INT for an int one is pinned in the emitted IR instead
// (internal/backend/llvm, TestARangeConstructorNamesItsBoundKind).
const runtimeV2RangeBoundLifecycleSource = `
@entrypoint
fn main() -> int {
    let a: float = 2.5;
    let r = a..2.5;
    let mut n: int = 0;
    for x: float in r { n = n + 1; }
    if n != 0 { return 1; }

    let s = 9.5..1.5;
    let mut m: int = 0;
    for y: float in s { m = m + 1; }
    if m != 0 { return 2; }

    let u = 3:uint..3:uint;
    let mut k: int = 0;
    for z: uint in u { k = k + 1; }
    if k != 0 { return 3; }

    let f = 7..7;
    let mut i: int = 0;
    for w: int in f { i = i + 1; }
    if i != 0 { return 4; }

    let arr: int[] = [10, 20, 30];
    let head: int[] = arr[[..2]];
    let tail: int[] = arr[[1..]];
    let whole: int[] = arr[[..]];
    let mid: int[] = arr[[1..3]];
    if head[0] + tail[0] + whole[0] + mid[0] != 10 + 20 + 10 + 20 { return 5; }

    print("range-bound-lifecycle-witness");
    return 0;
}
`

// The census AND the answer. This area has a history of a fix that was clean
// under valgrind and printed the wrong result, so the loop counts and the slice
// contents are checked by the program itself before the marker is printed: a
// build that reclaimed everything by never running the comparisons would return
// a nonzero code instead of reaching the marker.
func TestRuntimeV2RangeBoundLifecycleValgrindZero(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2RangeBoundLifecycleSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a real memcheck error -- a bound was read through the wrong "+
			"layout, or released while somebody still held it\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("range bound lifecycle e2e failed (program exit=%d; each nonzero code names the "+
			"shape whose walk answered wrongly)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "range-bound-lifecycle-witness") {
		t.Fatalf("range bound lifecycle e2e missing completion marker; stdout=%q", stdout)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	if bytesLost != 0 || blocksLost != 0 {
		t.Fatalf("a range abandoned its bounds: got %d bytes in %d blocks definitely lost, want "+
			"strict zero\nstderr:\n%s", bytesLost, blocksLost, stderr)
	}
}

// RV2-DEBT-357, answered.
//
// `let r = 1.5..2.5; for x in r {}` compiled and faulted: the step dispatched on
// the static element type and had no float arm, so rt_bigint_cmp read the
// float's words as a limb count. Reproduced under valgrind on the tree before
// this lane as `Invalid read of size 4 at bi_is_zero <- bi_cmp <- rt_bigint_cmp`
// on an address four bytes inside a block rt_bigfloat_release had already freed
// -- a use-after-free, because the bounds were stored without a retain and both
// were dead before the loop began. Both halves are fixed here and neither would
// have been enough alone: the float arm without the retain would have handed
// rt_bigfloat_cmp the same freed block.
//
// THE ANSWER IS THE GATE, not the absence of a crash. 1.5 + 2.5 + 3.5 = 7.5 is
// what a walk that steps by one and stops before the end produces; a step that
// compared wrongly would still run and print something else.
//
// This same source now measures the yielded values' complete lifetime as well
// as the answer; every bound, cursor and generated binding must be reclaimed.
const runtimeV2RangeFloatIterationSource = `
@entrypoint
fn main() -> int {
    let r = 1.5..4.5;
    let mut acc: float = 0.0;
    let mut n: int = 0;
    for x: float in r { acc = acc + x; n = n + 1; }
    if n != 3 { return 1; }
    print(acc to string);
    print("range-float-iteration-witness");
    return 0;
}
`

func TestRuntimeV2RangeForFloatBoundsIterateAndAnswer(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2RangeFloatIterationSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a real memcheck error -- the float bound is being read through "+
			"the integer layout again\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("float range iteration failed (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "range-float-iteration-witness") {
		t.Fatalf("missing completion marker; stdout=%q", stdout)
	}
	// 1.5 + 2.5 + 3.5. Printed first, so a walk that ran and computed something
	// else fails here rather than passing on a clean leak summary.
	if !strings.HasPrefix(stdout, "7.5E+0\n") {
		t.Fatalf("the float range did not walk 1.5, 2.5, 3.5; want the sum 7.5 first, stdout=%q", stdout)
	}
	requireNumericIteratorHeapZero(t, stderr)
}

// A range with counted bounds, given to a worker thread.
//
// This is the crossing the gate refused until this lane, and the runtime half
// of internal/buildpipeline's TestRangeWithCountedBoundsCrosses: there the
// module is read for the walk, here the program is run for the answer and the
// census. `a` is still live on this side while the job runs, so both bounds
// have two holders at the boundary and the walk is what makes the shipped ones
// private.
const runtimeV2RangeCrossingSource = `
async fn run() -> int {
    let a: float = 2.5;
    let r: Range<float> = a..2.5;
    let job: Task<int> = blocking {
        let mut n: int = 0;
        for x: float in r { n = n + 1; }
        ret n + 7;
    };
    let got: int = compare job.await() { Success(v) => v; Cancelled() => 0 - 1; };
    print(a to string);
    return got;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    let total: int = compare t.await() { Success(v) => v; Cancelled() => 90; };
    if total != 7 { return 1; }
    print("range-crossing-witness");
    return 0;
}
`

func TestRuntimeV2RangeWithCountedBoundsCrossesIntoBlocking(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2RangeCrossingSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 180*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a real memcheck error on the crossing\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("the range crossing failed (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "range-crossing-witness") {
		t.Fatalf("missing completion marker; stdout=%q", stdout)
	}
	// The origin thread kept reading its own bound after the job was submitted.
	// A walk that handed the worker the block this side still holds would show
	// here as a wrong number or a memcheck error rather than as a leak.
	if !strings.Contains(stdout, "2.5E+0") {
		t.Fatalf("the origin thread's own bound did not survive the crossing; stdout=%q", stdout)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	if bytesLost != 0 || blocksLost != 0 {
		t.Fatalf("the crossed range abandoned a bound: got %d bytes in %d blocks definitely lost, "+
			"want strict zero\nstderr:\n%s", bytesLost, blocksLost, stderr)
	}
}

// The cursor, refused by name at run time.
//
// `arr.__range()` and a `for` over an array both build a Range object of the
// OTHER shape: its two slots hold the element data pointer and the stride, an
// interior pointer into a buffer the origin shard keeps reading. Sema cannot
// see the difference -- both shapes are `Range<T>` -- so the refusal is the
// runtime's, taken on the object's own shape byte, the way rt_array_unshare_walk
// refuses a view.
//
// This row guards an opening rather than closing an old hole, and the
// distinction is worth stating because the two look alike from here. The
// program below did not compile before this lane: a cursor over a `float[]` is
// a `Range<float>`, and EVERY `Range<float>` was refused at the crossing gate
// for its bounds. Lifting that refusal is what let this shape reach a worker at
// all, so the runtime refusal is not an improvement on the old behaviour -- it
// is the thing that keeps the lifting from being a regression, and cutting it
// would hand a worker an interior pointer into a buffer this thread is reading.
//
// The hole that IS old is the same program over an `int[]`: a `Range<int>`
// carries no counted block, so no crossing gate arms a walk for it, no call is
// made, and this refusal is never reached. Measured on the tree before this
// lane and after it, unchanged: the worker returns 11+22+33 = 66, read out of
// the origin shard's live buffer. Closing it means arming the walk for every
// range whatever its bounds hold -- the argument the array walk already makes
// about its own view check, that the runtime is asked a question no type can
// answer -- and the refusal it would then reach is the one this row runs.
const runtimeV2RangeCursorCrossingSource = `
async fn run() -> int {
    let xs: float[] = [1.5, 2.5, 3.5];
    let c = xs.__range();
    let job: Task<int> = blocking {
        let mut n: int = 0;
        for v: float in c { n = n + 1; }
        ret n;
    };
    let got: int = compare job.await() { Success(v) => v; Cancelled() => 0 - 1; };
    print("range-cursor-crossed");
    return got;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    let total: int = compare t.await() { Success(v) => v; Cancelled() => 90; };
    print(total to string);
    return 0;
}
`

const runtimeV2RangeCursorRefusalText = "panic VM1003: array cursor cannot cross a shard boundary: " +
	"it reads elements out of the buffer the origin shard keeps; cross the array itself and walk it " +
	"on the other side"

func TestRuntimeV2ArrayCursorCannotCrossIntoBlocking(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2RangeCursorCrossingSource, nil)
	env := envWithStdlib(repoRoot(t))
	for _, shards := range []int{2, 8} {
		shardEnv := overrideEnvVar(env, "SURGE_SHARDS", strconv.Itoa(shards))
		shardEnv = overrideEnvVar(shardEnv, "SURGE_THREADS", strconv.Itoa(shards))
		_, result := runBinaryWithTimeout(t, outputPath, shardEnv, 60*time.Second)
		if result.exitCode == 0 {
			t.Fatalf("shards=%d: the cursor crossed and the program completed; the worker read the "+
				"origin shard's buffer\nstdout:\n%s\nstderr:\n%s", shards, result.stdout, result.stderr)
		}
		if strings.Contains(result.stdout, "range-cursor-crossed") {
			t.Fatalf("shards=%d: the blocking body ran, so the crossing already happened\nstdout:\n%s",
				shards, result.stdout)
		}
		if !strings.Contains(result.stderr, runtimeV2RangeCursorRefusalText) {
			t.Fatalf("shards=%d: stderr does not carry the refusal by name\nwant substring:\n%s\ngot:\n%s",
				shards, runtimeV2RangeCursorRefusalText, result.stderr)
		}
	}
}
