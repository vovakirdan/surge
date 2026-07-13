//go:build !golden

package vm_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"surge/internal/sema"
)

// The migration vertical end to end: an owned @shard_movable value is built
// locally and MOVED into crossing bodies (spawn on and immediate on) that
// process it on another shard — the exact shape the old FUT7020 guard named
// as "build the value on the destination" because it could not ship. The
// source binding is moved-from after the crossing per affine semantics.
const runtimeV2MigrationSource = `
@shard_movable
type Job = { id: int, weight: int };

fn describe(j: own Job) -> int { return j.id * 100 + j.weight; }

async fn run() -> int {
    let j: own Job = own Job{ id: 4, weight: 2 };
    let t: far Task<int> = spawn on distributed { ret describe(own j); };
    let r: int = compare t.await() { Success(x) => x; Cancelled() => 0; };
    if r != 402 { return 11; }
    let j2: own Job = own Job{ id: 7, weight: 5 };
    let got: TaskResult<int> = on distributed { ret describe(own j2); };
    let second: int = compare got { Success(x) => x; Cancelled() => 0; };
    if second != 705 { return 12; }
    print("migration-ok");
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

func TestRuntimeV2OwnedCaptureMigrationAcrossShards(t *testing.T) {
	var forms map[sema.CrossingLoweringKind]bool
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2MigrationSource, forms)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, shardCount := range []int{1, 2, 8} {
		t.Run(fmt.Sprintf("shards_%d", shardCount), func(t *testing.T) {
			value := strconv.Itoa(shardCount)
			env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
			env = overrideEnvVar(env, "SURGE_THREADS", value)
			duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
			if result.exitCode != 0 {
				t.Fatalf(
					"migration e2e failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode,
					duration,
					result.stdout,
					result.stderr,
				)
			}
			if !strings.Contains(result.stdout, "migration-ok") {
				t.Fatalf("migration e2e missing completion marker; stdout=%q", result.stdout)
			}
		})
	}
}
