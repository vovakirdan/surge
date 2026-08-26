package vm_test

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// Moved out of mt_executor_test.go unchanged: that file sits over the legacy
// size ceiling and may not grow, and the pop-drain rewrites of 2026-08-26
// grew it by eight effective lines.
func TestMTBufferedBlockingRecvRefillWakesSender(t *testing.T) {
	requireLLVMBackend(t)
	ensureLLVMToolchain(t)
	if runtime.NumCPU() < 2 {
		t.Skip("buffered blocking recv refill test needs >=2 CPUs")
	}
	t.Parallel()

	source := `async fn sender(ch: own Channel<int>) -> int {
    ch.send(2);
    return 7;
}

@entrypoint
fn main() -> int {
    if rt_worker_count() <= 1:uint {
        return 90;
    }
    let ch = Channel::<int>::new(1:uint);
    ch.send(1);
    let send_ch = ch;
    let task = spawn sender(send_ch);
    sleep(10:uint).await();

    let first = ch.recv();
    let first_ok = compare first {
        Some(v) => v == 1;
        nothing => false;
    };
    if !first_ok {
        return 1;
    }

    let send_res = task.await();
    let send_ok = compare send_res {
        Success(v) => v == 7;
        Cancelled() => false;
    };
    if !send_ok {
        return 2;
    }
    print("ok");
    return 0;
}
`

	outputPath := buildLLVMProgramFromSource(t, source)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnv(baseEnv, "2")
	dur, res := runBinaryWithTimeout(t, outputPath, env, 10*time.Second)
	if res.exitCode != 0 {
		t.Fatalf("run failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
			res.exitCode, dur, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "ok") {
		t.Fatalf("unexpected stdout: %q", res.stdout)
	}
}
