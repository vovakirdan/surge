package vm_test

import (
	"testing"
	"time"
)

// An array of task handles is emptied by POPPING, never by `for`: a `for`
// loop only reads its elements (SEM3205 refuses a move out of the binding,
// owner ruling 2026-08-26, RV2-DEBT-258), and each `pop()` takes the handle
// out of its slot, so the array's drop glue meets only what is left.
//
// What is left is not nothing: `with_len` filled every slot with a default
// handle before `tasks[i] = spawn ...` overwrote it, and each overwrite drops
// that default through rt_task_handle_drop. A droppable Task<T> (D4b) once
// handed that NULL to task_from_handle and died with `panic: invalid task
// handle`; the guard (`TestRuntimeV2TaskHandleDropTreatsAnEmptySlotAsNothing`,
// a C stand) treats an empty slot as nothing, and this program is its
// end-to-end row: both lanes, one and four workers, clean teardown.
const runtimeV2TaskHandleArrayDrainedSource = `
async fn work(k: int) -> int {
    checkpoint().await();
    return k * 2;
}

async fn run() -> int {
    let mut tasks: Task<int>[] = Array::<Task<int>>::with_len(4:uint);
    let mut i: int = 0;
    while i < 4 {
        tasks[i] = spawn work(i);
        i = i + 1;
    }
    let mut total: int = 0;
    while tasks.__len() > 0:uint {
        let t = tasks.pop().safe();
        total = total + compare t.await() { Success(v) => v; Cancelled() => 0 - 100; };
    }
    if total != 12 { return 2; }
    if tasks.__len() != 0:uint { return 3; }
    print("task-handle-array-drained-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() { Success(c) => c; Cancelled() => 90; };
}
`

func TestRuntimeV2TaskHandleArrayDrainedByPopTearsDownClean(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2TaskHandleArrayDrainedSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, threads := range []string{"1", "4"} {
		env := overrideEnvVar(baseEnv, "SURGE_THREADS", threads)
		_, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
		if result.exitCode != 0 || result.stderr != "" {
			t.Fatalf("threads=%s: exit=%d stderr=%q stdout=%q", threads, result.exitCode, result.stderr, result.stdout)
		}
	}
}
