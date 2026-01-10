package vm_test

import (
	"bytes"
	"context"
	"errors"
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

func buildLLVMProgramFromSource(t *testing.T, source string) string {
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
	return outputPath
}

func overrideEnv(base []string, value string) []string {
	const key = "SURGE_THREADS"
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

func overrideEnvVar(base []string, key, value string) []string {
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

type schedTrace struct {
	mode   string
	seed   uint64
	local  uint64
	inject uint64
	steal  uint64
	events uint64
	hash   uint64
}

func parseSchedTrace(t *testing.T, stderr string) schedTrace {
	t.Helper()
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.HasPrefix(line, "SCHED_TRACE") {
			continue
		}
		fields := strings.Fields(line)
		out := schedTrace{}
		for _, field := range fields[1:] {
			kv := strings.SplitN(field, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "mode":
				out.mode = kv[1]
			case "seed":
				v, err := strconv.ParseUint(kv[1], 10, 64)
				if err != nil {
					t.Fatalf("parse seed: %v", err)
				}
				out.seed = v
			case "local":
				v, err := strconv.ParseUint(kv[1], 10, 64)
				if err != nil {
					t.Fatalf("parse local: %v", err)
				}
				out.local = v
			case "inject":
				v, err := strconv.ParseUint(kv[1], 10, 64)
				if err != nil {
					t.Fatalf("parse inject: %v", err)
				}
				out.inject = v
			case "steal":
				v, err := strconv.ParseUint(kv[1], 10, 64)
				if err != nil {
					t.Fatalf("parse steal: %v", err)
				}
				out.steal = v
			case "events":
				v, err := strconv.ParseUint(kv[1], 10, 64)
				if err != nil {
					t.Fatalf("parse events: %v", err)
				}
				out.events = v
			case "hash":
				v, err := strconv.ParseUint(kv[1], 10, 64)
				if err != nil {
					t.Fatalf("parse hash: %v", err)
				}
				out.hash = v
			}
		}
		return out
	}
	t.Fatalf("missing SCHED_TRACE in stderr")
	return schedTrace{}
}

func runBinaryWithTimeout(t *testing.T, outputPath string, env []string, timeout time.Duration) (time.Duration, runResult) {
	t.Helper()
	root := repoRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, outputPath)
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
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
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
	t.Parallel()

	source := `async fn spin(progress: own Channel<nothing>, n: int) -> int {
    let mut i: int = 0;
    let mut sent: bool = false;
    while i < n {
        sent = progress.try_send(nothing);
        while !sent {
            sent = progress.try_send(nothing);
        }
        i = i + 1;
    }
    return 0;
}

async fn sink(progress: own Channel<nothing>, target: int) -> int {
    let mut seen: int = 0;
    while seen < target {
        let v = progress.recv();
        let ok = compare v {
            Some(_) => true;
            nothing => false;
        };
        if !ok {
            return 2;
        }
        seen = seen + 1;
    }
    return 0;
}

async fn run() -> int {
    let workers: uint = rt_worker_count();
    if workers <= 1:uint {
        return 3;
    }
    let mut spinners_u: uint = workers - 1:uint;
    if spinners_u > 4:uint {
        spinners_u = 4:uint;
    }
    let spinners: int = spinners_u to int;
    let iters: int = 1000;
    let target: int = spinners * iters;
    let progress = make_channel::<nothing>(0);

    let sink_task = spawn sink(progress, target);
    checkpoint().await();

    let mut tasks: Task<int>[] = Array::<Task<int>>::with_len(spinners to uint);
    let mut i: int = 0;
    while i < spinners {
        tasks[i] = spawn spin(progress, iters);
        i = i + 1;
    }

    let mut failed: bool = false;
    for t in tasks {
        let r = t.await();
        let ok = compare r {
            Success(v) => v == 0;
            Cancelled() => false;
        };
        if !ok {
            failed = true;
        }
    }
    let sink_res = sink_task.await();
    let sink_ok = compare sink_res {
        Success(v) => v == 0;
        Cancelled() => false;
    };
    if failed || !sink_ok {
        return 4;
    }
    print("ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let res = run().await();
    return compare res {
        Success(v) => v;
        Cancelled() => 1;
    };
}
`

	outputPath := buildLLVMProgramFromSource(t, source)
	baseEnv := envWithStdlib(repoRoot(t))

	env := overrideEnv(baseEnv, "2")
	_, res := runBinaryWithTimeout(t, outputPath, env, 10*time.Second)
	if res.exitCode != 0 {
		t.Fatalf("run failed (exit=%d)\nstdout:\n%s\nstderr:\n%s",
			res.exitCode, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "ok") {
		t.Fatalf("unexpected stdout: %q", res.stdout)
	}
}

func TestMTWakeupsAndCancellation(t *testing.T) {
	ensureLLVMToolchain(t)
	t.Parallel()

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

func TestMTChannelParkUnpark(t *testing.T) {
	ensureLLVMToolchain(t)
	t.Parallel()

	source := `async fn producer(ch: own Channel<int>, count: int, base: int) -> int {
    let mut i = 0;
    while i < count {
        ch.send(base + i);
        i = i + 1;
    }
    return count;
}

async fn consumer(ch: own Channel<int>) -> int {
    let mut received = 0;
    let mut done = false;
    while !done {
        let v = ch.recv();
        let got = compare v {
            Some(_) => 1;
            nothing => 0;
        };
        if got == 1 {
            received = received + 1;
        } else {
            done = true;
        }
    }
    return received;
}

async fn wait_recv(ch: own Channel<int>) -> int {
    let v = ch.recv();
    let ok = compare v {
        Some(_) => true;
        nothing => false;
    };
    if ok {
        return 1;
    }
    return 0;
}

async fn wait_send(ch: own Channel<int>) -> int {
    ch.send(1);
    return 0;
}

async fn ping(out: own Channel<int>, inp: own Channel<int>, count: int) -> int {
    let mut i = 0;
    while i < count {
        out.send(i);
        let v = inp.recv();
        let ok = compare v {
            Some(_) => true;
            nothing => false;
        };
        if !ok {
            return 1;
        }
        i = i + 1;
    }
    return 0;
}

async fn pong(out: own Channel<int>, inp: own Channel<int>, count: int) -> int {
    let mut i = 0;
    while i < count {
        let v = inp.recv();
        let ok = compare v {
            Some(_) => true;
            nothing => false;
        };
        if !ok {
            return 1;
        }
        out.send(i);
        i = i + 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let producers = 4;
    let consumers = 4;
    let per = 2000;
    let total = producers * per;
    let ch = make_channel::<int>(0);

    let mut prod_tasks: Task<int>[] = Array::<Task<int>>::with_len(producers to uint);
    let mut cons_tasks: Task<int>[] = Array::<Task<int>>::with_len(consumers to uint);

    let mut i = 0;
    while i < producers {
        let c = ch;
        prod_tasks[i] = spawn producer(c, per, i * per);
        i = i + 1;
    }

    let mut j = 0;
    while j < consumers {
        let c = ch;
        cons_tasks[j] = spawn consumer(c);
        j = j + 1;
    }

    let mut produced = 0;
    let mut prod_cancelled = false;
    for task in prod_tasks {
        let r = task.await();
        let was_cancelled = compare r {
            Success(v) => {
                produced = produced + v;
                false;
            }
            Cancelled() => true;
        };
        if was_cancelled {
            prod_cancelled = true;
        }
    }
    if prod_cancelled {
        return 1;
    }
    if produced != total {
        return 2;
    }

    ch.close();

    let mut received = 0;
    let mut recv_cancelled = false;
    for task in cons_tasks {
        let r = task.await();
        let was_cancelled = compare r {
            Success(v) => {
                received = received + v;
                false;
            }
            Cancelled() => true;
        };
        if was_cancelled {
            recv_cancelled = true;
        }
    }
    if recv_cancelled {
        return 3;
    }
    if received != total {
        return 4;
    }

    let recv_ch = make_channel::<int>(0);
    let recv_task = spawn wait_recv(recv_ch);
    checkpoint().await();
    checkpoint().await();
    recv_task.cancel();
    let recv_res = recv_task.await();
    let recv_cancel_ok = compare recv_res {
        Success(_) => false;
        Cancelled() => true;
    };
    if !recv_cancel_ok {
        return 5;
    }

    let send_ch = make_channel::<int>(0);
    let send_task = spawn wait_send(send_ch);
    checkpoint().await();
    checkpoint().await();
    send_task.cancel();
    let send_res = send_task.await();
    let send_cancel_ok = compare send_res {
        Success(_) => false;
        Cancelled() => true;
    };
    if !send_cancel_ok {
        return 6;
    }

    let rounds = 20;
    let iter = 2000;
    let mut round = 0;
    while round < rounds {
        let ping_ch = make_channel::<int>(0);
        let pong_ch = make_channel::<int>(0);
        let ping_out = ping_ch;
        let ping_in = pong_ch;
        let pong_out = pong_ch;
        let pong_in = ping_ch;
        let ping_task = spawn ping(ping_out, ping_in, iter);
        let pong_task = spawn pong(pong_out, pong_in, iter);
        let ping_res = ping_task.await();
        let pong_res = pong_task.await();
        let ping_ok = compare ping_res {
            Success(v) => v == 0;
            Cancelled() => false;
        };
        let pong_ok = compare pong_res {
            Success(v) => v == 0;
            Cancelled() => false;
        };
        if !ping_ok || !pong_ok {
            return 7;
        }
        round = round + 1;
    }

    print("ok");
    return 0;
}
`

	outputPath := buildLLVMProgramFromSource(t, source)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnv(baseEnv, "2")
	dur, res := runBinaryWithTimeout(t, outputPath, env, 20*time.Second)
	if res.exitCode != 0 {
		t.Fatalf("run failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
			res.exitCode, dur, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "ok") {
		t.Fatalf("unexpected stdout: %q", res.stdout)
	}
}

func TestMTWorkStealing(t *testing.T) {
	ensureLLVMToolchain(t)
	threads := mtThreadCount(t)
	t.Parallel()

	source := `async fn worker(steps: int) -> int {
    let mut i: int = 0;
    while i < steps {
        checkpoint().await();
        i = i + 1;
    }
    return 0;
}

async fn spawn_many(count: int, steps: int) -> int {
    let mut tasks: Task<int>[] = Array::<Task<int>>::with_len(count to uint);
    let mut i: int = 0;
    while i < count {
        tasks[i] = spawn worker(steps);
        i = i + 1;
    }
    let mut failed: bool = false;
    for t in tasks {
        let r = t.await();
        let ok = compare r {
            Success(v) => v == 0;
            Cancelled() => false;
        };
        if !ok {
            failed = true;
        }
    }
    if failed {
        return 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    if rt_worker_count() <= 1:uint {
        return 2;
    }
    let workers: int = rt_worker_count() to int;
    let count: int = workers * 8;
    let steps: int = 80;
    let res = spawn_many(count, steps).await();
    let ok = compare res {
        Success(v) => v == 0;
        Cancelled() => false;
    };
    if !ok {
        return 1;
    }
    print("ok");
    return 0;
}
`

	outputPath := buildLLVMProgramFromSource(t, source)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnv(baseEnv, strconv.Itoa(threads))
	env = overrideEnvVar(env, "SURGE_SCHED_TRACE", "1")
	dur, res := runBinaryWithTimeout(t, outputPath, env, 20*time.Second)
	if res.exitCode != 0 {
		t.Fatalf("run failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
			res.exitCode, dur, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "ok") {
		t.Fatalf("unexpected stdout: %q", res.stdout)
	}
	trace := parseSchedTrace(t, res.stderr)
	if trace.steal == 0 {
		t.Fatalf("expected steals > 0 (trace=%+v)\nstderr:\n%s", trace, res.stderr)
	}
}

func TestMTSeededScheduler(t *testing.T) {
	ensureLLVMToolchain(t)
	threads := mtThreadCount(t)
	t.Parallel()

	source := `async fn leaf(id: int) -> int {
    let mut sum: int = 0;
    let mut i: int = 0;
    while i < 400 {
        sum = sum + i;
        i = i + 1;
    }
    return sum + id;
}

@entrypoint
fn main() -> int {
    if rt_worker_count() <= 1:uint {
        return 2;
    }
    let count: int = 32;
    let mut tasks: Task<int>[] = Array::<Task<int>>::with_len(count to uint);
    let mut i: int = 0;
    while i < count {
        tasks[i] = spawn leaf(i);
        i = i + 1;
    }
    let mut cancelled: bool = false;
    let mut total: int = 0;
    for t in tasks {
        let r = t.await();
        let ok = compare r {
            Success(v) => {
                total = total + v;
                true;
            }
            Cancelled() => false;
        };
        if !ok {
            cancelled = true;
        }
    }
    if cancelled {
        return 3;
    }
    print("ok");
    return 0;
}
`

	outputPath := buildLLVMProgramFromSource(t, source)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnv(baseEnv, strconv.Itoa(threads))
	env = overrideEnvVar(env, "SURGE_SCHED", "seeded")
	env = overrideEnvVar(env, "SURGE_SCHED_SEED", "424242")
	env = overrideEnvVar(env, "SURGE_SCHED_TRACE", "1")

	dur1, res1 := runBinaryWithTimeout(t, outputPath, env, 10*time.Second)
	if res1.exitCode != 0 {
		t.Fatalf("run 1 failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
			res1.exitCode, dur1, res1.stdout, res1.stderr)
	}
	if !strings.Contains(res1.stdout, "ok") {
		t.Fatalf("unexpected stdout (run 1): %q", res1.stdout)
	}
	trace1 := parseSchedTrace(t, res1.stderr)
	if trace1.mode != "seeded" || trace1.seed != 424242 {
		t.Fatalf("unexpected trace mode/seed: %+v\nstderr:\n%s", trace1, res1.stderr)
	}

	dur2, res2 := runBinaryWithTimeout(t, outputPath, env, 10*time.Second)
	if res2.exitCode != 0 {
		t.Fatalf("run 2 failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
			res2.exitCode, dur2, res2.stdout, res2.stderr)
	}
	if !strings.Contains(res2.stdout, "ok") {
		t.Fatalf("unexpected stdout (run 2): %q", res2.stdout)
	}
	trace2 := parseSchedTrace(t, res2.stderr)
	if trace1.hash != trace2.hash || trace1.events != trace2.events {
		t.Fatalf("seeded trace mismatch:\ntrace1=%+v\ntrace2=%+v", trace1, trace2)
	}
}
