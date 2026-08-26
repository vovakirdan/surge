package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// RV2-DEBT-261, the program-level row. Two @failfast blocks, each with a child
// that is cancelled and awaited, each expected to resolve Cancelled -- the
// shapes that gave TestMTStructuredConcurrency its exit 12 and exit 13 under
// pinned CPUs. The block resolves Success when rt_scope_join_all's verify
// answers "drained" with a fail-fast flag read before the cancelled child's
// completion landed (rt_async_scope.c), and that window is a few instructions
// wide against a completion that is itself waiting on the control lane, so a
// single green run says nothing: this row is meant to be run many times, on
// both lanes, at one worker and at four. Exit 12 names the first block, 13 the
// second, 99 the driver itself; the eight rounds widen the chance per run.
//
// The first block also tolerates the fast child winning the race against its
// own cancel (it returns 10 from inside the block): the early return cancels
// the slow sibling through rt_scope_cancel_all, and the block must still
// resolve Cancelled -- which is exactly the interleaving exit 12 came from.
const runtimeV2FailfastJoinSource = `async fn spin(count: int) -> int {
    let mut i = 0;
    while i < count {
        checkpoint().await();
        i = i + 1;
    }
    return count;
}

async fn main_async() -> int {
    let mut round = 0;
    while round < 8 {
        let ff = (@failfast async {
            let slow = spawn async {
                let _ = spin(200).await();
                return 1;
            };
            let fast = spawn async {
                checkpoint().await();
                return 2;
            };
            fast.cancel();
            let r_fast = fast.await();
            let fast_cancelled = compare r_fast {
                Cancelled() => true;
                Success(_) => false;
            };
            if !fast_cancelled {
                return 10;
            }
            let r_slow = slow.await();
            let slow_cancelled = compare r_slow {
                Cancelled() => true;
                Success(_) => false;
            };
            if !slow_cancelled {
                return 11;
            }
            return 0;
        }).await();
        let ff_ok = compare ff {
            Cancelled() => true;
            Success(_) => false;
        };
        if !ff_ok {
            return 12;
        }

        let ff2 = (@failfast async {
            let a = spawn async {
                let _ = spin(50).await();
                return 1;
            };
            let b = spawn async {
                let _ = spin(50).await();
                return 2;
            };
            a.cancel();
            b.cancel();
            let _ = a.await();
            let _ = b.await();
            return 0;
        }).await();
        let ff2_ok = compare ff2 {
            Cancelled() => true;
            Success(_) => false;
        };
        if !ff2_ok {
            return 13;
        }
        round = round + 1;
    }
    print("failfast-join-ok");
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

func TestRuntimeV2FailfastJoinAnswersCancelled(t *testing.T) {
	t.Run("llvm", func(t *testing.T) {
		outputPath := buildLLVMProgramFromSource(t, runtimeV2FailfastJoinSource)
		baseEnv := envWithStdlib(repoRoot(t))
		for _, threads := range []string{"1", "4"} {
			t.Run("threads-"+threads, func(t *testing.T) {
				env := overrideEnv(baseEnv, threads)
				dur, res := runBinaryWithTimeout(t, outputPath, env, 60*time.Second)
				assertFailfastJoinRun(t, "llvm", threads, res.exitCode, dur, res.stdout, res.stderr)
			})
		}
	})
	t.Run("vm", func(t *testing.T) {
		root := repoRoot(t)
		surge := buildSurgeBinary(t, root)
		srcPath := filepath.Join(t.TempDir(), "failfast_join.sg")
		if err := os.WriteFile(srcPath, []byte(runtimeV2FailfastJoinSource), 0o600); err != nil {
			t.Fatalf("write source: %v", err)
		}
		baseEnv := envWithStdlib(root)
		for _, threads := range []string{"1", "4"} {
			t.Run("threads-"+threads, func(t *testing.T) {
				env := overrideEnv(baseEnv, threads)
				start := time.Now()
				stdout, stderr, code := runSurgeWithEnv(t, root, surge, env, "run", "--backend=vm", srcPath)
				assertFailfastJoinRun(t, "vm", threads, code, time.Since(start), stdout, stderr)
			})
		}
	})
}

func assertFailfastJoinRun(t *testing.T, lane, threads string, exitCode int, dur time.Duration, stdout, stderr string) {
	t.Helper()
	reasons := map[int]string{
		12: "the first @failfast block (fast cancelled, slow cancelled by fail-fast) resolved Success",
		13: "the second @failfast block (both children cancelled before they ran) resolved Success",
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
	if !strings.Contains(stdout, "failfast-join-ok") {
		t.Fatalf("%s SURGE_THREADS=%s: missing completion marker\nstdout:\n%s\nstderr:\n%s",
			lane, threads, stdout, stderr)
	}
}
