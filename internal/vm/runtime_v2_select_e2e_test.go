package vm_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"surge/internal/sema"
)

// The remote-select vertical end to end under the production capability: the
// select ships to the arms' owner shard as one anchored block and resumes
// with the winner index driving the caller-side arm dispatch. The first
// select decides on a ready arm (readiness consume, value not surfaced —
// the local select contract); the second parks owner-side and a sibling
// producer wakes it.
const runtimeV2SelectSource = `
async fn feed(ch: far Channel<int>, value: int) -> int {
    let sent: TaskResult<nothing> = on ch { ch.send(value); ret nothing; };
    return compare sent { Success(_) => 0; Cancelled() => 1; };
}

async fn pick(a: far Channel<int>, b: far Channel<int>) -> int {
    let w: int = select { a.recv() => 10; b.recv() => 20; };
    return w;
}

async fn run() -> int {
    let a: far Channel<int> = channel_on::<int>(shard(0:ShardId), 2);
    let b: far Channel<int> = channel_on::<int>(shard(0:ShardId), 2);
    let fed: TaskResult<nothing> = on b { b.send(7); ret nothing; };
    let _ = fed;
    let first: int = compare pick(a.share(), b.share()).await() {
        Success(x) => x;
        Cancelled() => 0;
    };
    if first != 20 { return 11; }
    let feeder: Task<int> = spawn feed(a.share(), 41);
    let second: int = compare pick(a.share(), b.share()).await() {
        Success(x) => x;
        Cancelled() => 0;
    };
    if second != 10 { return 12; }
    let fres: int = compare feeder.await() { Success(x) => x; Cancelled() => 1; };
    if fres != 0 { return 13; }
    print("remote-select-ok");
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

func TestRuntimeV2RemoteSelectFanInAcrossShards(t *testing.T) {
	var forms map[sema.CrossingLoweringKind]bool
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2SelectSource, forms)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, shardCount := range []int{1, 2, 8} {
		t.Run(fmt.Sprintf("shards_%d", shardCount), func(t *testing.T) {
			value := strconv.Itoa(shardCount)
			env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
			env = overrideEnvVar(env, "SURGE_THREADS", value)
			duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
			if result.exitCode != 0 {
				t.Fatalf(
					"remote select e2e failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode,
					duration,
					result.stdout,
					result.stderr,
				)
			}
			if !strings.Contains(result.stdout, "remote-select-ok") {
				t.Fatalf("remote select e2e missing completion marker; stdout=%q", result.stdout)
			}
		})
	}
}
