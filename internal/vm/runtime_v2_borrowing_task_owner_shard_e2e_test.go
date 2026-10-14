package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// A borrowing child inherits its creator's owner shard. Runtime V2 has one
// carrier per shard, so owner placement alone keeps the child beside the frame
// it reads; the parent joins it on every path, as task_borrow_pin.go requires.
const runtimeV2BorrowingTaskOwnerShardSource = `async fn plus_one(x: &int) -> int {
    return *x + 1;
}

async fn main_async() -> int {
    let v: int = 41;
    let t = spawn plus_one(&v);
    let r = t.await();
    let got = compare r {
        Success(n) => n;
        Cancelled() => -1;
    };
    if got != 42 {
        return 1;
    }
    print("ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let r = main_async().await();
    let code = compare r {
        Success(v) => v;
        Cancelled() => 99;
    };
    return code;
}
`

func TestRuntimeV2BorrowingTaskStaysOnOwnerShardAndAnswers(t *testing.T) {
	ensureLLVMToolchain(t)
	skipTimeoutTests(t)
	outputPath := buildLLVMProgramFromSource(t, runtimeV2BorrowingTaskOwnerShardSource)
	for _, shards := range []int{1, 2, 8} {
		t.Run(fmt.Sprintf("shards-%d", shards), func(t *testing.T) {
			env := envWithStdlib(repoRoot(t))
			env = overrideEnvVar(env, "SURGE_SHARDS", fmt.Sprintf("%d", shards))
			env = overrideEnvVar(env, "SURGE_BLOCKING_THREADS", "1")
			dur, res := runBinaryWithTimeout(t, outputPath, env, 20*time.Second)
			if res.exitCode != 0 {
				t.Fatalf("borrow spawn failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
					res.exitCode, dur, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stdout, "ok") {
				t.Fatalf("unexpected stdout: %q", res.stdout)
			}
		})
	}
}
