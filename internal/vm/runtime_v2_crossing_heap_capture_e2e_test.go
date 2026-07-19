//go:build !golden

package vm_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Heap-bearing captures end to end: a @shard_movable struct whose field is
// a runtime-built string is MOVED into a spawn-on body that reads BOTH the
// Copy field and the heap field. All-Copy captures ship the box pointer and
// never dereference the heap, so only this shape proves the capture box
// survives until the body consumes it (the caller must not drop the box it
// transferred into the shipped state).
const runtimeV2CrossingHeapCaptureSource = `
@shard_movable
type Job = { id: int, note: string };

fn build_note(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

fn describe(j: own Job) -> int {
    if j.note != "n-xxxx" { return 0 - 1; }
    return j.id * 100 + 6;
}

async fn far_loop(n: int) -> int {
    let mut k = 0;
    let mut acc = 0;
    while k < n {
        let j: own Job = own Job{ id: 4, note: build_note("n-") };
        let t: far Task<int> = spawn on distributed { ret describe(own j); };
        let got: TaskResult<int> = t.await();
        acc = acc + compare got { Success(x) => x; Cancelled() => 0 - 1; };
        k = k + 1;
    }
    return acc;
}

async fn run() -> int {
    let r1: TaskResult<int> = far_loop(1).await();
    let one: int = compare r1 { Success(x) => x; Cancelled() => 0; };
    if one != 406 { return 11; }
    let r8: TaskResult<int> = far_loop(8).await();
    let eight: int = compare r8 { Success(x) => x; Cancelled() => 0; };
    if eight != 8 * 406 { return 12; }
    print("crossing-heap-capture-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

// The census twin: with RV2-DEBT-052 fixed (the consumed TaskResult box
// no longer leaks), the far loop's per-iteration heap growth is exactly
// zero — d(8) == d(1) — and so is the local loop's — b(8) == b(1) —
// checked independently rather than merely equal to each other, a
// strictly stronger claim than the original cross-comparison (that form
// would still pass if both sides leaked the SAME nonzero amount per
// iteration). Measured with RV2-DEBT-052 fixed: d(1)=d(8)=1,
// b(1)=b(8)=3 — both flat (not growing with n), but not literally zero;
// each is a fixed, n-independent window-edge/setup cost (unrelated to
// this row, and NOT re-introduced by this fix), left unpinned here.
const runtimeV2CrossingHeapCaptureCensusSource = `
@shard_movable
type Job = { id: int, note: string };

fn build_note(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

fn describe(j: own Job) -> int {
    if j.note != "n-xxxx" { return 0 - 1; }
    return j.id * 100 + 6;
}

async fn local_make(v: int) -> int { return v; }

async fn far_window(n: int) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut k = 0;
    let mut acc = 0;
    while k < n {
        let j: own Job = own Job{ id: 4, note: build_note("n-") };
        let t: far Task<int> = spawn on distributed { ret describe(own j); };
        let got: TaskResult<int> = t.await();
        acc = acc + compare got { Success(x) => x; Cancelled() => 0 - 1; };
        k = k + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    if acc != n * 406 { return 999999; }
    return (c1.alloc_count - c0.alloc_count) - (c1.free_count - c0.free_count);
}

async fn local_window(n: int) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut k = 0;
    let mut acc = 0;
    while k < n {
        let t: Task<int> = spawn local_make(406);
        let got: TaskResult<int> = t.await();
        acc = acc + compare got { Success(x) => x; Cancelled() => 0 - 1; };
        k = k + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    if acc != n * 406 { return 999999; }
    return (c1.alloc_count - c0.alloc_count) - (c1.free_count - c0.free_count);
}

async fn run() -> int {
    let rd1: TaskResult<uint> = far_window(1).await();
    let rd8: TaskResult<uint> = far_window(8).await();
    let rb1: TaskResult<uint> = local_window(1).await();
    let rb8: TaskResult<uint> = local_window(8).await();
    let d1: uint = compare rd1 { Success(x) => x; Cancelled() => 888888; };
    let d8: uint = compare rd8 { Success(x) => x; Cancelled() => 888888; };
    let b1: uint = compare rb1 { Success(x) => x; Cancelled() => 888888; };
    let b8: uint = compare rb8 { Success(x) => x; Cancelled() => 888888; };
    if d1 >= 888888 || d8 >= 888888 || b1 >= 888888 || b8 >= 888888 {
        print("FAIL result");
        return 1;
    }
    if d8 != d1 {
        print("FAIL census far growth d1=");
        print(d1 to string);
        print(" d8=");
        print(d8 to string);
        return 2;
    }
    if b8 != b1 {
        print("FAIL census local growth b1=");
        print(b1 to string);
        print(" b8=");
        print(b8 to string);
        return 3;
    }
    print("crossing-heap-capture-census-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

func TestRuntimeV2CrossingHeapCaptureArrivesIntact(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CrossingHeapCaptureSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, shardCount := range []int{1, 2, 8} {
		t.Run(fmt.Sprintf("shards_%d", shardCount), func(t *testing.T) {
			value := strconv.Itoa(shardCount)
			env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
			env = overrideEnvVar(env, "SURGE_THREADS", value)
			duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
			if result.exitCode != 0 {
				t.Fatalf(
					"heap capture e2e failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode,
					duration,
					result.stdout,
					result.stderr,
				)
			}
			if !strings.Contains(result.stdout, "crossing-heap-capture-ok") {
				t.Fatalf("heap capture e2e missing completion marker; stdout=%q", result.stdout)
			}
		})
	}
}

func TestRuntimeV2CrossingHeapCaptureCensusBalanced(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CrossingHeapCaptureCensusSource, nil)
	// Single shard only: with more shards the transport releases its
	// request/ack payloads on other lanes, so those frees race the c1
	// snapshot and the window comparison stops being deterministic.
	env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")
	duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
	if result.exitCode != 0 {
		t.Fatalf(
			"heap capture census failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode,
			duration,
			result.stdout,
			result.stderr,
		)
	}
	if !strings.Contains(result.stdout, "crossing-heap-capture-census-ok") {
		t.Fatalf("heap capture census missing completion marker; stdout=%q", result.stdout)
	}
}
