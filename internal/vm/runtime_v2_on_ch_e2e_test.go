//go:build !golden

package vm_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The anchored-op vertical end to end under the production capability (no
// test-scoped override): mint a channel, run `on ch` send/send/recv/recv/
// close/recv blocks from a non-owner caller, and observe single-producer
// FIFO plus the closed outcome. The handle is affine, so all blocks share
// one sequential holder; the compiled park path (a second holder draining
// a full channel) is blocked on copyable far handles and is covered at the
// harness level by the helper-protocol row, which drives the exact helper
// sequence compiled bodies emit. The recv result is unwrapped in-body: the
// reply payload must stay plain-copy data (a union value would ship an
// owner-heap pointer across shards), so `ret ch.recv()` stays behind the
// payload gate until unions get a by-value wire representation.
const runtimeV2OnChSource = `
async fn run() -> int {
    let ch: far Channel<int> = channel_on::<int>(shard(0:ShardId), 4);
    let s1: TaskResult<nothing> = on ch { ch.send(41); ret nothing; };
    let _ = s1;
    let s2: TaskResult<nothing> = on ch { ch.send(42); ret nothing; };
    let _ = s2;
    let r1: TaskResult<int> = on ch {
        let v: Option<int> = ch.recv();
        ret compare v { Some(x) => x; nothing => 101; };
    };
    let v1: int = compare r1 { Success(x) => x; Cancelled() => 102; };
    let r2: TaskResult<int> = on ch {
        let v: Option<int> = ch.recv();
        ret compare v { Some(x) => x; nothing => 101; };
    };
    let v2: int = compare r2 { Success(x) => x; Cancelled() => 102; };
    let done: TaskResult<nothing> = on ch { ch.close(); ret nothing; };
    let _ = done;
    let r3: TaskResult<int> = on ch {
        let v: Option<int> = ch.recv();
        ret compare v { Some(x) => x; nothing => 7; };
    };
    let closed: int = compare r3 { Success(x) => x; Cancelled() => 102; };
    if v1 == 41 {
        if v2 == 42 {
            if closed == 7 {
                print("on-ch-ok");
                return 0;
            }
        }
    }
    return 1;
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

func TestRuntimeV2OnChAnchoredOpsAcrossShards(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2OnChSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, shardCount := range []int{1, 2, 8} {
		t.Run(fmt.Sprintf("shards_%d", shardCount), func(t *testing.T) {
			value := strconv.Itoa(shardCount)
			env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
			env = overrideEnvVar(env, "SURGE_THREADS", value)
			duration, result := runBinaryWithTimeout(t, outputPath, env, 20*time.Second)
			if result.exitCode != 0 {
				t.Fatalf(
					"on-ch e2e failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode,
					duration,
					result.stdout,
					result.stderr,
				)
			}
			if !strings.Contains(result.stdout, "on-ch-ok") {
				t.Fatalf("on-ch e2e missing completion marker; stdout=%q", result.stdout)
			}
		})
	}
}
