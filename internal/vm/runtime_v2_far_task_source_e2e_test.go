package vm_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"surge/internal/sema"
)

const runtimeV2FarTaskSource = `
fn take_owned_int(value: own int) -> int {
    return value;
}

async fn exercise_far_tasks() -> int {
    let same_shard: far Task<int> = spawn on shard(0:ShardId) {
        ret 21;
    };
    let same_value: int = compare same_shard.await() {
        Success(value) => value;
        Cancelled() => -1;
    };
    if same_value != 21 {
        return 11;
    }

    let distributed_task: far Task<int> = spawn on distributed {
        ret 56;
    };
    let distributed_value: int = compare distributed_task.await() {
        Success(value) => value;
        Cancelled() => -1;
    };
    if distributed_value != 56 {
        return 12;
    }

    let copy_base: int = 40;
    let copy_task: far Task<int> = spawn on distributed {
        ret copy_base + 2;
    };
    let copy_value: int = compare copy_task.await() {
        Success(value) => value;
        Cancelled() => -1;
    };
    if copy_value != 42 {
        return 13;
    }

    let owned_base: own int = 34;
    let owned_task: far Task<int> = spawn on distributed {
        ret take_owned_int(own owned_base) + 1;
    };
    let owned_value: int = compare owned_task.await() {
        Success(value) => value;
        Cancelled() => -1;
    };
    if owned_value != 35 {
        return 14;
    }

    let cancel_task: far Task<int> = spawn on distributed {
        ret 99;
    };
    compare cancel_task.cancel() {
        Success(_) => print("cancel-completed");
        Cancelled() => print("cancelled");
    }

    print("far-task-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn exercise_far_tasks();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

func TestRuntimeV2FarTaskSourceOverrideAcrossShards(t *testing.T) {
	forms := map[sema.CrossingLoweringKind]bool{
		sema.CrossingLoweringSpawnOn:       true,
		sema.CrossingLoweringFarTaskAwait:  true,
		sema.CrossingLoweringFarTaskCancel: true,
	}
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2FarTaskSource, forms)
	runRuntimeV2FarTaskSourceMatrix(t, outputPath)
}

func TestRuntimeV2FarTaskSourceProductionCapability(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2FarTaskSource, nil)
	runRuntimeV2FarTaskSourceMatrix(t, outputPath)
}

const runtimeV2SpawnOnPoolSource = `
async fn spawn_pool() -> int {
    let task: far Task<int> = spawn on pool {
        ret 1;
    };
    return compare task.await() {
        Success(value) => value;
        Cancelled() => -1;
    };
}

@entrypoint
fn main() -> int {
    let task = spawn spawn_pool();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

// Post-flip, `spawn on pool` compiles under the production LLVM capability
// (the gate keys on the crossing form, not the destination kind) and must
// fail deterministically at placement resolution, not run on a hidden local
// fallback.
func TestRuntimeV2SpawnOnPoolProductionCapabilityFailsDeterministically(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2SpawnOnPoolSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, shardCount := range []int{1, 2} {
		t.Run(fmt.Sprintf("shards_%d", shardCount), func(t *testing.T) {
			value := strconv.Itoa(shardCount)
			env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
			env = overrideEnvVar(env, "SURGE_THREADS", value)
			duration, result := runBinaryWithTimeout(t, outputPath, env, 20*time.Second)
			if result.exitCode == 0 {
				t.Fatalf(
					"spawn on pool must not succeed on the placement-task vertical (duration=%s)\nstdout:\n%s",
					duration,
					result.stdout,
				)
			}
			if !strings.Contains(result.stderr, "spawn on placement is not supported by this backend") {
				t.Fatalf(
					"spawn on pool must fail with the deterministic unsupported-placement panic; stderr=%q",
					result.stderr,
				)
			}
		})
	}
}

func runRuntimeV2FarTaskSourceMatrix(t *testing.T, outputPath string) {
	t.Helper()
	baseEnv := envWithStdlib(repoRoot(t))

	for _, shardCount := range []int{1, 2, 8} {
		t.Run(fmt.Sprintf("shards_%d", shardCount), func(t *testing.T) {
			value := strconv.Itoa(shardCount)
			env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
			env = overrideEnvVar(env, "SURGE_THREADS", value)
			duration, result := runBinaryWithTimeout(t, outputPath, env, 20*time.Second)
			if result.exitCode != 0 {
				t.Fatalf(
					"far-task source e2e failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode,
					duration,
					result.stdout,
					result.stderr,
				)
			}
			if !strings.Contains(result.stdout, "far-task-ok") {
				t.Fatalf("far-task source e2e missing completion marker; stdout=%q", result.stdout)
			}
			if !strings.Contains(result.stdout, "cancel-completed") &&
				!strings.Contains(result.stdout, "cancelled") {
				t.Fatalf("far-task source e2e did not materialize cancel result; stdout=%q", result.stdout)
			}
		})
	}
}
