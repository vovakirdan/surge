package vm_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func mtThreadCount(t *testing.T) int {
	t.Helper()
	threads := runtime.NumCPU()
	if threads > 4 {
		threads = 4
	}
	if threads < 2 {
		t.Skip("MT correctness tests require >=2 CPUs")
	}
	return threads
}

func mtEnv(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	threads := mtThreadCount(t)
	baseEnv := envWithStdlib(root)
	return overrideEnv(baseEnv, strconv.Itoa(threads))
}

func runMTSource(t *testing.T, source string, timeout time.Duration) {
	t.Helper()
	outputPath := buildLLVMProgramFromSource(t, source)
	env := mtEnv(t)
	dur, res := runBinaryWithTimeout(t, outputPath, env, timeout)
	if res.exitCode != 0 {
		t.Fatalf("run failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
			res.exitCode, dur, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "ok") {
		t.Fatalf("unexpected stdout: %q", res.stdout)
	}
}

func TestMTCorrectnessWakeups(t *testing.T) {
	ensureLLVMToolchain(t)
	t.Parallel()

	source := `async fn stress_sender(id: int, rounds: int, gate: own Channel<int>) -> int {
    let mut i = 0;
    while i < rounds {
        checkpoint().await();
        gate.send(id);
        i = i + 1;
    }
    return id;
}

async fn drain(gate: own Channel<int>, total: int) -> int {
    let mut seen = 0;
    while seen < total {
        let v = gate.recv();
        let ok = compare v {
            Some(_) => true;
            nothing => false;
        };
        if !ok {
            return 1;
        }
        seen = seen + 1;
    }
    return 0;
}

async fn recv_once(ch: own Channel<int>) -> int {
    let v = ch.recv();
    let ok = compare v {
        Some(_) => true;
        nothing => false;
    };
    if !ok {
        return 1;
    }
    return 0;
}

async fn send_once(ch: own Channel<int>, value: int) -> int {
    ch.send(value);
    return 0;
}

async fn send_after(ch: own Channel<int>, value: int) -> int {
    checkpoint().await();
    checkpoint().await();
    ch.send(value);
    return 0;
}

async fn main_async() -> int {
    if rt_worker_count() <= 1:uint {
        return 90;
    }

    let workers = 8;
    let rounds = 500;
    let total = workers * rounds;
    let gate = make_channel::<int>(0);

    let sink_task = spawn drain(gate, total);
    let mut tasks: Task<int>[] = Array::<Task<int>>::with_len(workers to uint);
    let mut i = 0;
    while i < workers {
        let c = gate;
        tasks[i] = spawn stress_sender(i, rounds, c);
        i = i + 1;
    }

    let mut cancelled = false;
    for t in tasks {
        let r = t.await();
        let ok = compare r {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            cancelled = true;
        }
    }
    if cancelled {
        return 1;
    }
    let sink_res = sink_task.await();
    let sink_ok = compare sink_res {
        Success(v) => v == 0;
        Cancelled() => false;
    };
    if !sink_ok {
        return 2;
    }

    let rounds_race = 500;
    let mut j = 0;
    while j < rounds_race {
        let ch = make_channel::<int>(0);
        let recv_task = spawn recv_once(ch);
        let send_task = spawn send_once(ch, j);
        let r1 = recv_task.await();
        let r2 = send_task.await();
        let ok1 = compare r1 {
            Success(v) => v == 0;
            Cancelled() => false;
        };
        let ok2 = compare r2 {
            Success(v) => v == 0;
            Cancelled() => false;
        };
        if !ok1 || !ok2 {
            return 4;
        }
        j = j + 1;
    }

    let mut k = 0;
    while k < rounds_race {
        let ch = make_channel::<int>(0);
        let recv_task = spawn recv_once(ch);
        let send_task = spawn send_after(ch, k);
        let r1 = recv_task.await();
        let r2 = send_task.await();
        let ok1 = compare r1 {
            Success(v) => v == 0;
            Cancelled() => false;
        };
        let ok2 = compare r2 {
            Success(v) => v == 0;
            Cancelled() => false;
        };
        if !ok1 || !ok2 {
            return 5;
        }
        k = k + 1;
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

	outputPath := buildLLVMProgramFromSource(t, source)
	env := mtEnv(t)
	env = overrideEnvVar(env, "SURGE_BLOCKING_THREADS", "1")
	dur, res := runBinaryWithTimeout(t, outputPath, env, 20*time.Second)
	if res.exitCode != 0 {
		t.Fatalf("run failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
			res.exitCode, dur, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "ok") {
		t.Fatalf("unexpected stdout: %q", res.stdout)
	}
}

func TestMTCorrectnessChannels(t *testing.T) {
	ensureLLVMToolchain(t)
	t.Parallel()

	root := repoRoot(t)
	sourcePath := filepath.Join(root, "testdata", "mt", "mt_correctness_channels.sg")
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read mt correctness source: %v", err)
	}
	source := string(sourceBytes)

	runMTSource(t, source, 20*time.Second)
}

func TestMTStructuredConcurrency(t *testing.T) {
	ensureLLVMToolchain(t)
	t.Parallel()

	source := `async fn spin(count: int) -> int {
    let mut i = 0;
    while i < count {
        checkpoint().await();
        i = i + 1;
    }
    return count;
}

async fn send_done(done: own Channel<int>, id: int, spins: int) -> int {
    let _ = spin(spins).await();
    done.send(id);
    return id;
}

async fn worker_recv(ch: own Channel<int>) -> int {
    checkpoint().await();
    ch.recv();
    return 1;
}

async fn join_worker(t: Task<int>) -> int {
    let r = t.await();
    let ok = compare r {
        Success(_) => true;
        Cancelled() => false;
    };
    if ok {
        return 0;
    }
    return 1;
}

async fn main_async() -> int {
    if rt_worker_count() <= 1:uint {
        return 90;
    }

    let pres = (async {
        let done = make_channel::<int>(8);
        let mut i = 0;
        while i < 4 {
            let c = done;
            let t = spawn send_done(c, i, 10 + i);
            let _ = t;
            i = i + 1;
        }
        return done;
    }).await();
    let mut done_ch = make_channel::<int>(0);
    let parent_ok = compare pres {
        Success(v) => {
            done_ch = v;
            true;
        }
        Cancelled() => false;
    };
    if !parent_ok {
        return 1;
    }
    let mut got = 0;
    while got < 4 {
        let v = done_ch.try_recv();
        let ok = compare v {
            Some(_) => true;
            nothing => false;
        };
        if !ok {
            return 2;
        }
        got = got + 1;
    }

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

    let long = spawn spin(200);
    let long_clone = long.clone();
    let r_timeout = timeout(long_clone, 5);
    let timed_out = compare r_timeout {
        Cancelled() => true;
        Success(_) => false;
    };
    if !timed_out {
        return 20;
    }
    let r_long = long.await();
    let long_cancelled = compare r_long {
        Cancelled() => true;
        Success(_) => false;
    };
    if !long_cancelled {
        return 21;
    }

    let short = spawn spin(3);
    let short_clone = short.clone();
    let r_short = timeout(short_clone, 200);
    let short_ok = compare r_short {
        Success(_) => true;
        Cancelled() => false;
    };
    if !short_ok {
        return 22;
    }
    let r_short2 = short.await();
    let short2_ok = compare r_short2 {
        Success(_) => true;
        Cancelled() => false;
    };
    if !short2_ok {
        return 23;
    }

    let join_res = async {
        let ch = make_channel::<int>(0);
        let worker = spawn worker_recv(ch);
        let worker_clone = worker.clone();
        let joiner = spawn join_worker(worker_clone);
        checkpoint().await();
        joiner.cancel();
        let jr = joiner.await();
        let jr_ok = compare jr {
            Cancelled() => true;
            Success(_) => false;
        };
        if !jr_ok {
            return 30;
        }
        worker.cancel();
        let _ = worker.await();
        return 0;
    }.await();
    let join_ok = compare join_res {
        Success(v) => v == 0;
        Cancelled() => false;
    };
    if !join_ok {
        return 31;
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

	runMTSource(t, source, 20*time.Second)
}

func TestMTBlockingPool(t *testing.T) {
	ensureLLVMToolchain(t)
	t.Parallel()

	source := `fn busy_loop(iter: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < iter {
        acc = acc + (i % 2);
        i = i + 1;
    }
    return acc;
}

async fn progress_worker(steps: int) -> int {
    let mut i = 0;
    while i < steps {
        checkpoint().await();
        i = i + 1;
    }
    return steps;
}

async fn cancel_waiter(started: own Channel<int>, spin_iters: int) -> int {
    let b = blocking {
        return busy_loop(spin_iters);
    };
    started.send(1);
    let _ = b.await();
    return 0;
}

async fn main_async() -> int {
    if rt_worker_count() <= 1:uint {
        return 90;
    }
    let workers: int = rt_worker_count() to int;
    let spin_iters: int = 50000;
    let mut blocking_count = workers;
    if blocking_count > 2 {
        blocking_count = 2;
    }

    let mut blocking_tasks: Task<int>[] = Array::<Task<int>>::with_len(blocking_count to uint);
    let mut i = 0;
    while i < blocking_count {
        let iters = spin_iters;
        blocking_tasks[i] = blocking {
            return busy_loop(iters);
        };
        i = i + 1;
    }
    let mut y = 0;
    while y < blocking_count {
        checkpoint().await();
        y = y + 1;
    }

    let mut progress_tasks: Task<int>[] = Array::<Task<int>>::with_len(workers to uint);
    i = 0;
    while i < workers {
        progress_tasks[i] = spawn progress_worker(200);
        i = i + 1;
    }
    let mut progress_cancelled = false;
    for t in progress_tasks {
        let r = t.await();
        let ok = compare r {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            progress_cancelled = true;
        }
    }
    if progress_cancelled {
        return 1;
    }
    for b in blocking_tasks {
        let r = b.await();
        let ok = compare r {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            return 2;
        }
    }

    let cancel_iters = spin_iters / 2;
    let blocker = blocking {
        return busy_loop(spin_iters);
    };
    let started = make_channel::<int>(1);
    let st = started;
    let waiter = spawn cancel_waiter(st, cancel_iters);
    let s = started.recv();
    let s_ok = compare s {
        Some(_) => true;
        nothing => false;
    };
    if !s_ok {
        return 10;
    }
    checkpoint().await();
    waiter.cancel();
    let wres = waiter.await();
    let w_ok = compare wres {
        Cancelled() => true;
        Success(_) => false;
    };
    if !w_ok {
        return 11;
    }
    let bres = blocker.await();
    let b_ok = compare bres {
        Success(_) => true;
        Cancelled() => true;
    };
    if !b_ok {
        return 12;
    }

    let stress_jobs: int = blocking_count * 2;
    let stress_tasks: int = workers * 2;
    let mut stress_blocking: Task<int>[] = Array::<Task<int>>::with_len(stress_jobs to uint);
    let mut j = 0;
    while j < stress_jobs {
        let iters = cancel_iters;
        stress_blocking[j] = blocking {
            return busy_loop(iters);
        };
        j = j + 1;
    }
    let mut k = 0;
    while k < workers {
        checkpoint().await();
        k = k + 1;
    }

    let mut stress_tasks_arr: Task<int>[] = Array::<Task<int>>::with_len(stress_tasks to uint);
    j = 0;
    while j < stress_tasks {
        stress_tasks_arr[j] = spawn progress_worker(100);
        j = j + 1;
    }
    let mut stress_cancelled = false;
    for t in stress_tasks_arr {
        let r = t.await();
        let ok = compare r {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            stress_cancelled = true;
        }
    }
    if stress_cancelled {
        return 20;
    }
    for b in stress_blocking {
        let r = b.await();
        let ok = compare r {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            return 21;
        }
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

	runMTSource(t, source, 20*time.Second)
}

func TestMTCorrectnessHTTPServer(t *testing.T) {
	ensureLLVMToolchain(t)
	t.Parallel()

	source := `import stdlib/http as http;

fn string_to_bytes(s: &string) -> byte[] {
    let view = s.bytes();
    let length: uint = view.__len();
    let mut out: byte[] = [];
    out.reserve(length);
    let len_i: int = length to int;
    let mut i: int = 0;
    while i < len_i {
        let b: byte = view[i];
        out.push(b);
        i = i + 1;
    }
    return out;
}

async fn handle(req: http.Request) -> http.Response {
    if req.path == "/slow" {
        sleep(200:uint).await();
    }
    let bytes = string_to_bytes(&"ok");
    return { status = 200, headers = [], body = http.Bytes(bytes) };
}

@entrypoint("argv")
fn main(port: uint) -> int {
    if rt_worker_count() <= 1:uint {
        return 90;
    }
    let cfg: http.ServerConfig = {
        max_pipeline_depth = 16:uint,
        max_initial_line_bytes = 1024:uint,
        max_header_bytes = 4096:uint,
        max_headers_count = 64:uint,
        max_body_bytes = 16:uint,
        idle_timeout_ms = 1000:uint,
        read_timeout_ms = 1000:uint,
        write_timeout_ms = 1000:uint
    };
    let handler: http.Handler = handle;
    http.serve("127.0.0.1", port, cfg, handler).await();
    return 0;
}
`

	outputPath := buildLLVMProgramFromSource(t, source)
	port := pickFreePort(t)
	portStr := strconv.Itoa(port)
	cmd := exec.Command(outputPath, portStr)
	cmd.Env = mtEnv(t)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("start http server: %v", err)
	}

	addr := net.JoinHostPort("127.0.0.1", portStr)
	fail := func(action string, err error) {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		t.Fatalf("%s: %v\nstdout:\n%s\nstderr:\n%s", action, err, outBuf.String(), errBuf.String())
	}

	if err := runMTHTTPKeepaliveScenario(addr); err != nil {
		fail("keepalive scenario failed", err)
	}
	if err := runMTHTTPConcurrentScenario(addr, 50); err != nil {
		fail("concurrent scenario failed", err)
	}
	if err := runMTHTTPPostScenario(addr); err != nil {
		fail("post scenario failed", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case err := <-waitCh:
		if err != nil {
			t.Fatalf("http server exit: %v\nstdout:\n%s\nstderr:\n%s", err, outBuf.String(), errBuf.String())
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-waitCh
		t.Fatalf("http server command timed out\nstdout:\n%s\nstderr:\n%s", outBuf.String(), errBuf.String())
	}
}

func runMTHTTPKeepaliveScenario(addr string) error {
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}
	reader := bufio.NewReader(conn)

	req := "GET /fast HTTP/1.1\r\nHost: example\r\n\r\n"
	if _, err = conn.Write([]byte(req)); err != nil {
		return fmt.Errorf("write keepalive req1: %w", err)
	}
	status, body, err := readHTTPResponse(reader)
	if err != nil {
		return fmt.Errorf("read keepalive resp1: %w", err)
	}
	if status != 200 || body != "ok" {
		return fmt.Errorf("keepalive resp1 mismatch: status=%d body=%q", status, body)
	}

	if _, err = conn.Write([]byte(req)); err != nil {
		return fmt.Errorf("write keepalive req2: %w", err)
	}
	status, body, err = readHTTPResponse(reader)
	if err != nil {
		return fmt.Errorf("read keepalive resp2: %w", err)
	}
	if status != 200 || body != "ok" {
		return fmt.Errorf("keepalive resp2 mismatch: status=%d body=%q", status, body)
	}
	return nil
}

func runMTHTTPConcurrentScenario(addr string, clients int) error {
	var wg sync.WaitGroup
	errCh := make(chan error, clients)
	for i := range clients {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
			if err != nil {
				errCh <- fmt.Errorf("dial %d: %w", id, err)
				return
			}
			defer conn.Close()
			if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				errCh <- fmt.Errorf("set deadline %d: %w", id, err)
				return
			}
			reader := bufio.NewReader(conn)
			path := "/fast"
			if id%10 == 0 {
				path = "/slow"
			}
			req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: example\r\n\r\n", path)
			if _, err = conn.Write([]byte(req)); err != nil {
				errCh <- fmt.Errorf("write %d: %w", id, err)
				return
			}
			status, body, err := readHTTPResponse(reader)
			if err != nil {
				errCh <- fmt.Errorf("read %d: %w", id, err)
				return
			}
			if status != 200 || body != "ok" {
				errCh <- fmt.Errorf("resp %d mismatch: status=%d body=%q", id, status, body)
				return
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func runMTHTTPPostScenario(addr string) error {
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		return fmt.Errorf("dial small: %w", err)
	}
	if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		_ = conn.Close()
		return fmt.Errorf("set small deadline: %w", err)
	}
	reader := bufio.NewReader(conn)
	body := "abcdefgh"
	req := fmt.Sprintf("POST /upload HTTP/1.1\r\nHost: example\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
	if _, err = conn.Write([]byte(req)); err != nil {
		_ = conn.Close()
		return fmt.Errorf("write small body: %w", err)
	}
	status, respBody, err := readHTTPResponse(reader)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("read small body: %w", err)
	}
	if status != 200 || respBody != "ok" {
		_ = conn.Close()
		return fmt.Errorf("small body mismatch: status=%d body=%q", status, respBody)
	}
	_ = conn.Close()

	largeConn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		return fmt.Errorf("dial large: %w", err)
	}
	defer largeConn.Close()
	if err = largeConn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("set large deadline: %w", err)
	}
	largeReader := bufio.NewReader(largeConn)
	largeBody := strings.Repeat("a", 32)
	largeReq := fmt.Sprintf("POST /upload HTTP/1.1\r\nHost: example\r\nContent-Length: %d\r\n\r\n%s", len(largeBody), largeBody)
	if _, err = largeConn.Write([]byte(largeReq)); err != nil {
		return fmt.Errorf("write large body: %w", err)
	}
	status, respBody, err = readHTTPResponse(largeReader)
	if err != nil {
		return fmt.Errorf("read large body: %w", err)
	}
	if status != 413 || respBody != "" {
		return fmt.Errorf("large body mismatch: status=%d body=%q", status, respBody)
	}
	if err := largeConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return fmt.Errorf("set close deadline: %w", err)
	}
	if _, err := largeReader.ReadByte(); err == nil {
		return fmt.Errorf("expected connection close after body limit, got byte")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("expected connection close after body limit: %w", err)
	}
	return nil
}
