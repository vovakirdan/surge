package vm_test

import (
	"strings"
	"testing"
	"time"
)

// What a blocking BODY owns, measured under valgrind: the local it builds, the
// capture it only reads, the capture it consumes, and the `@copy` composite it
// receives a copy of. The fifth row, at the end of this file, is the one
// valgrind cannot see and is measured from inside the program instead.
//
// These are the rows runtime_v2_blocking_capture_reclamation_test.go said it
// could not pin: blocking bodies recorded no drop obligations at all
// (`dropObligationsSuppressed`, `internal/sema/drop_obligations.go`), so a
// string built inside the body had no scope-exit release and a capture the
// body merely read -- spent out of the job's state by the unpack, claimed by
// the worker before the body ran -- had no owner left. The earlier reproducer
// measured that loss as 219 bytes in one block, constant in the iteration count,
// and it was constant because ITS body CONSUMED its capture; a body that reads
// one loses it once per execution.
//
// Each of the four programs runs its body eight times so a per-execution loss is
// eight blocks, not one, and every program in this file prints a completion
// marker so one that exited before its bodies ran cannot pass by leaking
// nothing. The fifth row separates its two windows by 128 rounds for the same
// reason and divides.
//
// Only the native lane can witness any of these: the VM has no blocking pool
// whose release could be the second owner. What differs is who does the
// measuring — valgrind's `definitely lost` for the four, and the program's own
// `rt_heap_stats` for the fifth, which is abandoned rather than lost and so is
// invisible to a leak checker.

// The body builds a string and returns an int. Nothing moves in, so the only
// thing to reclaim is what the body made -- and it made it eight times.
const runtimeV2BlockingBodyLocalSource = `
fn wide() -> string {
    let mut s: string = "";
    let mut i: int = 0;
    while i < 20 {
        s = s + "0123456789";
        i = i + 1;
    }
    return s;
}

async fn run() -> int {
    let mut total: int = 0;
    let mut i: int = 0;
    while i < 8 {
        let job: Task<int> = blocking {
            let built: string = wide();
            ret built.__len() to int;
        };
        total = total + compare job.await() {
            Success(v) => v;
            Cancelled() => 0;
        };
        i = i + 1;
    }
    return total;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    let total: int = compare t.await() {
        Success(v) => v;
        Cancelled() => 90;
    };
    if total != 1600 {
        print("FAIL blocking body local total");
        return 1;
    }
    print("blocking-body-local-witness");
    return 0;
}
`

// The body READS its capture and returns. The state's unpack spent the field,
// the worker claimed the state before the body ran, so the body is the last
// owner and its return is where the string is released.
const runtimeV2BlockingReadCaptureSource = `
fn wide() -> string {
    let mut s: string = "";
    let mut i: int = 0;
    while i < 20 {
        s = s + "0123456789";
        i = i + 1;
    }
    return s;
}

async fn run() -> int {
    let mut total: int = 0;
    let mut i: int = 0;
    while i < 8 {
        let text: string = wide();
        let job: Task<int> = blocking { ret text.__len() to int; };
        total = total + compare job.await() {
            Success(v) => v;
            Cancelled() => 0;
        };
        i = i + 1;
    }
    return total;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    let total: int = compare t.await() {
        Success(v) => v;
        Cancelled() => 90;
    };
    if total != 1600 {
        print("FAIL blocking read-capture total");
        return 1;
    }
    print("blocking-read-capture-witness");
    return 0;
}
`

