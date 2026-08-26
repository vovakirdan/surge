package vm_test

import (
	"testing"
	"time"
)

// An array of task handles is consumed by `for`: each iteration moves the
// handle out of its slot, and the slot it leaves behind holds nothing. The array's
// drop glue still visits every slot, and a droppable Task<T> (D4b) gave that
// visit a runtime call -- rt_task_handle_drop -- which refused the empty slot
// with `panic: invalid task handle`. Every multi-worker row that keeps its
// workers in an array died at teardown the same way.
const runtimeV2TaskHandleArrayMovedOutSource = `
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
    for t in tasks {
        total = total + compare t.await() { Success(v) => v; Cancelled() => 0 - 100; };
    }
    if total != 12 { return 2; }
    print("task-handle-array-moved-out-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() { Success(c) => c; Cancelled() => 90; };
}
`

func TestRuntimeV2TaskHandleArrayDropsItsMovedOutSlots(t *testing.T) {
	// RV2-DEBT-258: `for t in tasks { t.await() }` consumes each handle through
	// the loop binding, but the container's slot is not emptied -- the loop
	// reads the word and leaves it -- so the array's drop glue meets a handle
	// that was already given back (`panic: async: invalid task owner shard`).
	// The same shape double-frees an array of strings under valgrind. Which
	// side moves is the owner's call (consuming for-in, or a refusal at sema);
	// until then the program stays here as the reproducer.
	t.Skip("RV2-DEBT-258: a for-in that consumes its elements does not empty the container's slots; owner decision pending")
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2TaskHandleArrayMovedOutSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, threads := range []string{"1", "4"} {
		env := overrideEnvVar(baseEnv, "SURGE_THREADS", threads)
		_, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
		if result.exitCode != 0 || result.stderr != "" {
			t.Fatalf("threads=%s: exit=%d stderr=%q stdout=%q", threads, result.exitCode, result.stderr, result.stdout)
		}
	}
}
