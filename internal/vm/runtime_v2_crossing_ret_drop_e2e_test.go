//go:build !golden

package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A crossing body's ONLY exit is `ret`, and until now `ret` discharged no
// ownership obligations at all: sema suppressed every drop recording inside a
// crossing body, and even where it had not, `StmtRet` carried no drop list
// through HIR or MIR. So anything a crossing body allocated and still held when
// it produced its result was abandoned.
//
// The body below builds two strings per crossing — one bound to a local, one a
// temporary consumed by the comparison — and both were lost, measured at 16
// blocks over 8 crossings with valgrind before the fix.
//
// The note is 200 characters, and that number is load-bearing rather than
// arbitrary: a short string is stored INLINE and owns no block, so a shorter
// one leaves nothing to lose and the census stays flat while the value is
// abandoned exactly as it is here. An earlier draft of this test used a
// 6-character string, passed, and proved nothing.
const runtimeV2CrossingRetDropCensusSource = `
fn build_note(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 200 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

async fn far_window(n: int) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut k = 0;
    let mut acc = 0;
    while k < n {
        let t: far Task<int> = spawn on distributed {
            let held: string = build_note("L-");
            if held != build_note("L-") { ret 0 - 1; }
            ret 406;
        };
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
    let d1: uint = compare rd1 { Success(x) => x; Cancelled() => 888888; };
    let d8: uint = compare rd8 { Success(x) => x; Cancelled() => 888888; };
    if d1 >= 888888 || d8 >= 888888 {
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
    print("crossing-ret-drop-census-ok");
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

// The window must not grow with the iteration count. A per-crossing leak shows
// up as d(8) - d(1) == 7 * (blocks abandoned per crossing); a fixed setup cost
// cancels out, which is why the claim is "flat", not "zero". Before the fix
// this reported d1=3, d8=17.
func TestRuntimeV2CrossingRetDischargesBodyDrops(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CrossingRetDropCensusSource, nil)
	// Single shard only, for the same reason the other crossing censuses give:
	// with more shards the transport releases request/ack payloads on other
	// lanes and those frees race the c1 snapshot.
	env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")
	duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
	if result.exitCode != 0 {
		t.Fatalf(
			"crossing ret-drop census failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode,
			duration,
			result.stdout,
			result.stderr,
		)
	}
	if !strings.Contains(result.stdout, "crossing-ret-drop-census-ok") {
		t.Fatalf("crossing ret-drop census missing completion marker; stdout=%q", result.stdout)
	}
}

// The negative control for the row above, and the reason it is a separate
// program rather than an extra assertion: an implementation that drops too
// EAGERLY passes the census (nothing is lost) while corrupting the value. Here
// the body moves an owned capture ONWARD into a call that consumes it, so a
// body-side drop of the same value would be a double free rather than a leak,
// and the capture's contents are checked on the destination shard, so a
// premature release shows up as a wrong answer instead of silence.
//
// The body also takes a MIXED shape on purpose: one exit hands the capture on,
// the other returns without it, and both exits have a body-local droppable
// live. An implementation that collects exit obligations per-exit passes; one
// that computes a single obligation list for the whole body either frees the
// moved capture on the path that gave it away, or abandons the local on the
// path that did not.
const runtimeV2CrossingRetDropControlSource = `
@shard_movable
type Job = { id: int, note: string };

fn build_note(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 200 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

fn describe(j: own Job) -> int {
    if j.note != build_note("n-") { return 0 - 1; }
    return j.id * 100 + 6;
}

async fn far_window(n: int) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut k = 0;
    let mut acc = 0;
    while k < n {
        let j: own Job = own Job{ id: 4, note: build_note("n-") };
        let t: far Task<int> = spawn on distributed {
            let mark: string = build_note("t-");
            if mark != build_note("t-") { ret 0 - 1; }
            ret describe(own j);
        };
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
    let d1: uint = compare rd1 { Success(x) => x; Cancelled() => 888888; };
    let d8: uint = compare rd8 { Success(x) => x; Cancelled() => 888888; };
    if d1 >= 888888 || d8 >= 888888 {
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
    print("crossing-ret-drop-control-ok");
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

func TestRuntimeV2CrossingRetDropsDoNotStealMovedValues(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CrossingRetDropControlSource, nil)
	env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")
	duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
	if result.exitCode != 0 {
		t.Fatalf(
			"crossing ret-drop control failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode,
			duration,
			result.stdout,
			result.stderr,
		)
	}
	if !strings.Contains(result.stdout, "crossing-ret-drop-control-ok") {
		t.Fatalf("crossing ret-drop control missing completion marker; stdout=%q", result.stdout)
	}
}