// The twin: the body CONSUMES its capture by handing it to a by-value callee.
// The callee releases it; the body's return must find nothing live, or the
// string is freed twice -- which is the row that would go red if the body's
// registration were added without the move tracking that pairs with it.
const runtimeV2BlockingConsumedCaptureSource = `
fn wide() -> string {
    let mut s: string = "";
    let mut i: int = 0;
    while i < 20 {
        s = s + "0123456789";
        i = i + 1;
    }
    return s;
}

fn eat(s: string) -> int {
    return s.__len() to int;
}

async fn run() -> int {
    let mut total: int = 0;
    let mut i: int = 0;
    while i < 8 {
        let text: string = wide();
        let job: Task<int> = blocking { ret eat(text); };
        total = total + compare job.await() {
            Success(v) => v;
            Cancelled() => 0;
        };
        i = i + 1;
    }
    return total;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    let total: int = compare t.await() {
        Success(v) => v;
        Cancelled() => 90;
    };
    if total != 1600 {
        print("FAIL blocking consumed-capture total");
        return 1;
    }
    print("blocking-consumed-capture-witness");
    return 0;
}
`

// A `@copy` value composite is captured by copy: the caller keeps its binding
// and reads it after the body has run. `@copy` admits only Copy members, so the
// copy owns no heap and nobody drops anything for it -- the row pins that the
// caller's later read is a read of an intact value and that the state block the
// copy travelled in is still reclaimed.
const runtimeV2BlockingCopyCompositeSource = `
@copy
type Pair = { a: int, b: int };

async fn run() -> int {
    let mut total: int = 0;
    let mut i: int = 0;
    while i < 8 {
        let p: Pair = Pair { a = i, b = 100 };
        let job: Task<int> = blocking { ret p.a + p.b; };
        let got: int = compare job.await() {
            Success(v) => v;
            Cancelled() => 0;
        };
        total = total + got + p.a;
        i = i + 1;
    }
    return total;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    let total: int = compare t.await() {
        Success(v) => v;
        Cancelled() => 90;
    };
    if total != 856 {
        print("FAIL blocking copy-composite total");
        return 1;
    }
    print("blocking-copy-composite-witness");
    return 0;
}
`

func TestRuntimeV2BlockingBodyLocalIsReclaimed(t *testing.T) {
	runBlockingReclamationProgram(t, runtimeV2BlockingBodyLocalSource, "blocking-body-local-witness")
}

func TestRuntimeV2BlockingReadCaptureIsReclaimed(t *testing.T) {
	runBlockingReclamationProgram(t, runtimeV2BlockingReadCaptureSource, "blocking-read-capture-witness")
}

func TestRuntimeV2BlockingConsumedCaptureIsReleasedOnce(t *testing.T) {
	runBlockingReclamationProgram(t, runtimeV2BlockingConsumedCaptureSource, "blocking-consumed-capture-witness")
}

func TestRuntimeV2BlockingCopyCompositeCaptureIsReclaimed(t *testing.T) {
	runBlockingReclamationProgram(t, runtimeV2BlockingCopyCompositeSource, "blocking-copy-composite-witness")
}

