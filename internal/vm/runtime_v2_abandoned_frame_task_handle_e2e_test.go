package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A frame a cancellation abandons can hold task handles: the clone a body
// made and did not get to await, and the handle the interrupted await itself
// was consuming. Each is a reference the runtime counts, and a reference
// nobody gives back keeps the task -- and the result it produced -- allocated
// for the life of the process.
//
// The body below is cancelled before its first poll, so its first poll runs
// straight into `t.await()` with `s` live across the suspend point and
// abandons its frame there. The frame is destroyed through the state's own
// descriptor, and that walk now reaches both task handles and gives their
// references back through rt_task_handle_drop, so the orphaned child is freed
// the moment it finishes. Measured as live heap blocks retained per round,
// from inside the program, because a task the runtime still holds at exit is
// freed by the executor's teardown and is invisible to a leak checker:
//
//	without the release: 4 blocks per round, and every orphaned child is still
//	                     DONE and allocated at exit (tasks_done=10 of 10);
//	with it:             2 blocks per round, tasks_done=0.
//
// The two that remain belong to a body cancelled before it ever started and
// are the same with or without the handles; they are not this row's.
const runtimeV2AbandonedFrameTaskHandleSource = `
async fn work() -> string {
    let mut i: int = 0;
    while i < 8 {
        checkpoint().await();
        i = i + 1;
    }
    return "finished";
}

async fn outer() -> int {
    let t: Task<string> = spawn work();
    let s: Task<string> = t.clone();
    let a: string = compare t.await() { Success(v) => v; Cancelled() => "cancelled"; };
    let b: string = compare s.await() { Success(v) => v; Cancelled() => "cancelled"; };
    if a != b { return 1; }
    return 0;
}

async fn window(n: int) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut round: int = 0;
    while round < n {
        let o: Task<int> = spawn outer();
        o.cancel();
        let r: int = compare o.await() { Success(_) => 2; Cancelled() => 0; };
        if r != 0 { return 999999; }
        // Let the orphaned child run to completion, so a handle nobody gave
        // back would be holding a DONE task and its result.
        let mut k: int = 0;
        while k < 12 {
            checkpoint().await();
            k = k + 1;
        }
        round = round + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    return c1.live_blocks - c0.live_blocks;
}

async fn run() -> int {
    let w1: uint = compare window(1).await() { Success(x) => x; Cancelled() => 999999; };
    let w9: uint = compare window(9).await() { Success(x) => x; Cancelled() => 999999; };
    if w1 >= 999000 || w9 >= 999000 {
        print("FAIL a cancelled body did not resolve Cancelled");
        return 1;
    }
    let per_round: uint = (w9 - w1) / 8:uint;
    print("abandoned frame census: retained per round=");
    print(per_round to string);
    // Pinned exactly, so the next reduction collapses this loudly rather than
    // passing quietly. Four is what a frame that walks past its task handles
    // retains: the DONE child and its result, on top of the two below.
    if per_round != 2:uint {
        print("FAIL abandoned frame retained blocks per round: ");
        print(per_round to string);
        return 1;
    }
    print("abandoned-frame-task-handle-ok");
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

func TestRuntimeV2AbandonedFrameReleasesItsTaskHandles(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2AbandonedFrameTaskHandleSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnvVar(baseEnv, "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")
	duration, result := runBinaryWithTimeout(t, outputPath, env, 60*time.Second)
	if result.exitCode != 0 {
		t.Fatalf("abandoned frame census failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode, duration, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "abandoned-frame-task-handle-ok") {
		t.Fatalf("abandoned frame census missing completion marker; stdout=%q", result.stdout)
	}
}
