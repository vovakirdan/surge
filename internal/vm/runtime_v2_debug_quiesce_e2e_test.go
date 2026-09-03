package vm_test

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// rt_debug_quiesce is the barrier an observer of a process-wide counter
// takes before reading it (RV2-DEBT-330): it returns once no task but the
// caller's own is running on any shard, nothing is runnable or being
// published, no inbound envelope is undrained and the blocking pool is idle,
// and it answers how many samples it took. Here a task awaits a spawned
// child that has already finished, calls the barrier, and reads a counter
// of at least one; a second call, with nothing having moved, answers
// exactly one. The row exists so the primitive is reached by a program on
// the shipping path and not only declared: the paired benchmark's shared
// fixture cannot call it yet (the base compiler does not know it), so
// nothing else does.
const runtimeV2DebugQuiesceSource = `async fn child() -> int {
    let _ = checkpoint().await();
    return 7;
}

async fn main_async() -> int {
    let task = spawn child();
    let v = compare task.await() { Success(x) => x; Cancelled() => 0; };
    let first: uint64 = rt_debug_quiesce();
    let second: uint64 = rt_debug_quiesce();
    if v != 7 { return 2; }
    if first < 1:uint64 { return 3; }
    if second != 1:uint64 { return 4; }
    print("quiesced");
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn main_async();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

func TestRuntimeV2DebugQuiesceAnswers(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2DebugQuiesceSource, nil)
	for _, shards := range []int{1, 2} {
		env := append(envWithStdlib(repoRoot(t)),
			"SURGE_SHARDS="+strconv.Itoa(shards), "SURGE_THREADS="+strconv.Itoa(shards), "SURGE_BLOCKING_THREADS=1")
		_, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
		if result.exitCode != 0 || !strings.Contains(result.stdout, "quiesced") {
			t.Fatalf("shards=%d: exit=%d\nstdout:\n%s\nstderr:\n%s", shards, result.exitCode, result.stdout, result.stderr)
		}
	}
}
