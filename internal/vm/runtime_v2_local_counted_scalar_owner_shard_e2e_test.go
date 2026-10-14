//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"testing"
	"time"
)

const runtimeV2LocalCountedScalarOwnerShardSource = `async fn churn(x: int, rounds: int) -> int {
    let mut i: int = 0;
    let mut ok: int = 0;
    while i < rounds {
        let y: int = x;
        if y == x { ok = ok + 1; };
        i = i + 1;
    };
    return ok;
}

async fn main_async() -> int {
    let shared: int = 9223372036854775808;
    let rounds: int = 200000;
    let child = spawn churn(shared, rounds);
    let mut i: int = 0;
    let mut ok: int = 0;
    while i < rounds {
        let y: int = shared;
        if y == shared { ok = ok + 1; };
        i = i + 1;
    };
    return compare child.await() { Success(n) => ok + n; Cancelled() => -1; };
}

@entrypoint
fn main() -> int {
    return compare main_async().await() {
        Success(n) => n == 400000 ? 0 : 2;
        Cancelled() => 3;
    };
}
`

// RV2-DEBT-452: local tasks keep one owner shard and therefore one carrier.
// The pre-cutover `1 shard x 8 carriers` tree fails this exact class 8/10 in
// the recorded counterfactual, with SIGSEGV or bigint release below zero.
func TestRuntimeV2LocalCountedScalarStaysOnOneCarrier(t *testing.T) {
	ensureLLVMToolchain(t)
	skipTimeoutTests(t)
	bin := buildLLVMProgramFromSource(t, runtimeV2LocalCountedScalarOwnerShardSource)
	for _, shards := range []int{1, 2, 8} {
		t.Run(fmt.Sprintf("shards-%d", shards), func(t *testing.T) {
			env := envWithStdlib(repoRoot(t))
			env = overrideEnvVar(env, "SURGE_SHARDS", fmt.Sprintf("%d", shards))
			env = overrideEnvVar(env, "SURGE_BLOCKING_THREADS", "1")
			for i := 0; i < 10; i++ {
				dur, res := runBinaryWithTimeout(t, bin, env, 30*time.Second)
				if res.exitCode != 0 || res.stdout != "" || res.stderr != "" {
					t.Fatalf("run %d failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
						i+1, res.exitCode, dur, res.stdout, res.stderr)
				}
			}
		})
	}
}
