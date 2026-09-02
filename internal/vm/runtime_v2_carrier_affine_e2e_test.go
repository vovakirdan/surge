package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// A compiled program's half of carrier affinity (RV2 D4.6, D4.8's "borrow
// spawn from @entrypoint"): a child that takes a reference to the parent's
// local is created through __task_create_affine, pinned to the parent's
// carrier before it is published, and the run reports the pin. The child
// reads the borrowed place and the parent joins it on every path, which is
// the shape sema admits (task_borrow_pin.go).
//
// SURGE_SHARDS=1 is the only topology with several carriers on one shard,
// so it is the only one where the pin is more than a note; at eight workers
// the child's publication goes to one of eight deques and its credit to one
// of eight tokens, and the answer still comes back.
const runtimeV2CarrierAffineBorrowSource = `async fn plus_one(x: &int) -> int {
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

func TestRuntimeV2CarrierAffineBorrowSpawnIsPinnedAndAnswers(t *testing.T) {
	ensureLLVMToolchain(t)
	skipTimeoutTests(t)
	outputPath := buildLLVMProgramFromSource(t, runtimeV2CarrierAffineBorrowSource)
	for _, threads := range []int{1, 8} {
		t.Run(fmt.Sprintf("threads-%d", threads), func(t *testing.T) {
			env := envWithStdlib(repoRoot(t))
			env = overrideEnvVar(env, "SURGE_SHARDS", "1")
			env = overrideEnvVar(env, "SURGE_THREADS", fmt.Sprintf("%d", threads))
			env = overrideEnvVar(env, "SURGE_BLOCKING_THREADS", "1")
			env = overrideEnvVar(env, "SURGE_SCHED_TRACE", "1")
			dur, res := runBinaryWithTimeout(t, outputPath, env, 20*time.Second)
			if res.exitCode != 0 {
				t.Fatalf("borrow spawn failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
					res.exitCode, dur, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stdout, "ok") {
				t.Fatalf("unexpected stdout: %q", res.stdout)
			}
			trace := parseSchedTrace(t, res.stderr)
			// The one borrowing child was pinned at creation; nothing pinned
			// was left for an exiting carrier to cancel. At one thread the
			// runtime runs its tasks on the control lane's single runner,
			// which is no worker and has no worker identity to pin to; the
			// child's only eligible carrier is then the only runner there is,
			// and no pin is recorded. The pin is a fact only where a second
			// carrier could take the task, which is where it is asserted.
			if threads > 1 && trace.carrierPinned < 1 {
				t.Fatalf("carrier_pinned=%d, want >= 1: the borrowing child was not pinned\n%s",
					trace.carrierPinned, res.stderr)
			}
			if trace.carrierShutdownCancelled != 0 {
				t.Fatalf("carrier_shutdown_cancelled=%d, want 0\n%s", trace.carrierShutdownCancelled, res.stderr)
			}
		})
	}
}
