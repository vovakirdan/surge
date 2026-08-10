package vm_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"surge/internal/sema"
)

// The shared-handle vertical end to end under the production capability:
// two producers hold sibling leases of one capacity-1 channel, both park on
// capacity, and the original holder drains — the concurrent compiled
// park-retry proof that affine handles could not express. Cross-producer
// arrival order is deliberately asserted as a SET (the runtime promises
// owner-lane order, never cross-producer order), and the closed outcome
// ends the run.
const runtimeV2ShareSource = `
async fn producer(ch: far Channel<int>, value: int) -> int {
    let sent: TaskResult<nothing> = on ch { ch.send(value); ret nothing; };
    return compare sent { Success(_) => 0; Cancelled() => 1; };
}

async fn take_one(ch: far Channel<int>) -> int {
    let got: TaskResult<int> = on ch {
        let v: Option<int> = ch.recv();
        ret compare v { Some(x) => x; nothing => 0; };
    };
    return compare got { Success(x) => x; Cancelled() => 0; };
}

async fn run() -> int {
    let ch: far Channel<int> = channel_on::<int>(shard(0:ShardId), 1);
    let first: Task<int> = spawn producer(ch.share(), 41);
    let second: Task<int> = spawn producer(ch.share(), 42);
    let a: int = compare take_one(ch.share()).await() {
        Success(x) => x;
        Cancelled() => 0;
    };
    let b: int = compare take_one(ch.share()).await() {
        Success(x) => x;
        Cancelled() => 0;
    };
    let p1: int = compare first.await() { Success(x) => x; Cancelled() => 1; };
    let p2: int = compare second.await() { Success(x) => x; Cancelled() => 1; };
    if p1 != 0 { return 11; }
    if p2 != 0 { return 12; }
    if a + b != 83 { return 13; }
    if a == b { return 14; }
    if a != 41 { if a != 42 { return 15; } }
    let done: TaskResult<nothing> = on ch { ch.close(); ret nothing; };
    let _ = done;
    let after: TaskResult<int> = on ch {
        let v: Option<int> = ch.recv();
        ret compare v { Some(x) => x; nothing => 7; };
    };
    let closed: int = compare after { Success(x) => x; Cancelled() => 0; };
    if closed != 7 { return 16; }
    print("share-fanout-ok");
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

func TestRuntimeV2ShareFanOutAcrossShards(t *testing.T) {
	var forms map[sema.CrossingLoweringKind]bool
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2ShareSource, forms)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, shardCount := range []int{1, 2, 8} {
		t.Run(fmt.Sprintf("shards_%d", shardCount), func(t *testing.T) {
			value := strconv.Itoa(shardCount)
			env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
			env = overrideEnvVar(env, "SURGE_THREADS", value)
			duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
			if result.exitCode != 0 {
				t.Fatalf(
					"share fan-out e2e failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode,
					duration,
					result.stdout,
					result.stderr,
				)
			}
			if !strings.Contains(result.stdout, "share-fanout-ok") {
				t.Fatalf("share fan-out e2e missing completion marker; stdout=%q", result.stdout)
			}
		})
	}
}
