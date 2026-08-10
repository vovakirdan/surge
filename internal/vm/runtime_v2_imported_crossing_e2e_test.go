package vm_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The dependency scan must treat crossings in imported modules exactly like
// root-module crossings: under the production LLVM capability an async
// crossing that lives in a dependency compiles, lowers, and executes through
// the transport path — no guard misfire, no internal error, no hidden local
// fallback.
const runtimeV2ImportedCrossingModule = `
pragma module::remote;

pub async fn remote_pair() -> int {
    let value: int = compare on distributed {
        ret 21;
    } {
        Success(v) => v;
        Cancelled() => -1;
    };
    let task: far Task<int> = spawn on distributed {
        ret 21;
    };
    let second: int = compare task.await() {
        Success(v) => v;
        Cancelled() => -1;
    };
    return value + second;
}
`

const runtimeV2ImportedCrossingMain = `
import remote::remote_pair;

async fn run() -> int {
    let task = spawn remote_pair();
    return compare task.await() {
        Success(v) => v;
        Cancelled() => -1;
    };
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    let got = compare task.await() {
        Success(v) => v;
        Cancelled() => -2;
    };
    if got != 42 {
        return 1;
    }
    print("imported-crossing-ok");
    return 0;
}
`

func TestRuntimeV2ImportedCrossingProductionCapability(t *testing.T) {
	outputPath := buildRuntimeV2CrossingProject(
		t,
		runtimeV2ImportedCrossingMain,
		map[string]string{"remote": runtimeV2ImportedCrossingModule},
		nil,
	)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, shardCount := range []int{1, 2, 8} {
		t.Run(fmt.Sprintf("shards_%d", shardCount), func(t *testing.T) {
			value := strconv.Itoa(shardCount)
			env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
			env = overrideEnvVar(env, "SURGE_THREADS", value)
			duration, result := runBinaryWithTimeout(t, outputPath, env, 20*time.Second)
			if result.exitCode != 0 {
				t.Fatalf(
					"imported crossing e2e failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode,
					duration,
					result.stdout,
					result.stderr,
				)
			}
			if !strings.Contains(result.stdout, "imported-crossing-ok") {
				t.Fatalf("imported crossing e2e missing completion marker; stdout=%q", result.stdout)
			}
		})
	}
}
