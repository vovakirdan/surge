package vm_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func buildLLVMProgramFromSource(t *testing.T, source string) (string, *testArtifacts) {
	t.Helper()
	ensureLLVMToolchain(t)
	root := repoRoot(t)
	artifacts := newTestArtifacts(t, root)
	srcPath := filepath.Join(artifacts.Dir, sanitizeTestName(t.Name())+".sg")
	if err := os.WriteFile(srcPath, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	surge := buildSurgeBinary(t, root)
	buildArgs := []string{"build", srcPath, "--emit-mir", "--emit-llvm", "--keep-tmp", "--print-commands"}
	buildOut, buildErr, buildCode := runSurgeWithInput(t, root, surge, "", buildArgs...)
	writeArtifact(t, artifacts.Dir, "build.stdout", buildOut)
	writeArtifact(t, artifacts.Dir, "build.stderr", buildErr)
	writeArtifact(t, artifacts.Dir, "build.exit_code", fmt.Sprintf("%d\n", buildCode))
	outputPath := llvmOutputPath(root, srcPath)
	artifacts.Repro = llvmReproCommand(root, srcPath, outputPath, nil)
	writeArtifact(t, artifacts.Dir, "repro.txt", artifacts.Repro+"\n")
	if buildCode != 0 {
		t.Fatalf("LLVM build failed (exit=%d). See %s", buildCode, artifacts.Dir)
	}
	return outputPath, artifacts
}

func overrideEnv(base []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, prefix+value)
	return out
}

func runBinaryWithTimeout(t *testing.T, outputPath string, env []string, args []string, timeout time.Duration) (time.Duration, runResult) {
	t.Helper()
	root := repoRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, outputPath, args...)
	cmd.Dir = root
	cmd.Env = env
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start)
	stdout := outBuf.String()
	stderr := errBuf.String()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("program timeout after %s\nstdout:\n%s\nstderr:\n%s", timeout, stdout, stderr)
	}
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("run %s: %v\nstderr:\n%s", outputPath, err, stderr)
		}
	}
	return dur, runResult{stdout: stdout, stderr: stderr, exitCode: exitCode}
}

func TestMTParallelism(t *testing.T) {
	ensureLLVMToolchain(t)
	if runtime.NumCPU() < 2 {
		t.Skip("parallelism test needs >=2 CPUs")
	}

	source := `async fn spin(n: int) -> int {
    let mut i: int = 0;
    let mut acc: int = 0;
    while (i < n) {
        acc = acc + i;
        i = i + 1;
    }
    return acc;
}

@entrypoint("argv")
fn main(iters: int) -> int {
    let t1 = spawn spin(iters);
    let t2 = spawn spin(iters);
    let r1 = t1.await();
    let r2 = t2.await();
    let mut total: int = 0;
    let cancelled1: bool = compare r1 {
        Success(v) => {
            total = total + v;
            false;
        }
        Cancelled() => true;
    };
    let cancelled2: bool = compare r2 {
        Success(v) => {
            total = total + v;
            false;
        }
        Cancelled() => true;
    };
    if cancelled1 || cancelled2 {
        return 1;
    }
    print(total to string);
    return 0;
}
`

	outputPath, _ := buildLLVMProgramFromSource(t, source)
	baseEnv := envWithStdlib(repoRoot(t))

	run := func(iters int, threads int) time.Duration {
		env := overrideEnv(baseEnv, "SURGE_THREADS", strconv.Itoa(threads))
		args := []string{strconv.Itoa(iters)}
		dur, res := runBinaryWithTimeout(t, outputPath, env, args, 15*time.Second)
		if res.exitCode != 0 {
			t.Fatalf("run failed (threads=%d iters=%d exit=%d)\nstdout:\n%s\nstderr:\n%s",
				threads, iters, res.exitCode, res.stdout, res.stderr)
		}
		return dur
	}

	iters := 5_000_000
	maxIters := 200_000_000
	dur := run(iters, 1)
	for dur < 200*time.Millisecond && iters < maxIters {
		iters *= 2
		dur = run(iters, 1)
	}
	if dur < 200*time.Millisecond {
		t.Skipf("single-thread runtime too short for timing (%s)", dur)
	}

	parallelDur := run(iters, 2)
	if parallelDur >= dur-(dur/10) {
		t.Fatalf("expected parallel speedup (iters=%d): single=%s parallel=%s", iters, dur, parallelDur)
	}
}

func TestMTWakeupsAndCancellation(t *testing.T) {
	ensureLLVMToolchain(t)

	// NOTE: Channel stress under MT showed nondeterministic hangs; keep this test on checkpoint/join wakeups for now.
	source := `async fn step(id: int) -> int {
    checkpoint().await();
    return id;
}

async fn slow_loop() -> int {
    let mut i: int = 0;
    while (i < 100) {
        checkpoint().await();
        i = i + 1;
    }
    return i;
}

@entrypoint
fn main() -> int {
    let count: int = 200;
    let mut tasks: Task<int>[] = Array::<Task<int>>::with_len(count to uint);
    let mut i: int = 0;
    while (i < count) {
        tasks[i] = spawn step(i);
        i = i + 1;
    }
    let mut sum: int = 0;
    let mut cancelled: bool = false;
    for task in tasks {
        let r = task.await();
        let was_cancelled: bool = compare r {
            Success(v) => {
                sum = sum + v;
                false;
            }
            Cancelled() => true;
        };
        if was_cancelled {
            cancelled = true;
        }
    }
    if cancelled {
        return 1;
    }
    let expected: int = (count - 1) * count / 2;
    if sum != expected {
        return 1;
    }

    let t = spawn slow_loop();
    t.cancel();
    let r = t.await();
    let cancel_ok: bool = compare r {
        Success(_) => false;
        Cancelled() => true;
    };
    if !cancel_ok {
        return 2;
    }

    print("ok");
    return 0;
}
`

	outputPath, _ := buildLLVMProgramFromSource(t, source)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnv(baseEnv, "SURGE_THREADS", "2")
	dur, res := runBinaryWithTimeout(t, outputPath, env, nil, 10*time.Second)
	if res.exitCode != 0 {
		t.Fatalf("run failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
			res.exitCode, dur, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "ok") {
		t.Fatalf("unexpected stdout: %q", res.stdout)
	}
}
