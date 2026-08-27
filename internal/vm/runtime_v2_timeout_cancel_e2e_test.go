package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// RV2-DEBT-263, the `timeout` row. When the timer wins, timeout(task, ms)
// cancels the target and answers Cancelled (rt_async_select.c) -- but the
// target could still commit Success, and a SECOND handle then read that Success
// out of the very task the runtime had just told its first asker was cancelled.
// Two answers for one task, out of one cancel.
//
// The shape is the one RV2-DEBT-261's analysis named. The target's body awaits
// `spin`, a chain of checkpoints; cancelling the target cancels `spin` through
// children[] and `spin` completes Cancelled. If `spin` reaches DONE before the
// target is re-polled -- which it usually does, because the target is woken by
// that completion -- the target reads its child from the DONE fast path, which
// answers from the TARGET and never consults the awaiter's own cancelled flag
// (rt_task_poll). `let _ =` discards it, `ret 1` follows with no suspension
// left to observe the cancel at, and rt_async_return published Success.
//
// The row counts the rounds where the timer actually won and refuses to pass on
// zero of them: a run where the target beat every deadline would otherwise be
// green without ever asking the question. Exit 20 is the defect, 21 says the
// row proved nothing, 99 that main_async itself was cancelled.
//
// Measured on this lane at 24 rounds, before and after the fix (SURGE_STDLIB
// set, `surge build --backend=llvm`): unfixed, exit 20 in 9 of 10 runs -- 5 of
// 5 at SURGE_THREADS=1, 4 of 5 at 4; fixed, exit 0 with "timeout-cancel-ok" in
// 10 of 10. The VM lane is green either way and cannot witness this defect: its
// executor is single-threaded and deterministic, and in this program the target
// is always re-polled while its child is still live, so the suspension carries
// the cancellation as it is meant to. The VM's own commit boundary is pinned by
// TestMarkDoneCommitsCancelledForACancelledTask (internal/asyncrt); the VM row
// here is an acceptance row -- it must STAY green -- not the witness.
const runtimeV2TimeoutCancelSource = `async fn spin(count: int) -> int {
    let mut i = 0;
    while i < count {
        checkpoint().await();
        i = i + 1;
    }
    return count;
}

async fn main_async() -> int {
    let mut round = 0;
    let mut timed = 0;
    while round < 24 {
        let t = spawn async {
            let _ = spin(4000).await();
            ret 1;
        };
        let r = timeout(t.clone(), 5:uint);
        let timed_out = compare r {
            Cancelled() => true;
            Success(_) => false;
        };
        let after = t.await();
        let after_cancelled = compare after {
            Cancelled() => true;
            Success(_) => false;
        };
        if timed_out {
            timed = timed + 1;
            if !after_cancelled {
                return 20;
            }
        }
        round = round + 1;
    }
    if timed == 0 {
        return 21;
    }
    print("timeout-cancel-ok");
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

func TestRuntimeV2TimeoutTargetAnswersCancelledToEveryHandle(t *testing.T) {
	t.Run("llvm", func(t *testing.T) {
		outputPath := buildLLVMProgramFromSource(t, runtimeV2TimeoutCancelSource)
		baseEnv := envWithStdlib(repoRoot(t))
		for _, threads := range []string{"1", "4"} {
			t.Run("threads-"+threads, func(t *testing.T) {
				env := overrideEnv(baseEnv, threads)
				dur, res := runBinaryWithTimeout(t, outputPath, env, 60*time.Second)
				assertTimeoutCancelRun(t, "llvm", threads, res.exitCode, dur, res.stdout, res.stderr)
			})
		}
	})
	t.Run("vm", func(t *testing.T) {
		root := repoRoot(t)
		surge := buildSurgeBinary(t, root)
		srcPath := filepath.Join(t.TempDir(), "timeout_cancel.sg")
		if err := os.WriteFile(srcPath, []byte(runtimeV2TimeoutCancelSource), 0o600); err != nil {
			t.Fatalf("write source: %v", err)
		}
		baseEnv := envWithStdlib(root)
		for _, threads := range []string{"1", "4"} {
			t.Run("threads-"+threads, func(t *testing.T) {
				env := overrideEnv(baseEnv, threads)
				start := time.Now()
				stdout, stderr, code := runSurgeWithEnv(t, root, surge, env, "run", "--backend=vm", srcPath)
				assertTimeoutCancelRun(t, "vm", threads, code, time.Since(start), stdout, stderr)
			})
		}
	})
}

func assertTimeoutCancelRun(t *testing.T, lane, threads string, exitCode int, dur time.Duration, stdout, stderr string) {
	t.Helper()
	reasons := map[int]string{
		20: "timeout answered Cancelled and cancelled the target, and a second handle still read Success out of it",
		21: "the timer never won a single round, so the row asked nothing",
		99: "main_async itself resolved Cancelled",
	}
	if exitCode != 0 {
		reason := reasons[exitCode]
		if reason == "" {
			reason = "the program did not run to its verdict"
		}
		t.Fatalf("%s SURGE_THREADS=%s: exit=%d -- %s (dur=%s)\nstdout:\n%s\nstderr:\n%s",
			lane, threads, exitCode, reason, dur, stdout, stderr)
	}
	if !strings.Contains(stdout, "timeout-cancel-ok") {
		t.Fatalf("%s SURGE_THREADS=%s: missing completion marker\nstdout:\n%s\nstderr:\n%s",
			lane, threads, stdout, stderr)
	}
}