// The fifth shape, and the one the four rows above cannot reach: a capture that
// is Copy AND owns heap.
//
// The `@copy` composite row states the reason it is not that shape -- `@copy`
// admits only Copy members, so the copy owns no heap. The family that is both is
// the reference-counted one, and of its two members only the HANDLE arrives
// here: sema refuses a blocking capture carrying a reference-counted scalar,
// because that count is not atomic and the worker is another thread. So
// `Channel<T>` is the whole of it.
//
// The state literal RETAINS such a capture into the frame's field, and the
// body's unpack copies the handle word out without touching the count -- which
// is the frame handing its reference to the local. Nothing else can give it
// back: the worker claims the job's state cell before calling the body, so
// `blocking_job_release` frees the block without walking a field
// (runtime/native/rt_async_blocking.c). Either the body releases it at its
// return or the reference is abandoned, while the frame's lifecycle word says
// SPENT -- that nothing in it owns anything.
//
// MEASURED FROM INSIDE THE PROGRAM, not under valgrind, and that is the row's
// whole reason for being separate from the four above. The reference is
// ABANDONED, not lost: the executor's teardown frees what it holds at exit, so
// `definitely lost` reads 0 bytes in 0 blocks both with the release and without
// it. Under --show-leak-kinds=all the channel object does show as one 72-byte
// still-reachable record from rt_channel_new, present without the release and
// absent with it -- but the surrounding still-reachable figure moves with the
// scheduler's deques from run to run (two 128-byte rt_async_deque records
// differed between two runs of one binary), so it is not a number to assert on.
//
// WHY THE WINDOWS ARE 1 AND 129. The blocking pool leaves a few blocks of its
// own behind, and they do not scale with the round count. At 1-versus-9 the
// figure crossed between the two states from run to run -- 15 runs read
// `1 0 1 1 1 1 1 2 1 ...` against `2 2 2 2 2 2 1 2 ...`. Dividing by 128 instead
// of 8 puts that constant below the rounding, and the two states then separate
// with no overlap at all.
//
// WHY THE SHARD COUNT IS PINNED, which is the part that would have shipped a
// flaky gate. The figure is not the same on every configuration:
//
//	                      without the release   with it
//	SURGE_SHARDS=1                          2         1
//	SURGE_SHARDS=1 THREADS=2             1..2      0..1
//	SURGE_SHARDS=2 THREADS=2                1         0
//	SURGE_SHARDS=4 THREADS=4                1         0
//
// A default-environment row would therefore have read 1 here and 0 on any host
// whose default shard count is not 1, and the pin would have been a property of
// the machine. Two shards is where the accounting comes out exactly balanced, so
// that is what this row asks for and what it asserts: STRICT ZERO retained per
// round, 12 runs at 0 with the release and 12 at 1 without.
//
// The single-shard residue is one block per CAPTURED CHANNEL (two captures read
// two) and is not identified here. It is not the frame or the blocking
// machinery -- a body with no capture reads 0, and so does one whose capture is
// a `string` -- and not the channel's ordinary lifetime either, since a channel
// created and closed in an async fn with no `blocking` reads 0. It is left
// unnamed rather than explained by a story that fits.
const runtimeV2BlockingRetainedCaptureSource = `
async fn round_once() -> int {
    let ch: own Channel<int> = Channel::<int>::new(1:uint);
    let job: Task<int> = blocking {
        ch.close();
        ret 7;
    };
    return compare job.await() { Success(x) => x; Cancelled() => 0 - 1; };
}

async fn window(n: int) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut round: int = 0;
    while round < n {
        let r: int = compare round_once().await() { Success(x) => x; Cancelled() => 0 - 1; };
        if r != 7 { return 999999; }
        round = round + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    return c1.live_blocks - c0.live_blocks;
}

async fn run() -> int {
    let w1: uint = compare window(1).await() { Success(x) => x; Cancelled() => 999999; };
    let w129: uint = compare window(129).await() { Success(x) => x; Cancelled() => 999999; };
    if w1 >= 999000:uint || w129 >= 999000:uint {
        print("FAIL a blocking round did not answer 7");
        return 1;
    }
    let per_round: uint = (w129 - w1) / 128:uint;
    print("blocking retained capture census: retained per round=");
    print(per_round to string);
    if per_round != 0:uint {
        print("FAIL blocking retained capture retained blocks per round: ");
        print(per_round to string);
        return 1;
    }
    print("blocking-retained-capture-witness");
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

func TestRuntimeV2BlockingRetainedCaptureCensusBalanced(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2BlockingRetainedCaptureSource, nil)
	// Two shards, because that is the configuration in which the count this row
	// asserts is exactly zero; see the table above. The default is the host's,
	// and pinning a number against it would pin the host.
	env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "2")
	env = overrideEnvVar(env, "SURGE_THREADS", "2")
	duration, result := runBinaryWithTimeout(t, outputPath, env, 120*time.Second)
	if result.exitCode != 0 {
		t.Fatalf("blocking retained-capture census failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode, duration, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "blocking-retained-capture-witness") {
		t.Fatalf("missing completion marker; stdout=%q", result.stdout)
	}
}
