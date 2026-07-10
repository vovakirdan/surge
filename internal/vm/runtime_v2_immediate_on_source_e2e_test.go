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

const runtimeV2ImmediateOnSource = `
async fn exercise_immediate_on() -> int {
    let same_shard: int = compare on shard(0:ShardId) {
        ret 21;
    } {
        Success(value) => value;
        Cancelled() => -1;
    };
    if same_shard != 21 {
        return 11;
    }

    let distributed_value: int = compare on distributed {
        ret 56;
    } {
        Success(value) => value;
        Cancelled() => -1;
    };
    if distributed_value != 56 {
        return 12;
    }

    let copy_base: int = 40;
    let copy_value: int = compare on distributed {
        ret copy_base + 2;
    } {
        Success(value) => value;
        Cancelled() => -1;
    };
    if copy_value != 42 {
        return 13;
    }

    let out_of_range: int = compare on shard(4096:ShardId) {
        ret 99;
    } {
        Success(_) => -2;
        Cancelled() => 77;
    };
    if out_of_range != 77 {
        return 14;
    }

    print("immediate-on-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn exercise_immediate_on();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

func TestRuntimeV2ImmediateOnSourceOverrideAcrossShards(t *testing.T) {
	forms := map[sema.CrossingLoweringKind]bool{
		sema.CrossingLoweringOnPlacement: true,
	}
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2ImmediateOnSource, forms)
	runRuntimeV2ImmediateOnSourceMatrix(t, outputPath)
}

func TestRuntimeV2ImmediateOnSourceProductionCapability(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2ImmediateOnSource, nil)
	runRuntimeV2ImmediateOnSourceMatrix(t, outputPath)
}

func runRuntimeV2ImmediateOnSourceMatrix(t *testing.T, outputPath string) {
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
					"immediate-on source e2e failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode,
					duration,
					result.stdout,
					result.stderr,
				)
			}
			if !strings.Contains(result.stdout, "immediate-on-ok") {
				t.Fatalf("immediate-on source e2e missing completion marker; stdout=%q", result.stdout)
			}
		})
	}
}

// Post-flip, `on pool` compiles under the production LLVM capability (the
// gate keys on the crossing form, not the destination kind) and must fail
// deterministically at placement resolution — no hidden local fallback.
func TestRuntimeV2ImmediateOnPoolProductionCapabilityFailsDeterministically(t *testing.T) {
	source := `
async fn run_pool() -> int {
    return compare on pool {
        ret 1;
    } {
        Success(value) => value;
        Cancelled() => -1;
    };
}

@entrypoint
fn main() -> int {
    let task = spawn run_pool();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`
	outputPath := buildRuntimeV2CrossingSource(t, source, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnvVar(baseEnv, "SURGE_SHARDS", "2")
	env = overrideEnvVar(env, "SURGE_THREADS", "2")
	duration, result := runBinaryWithTimeout(t, outputPath, env, 20*time.Second)
	if result.exitCode == 0 {
		t.Fatalf(
			"on pool must not succeed on the placement-task vertical (duration=%s)\nstdout:\n%s",
			duration,
			result.stdout,
		)
	}
	if !strings.Contains(result.stderr, "on placement is not supported by this backend") {
		t.Fatalf(
			"on pool must fail with the deterministic unsupported-placement panic; stderr=%q",
			result.stderr,
		)
	}
}
