//go:build runtime_v2_pending

package vm_test

import (
	"bufio"
	"bytes"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type fdRegistryLiveTraceServer struct {
	t        *testing.T
	cmd      *exec.Cmd
	outBuf   bytes.Buffer
	waitCh   chan error
	scanDone chan struct{}
	traceCh  chan string
	errMu    sync.Mutex
	errLines []string
}

func startFDRegistryLiveTraceServer(
	t *testing.T,
	source string,
	env []string,
	args ...string,
) *fdRegistryLiveTraceServer {
	t.Helper()
	outputPath := buildLLVMProgramFromSource(t, source)
	cmd := exec.Command(outputPath, args...)
	cmd.Env = overrideEnvVar(env, "SURGE_TRACE_EXEC", "1")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("create fd registry live stderr pipe: %v", err)
	}
	srv := &fdRegistryLiveTraceServer{
		t:        t,
		cmd:      cmd,
		waitCh:   make(chan error, 1),
		scanDone: make(chan struct{}),
		traceCh:  make(chan string, 16),
	}
	cmd.Stdout = &srv.outBuf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fd registry live trace fixture: %v", err)
	}
	go func() {
		defer close(srv.scanDone)
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			srv.errMu.Lock()
			srv.errLines = append(srv.errLines, line)
			srv.errMu.Unlock()
			if strings.HasPrefix(line, "TRACE_NET ") {
				select {
				case srv.traceCh <- line:
				default:
				}
			}
		}
		if err := scanner.Err(); err != nil {
			srv.errMu.Lock()
			srv.errLines = append(srv.errLines, "stderr scanner error: "+err.Error())
			srv.errMu.Unlock()
		}
	}()
	go func() {
		srv.waitCh <- cmd.Wait()
	}()
	return srv
}

func (s *fdRegistryLiveTraceServer) stderrString() string {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	if len(s.errLines) == 0 {
		return ""
	}
	return strings.Join(s.errLines, "\n") + "\n"
}

func (s *fdRegistryLiveTraceServer) fail(format string, args ...any) {
	s.t.Helper()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	<-s.waitCh
	<-s.scanDone
	s.t.Fatalf(format+"\nstdout:\n%s\nstderr:\n%s",
		append(args, s.outBuf.String(), s.stderrString())...)
}

func (s *fdRegistryLiveTraceServer) signalTrace() {
	s.t.Helper()
	if s.cmd.Process == nil {
		s.fail("fd registry fixture process is missing")
	}
	if err := s.cmd.Process.Signal(syscall.SIGUSR1); err != nil {
		s.fail("signal live trace: %v", err)
	}
}

func (s *fdRegistryLiveTraceServer) waitTrace(reason string, timeout time.Duration) string {
	s.t.Helper()
	prefix := "TRACE_NET reason=" + reason + " "
	deadline := time.After(timeout)
	for {
		select {
		case line := <-s.traceCh:
			if strings.HasPrefix(line, prefix) {
				return line
			}
		case <-deadline:
			s.fail("timed out waiting for %s trace", reason)
		}
	}
}

func (s *fdRegistryLiveTraceServer) waitExitZero(timeout time.Duration) string {
	s.t.Helper()
	select {
	case err := <-s.waitCh:
		<-s.scanDone
		if err != nil {
			s.t.Fatalf("fd registry live trace fixture exit: %v\nstdout:\n%s\nstderr:\n%s",
				err, s.outBuf.String(), s.stderrString())
		}
	case <-time.After(timeout):
		_ = s.cmd.Process.Kill()
		<-s.waitCh
		<-s.scanDone
		s.t.Fatalf("fd registry live trace fixture timed out\nstdout:\n%s\nstderr:\n%s",
			s.outBuf.String(), s.stderrString())
	}
	return s.stderrString()
}

func signalFDRegistryTrace(t *testing.T, srv *fdRegistryServer) {
	t.Helper()
	if srv.cmd.Process == nil {
		srv.fail("fd registry fixture process is missing")
	}
	if err := srv.cmd.Process.Signal(syscall.SIGUSR1); err != nil {
		srv.fail("signal live trace: %v", err)
	}
}

func requireFDRegistryZeroLegacyPollBuild(t *testing.T, values map[string]uint64, line string) {
	t.Helper()
	for _, field := range []string{
		"io_waiter_scan_entries",
		"io_waiter_net_entries",
		"io_poll_dedup_checks",
	} {
		value, ok := values[field]
		if !ok {
			t.Fatalf("missing %s in TRACE_NET line:\n%s", field, line)
		}
		if value != 0 {
			t.Fatalf("expected zero %s (registry-only poll input), got %d:\n%s",
				field, value, line)
		}
	}
}

func requireFDRegistryTraceLineZeroLegacyPollBuild(t *testing.T, line string) map[string]uint64 {
	t.Helper()
	values := make(map[string]uint64)
	for _, field := range strings.Fields(line) {
		name, raw, ok := strings.Cut(field, "=")
		if !ok || name == "reason" {
			continue
		}
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			t.Fatalf("parse TRACE_NET field %q in line:\n%s", field, line)
		}
		values[name] = value
	}
	requireFDRegistryZeroLegacyPollBuild(t, values, line)
	return values
}

func requireFDRegistryWakeFDTrace(
	t *testing.T,
	stderr, reason string,
	minWakeFD, minWaitersMax uint64,
) map[string]uint64 {
	t.Helper()
	values, line := runtimeV2NetTraceValues(t, stderr, reason)
	requireFDRegistryZeroLegacyPollBuild(t, values, line)
	if got := values["io_poll_wake_fd"]; got < minWakeFD {
		t.Fatalf("expected io_poll_wake_fd>=%d in TRACE_NET reason=%s, got %d:\n%s",
			minWakeFD, reason, got, line)
	}
	if got := values["io_poll_waiters_max"]; got < minWaitersMax {
		t.Fatalf("expected io_poll_waiters_max>=%d in TRACE_NET reason=%s, got %d:\n%s",
			minWaitersMax, reason, got, line)
	}
	return values
}

func requireFDRegistryWakeFDDeltaFromBaseline(
	t *testing.T,
	baselineLine, stderr, label string,
) {
	t.Helper()
	before := requireFDRegistryTraceLineZeroLegacyPollBuild(t, baselineLine)
	after, afterLine := runtimeV2NetTraceValues(t, stderr, "exit")
	requireFDRegistryZeroLegacyPollBuild(t, after, afterLine)
	if got := before["io_poll_waiters_max"]; got < 2 {
		t.Fatalf("expected two parked fd rows before %s release, got io_poll_waiters_max=%d:\n%s",
			label, got, baselineLine)
	}
	if after["io_poll_wake_fd"] <= before["io_poll_wake_fd"] {
		t.Fatalf("expected io_poll_wake_fd to increase after %s, before=%d after=%d\nsigusr1:\n%s\nexit:\n%s",
			label, before["io_poll_wake_fd"], after["io_poll_wake_fd"], baselineLine, afterLine)
	}
}

type fdRegistryFailer interface {
	fail(format string, args ...any)
}

func dialAndCloseFDRegistryPorts(t *testing.T, srv fdRegistryFailer, ports ...int) {
	t.Helper()
	for _, port := range ports {
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
		if err != nil {
			srv.fail("dial %s: %v", addr, err)
		}
		_ = conn.Close()
	}
}

func pickDistinctFDRegistryPort(t *testing.T, first int) int {
	t.Helper()
	for range 16 {
		port := pickFreePort(t)
		if port != first {
			return port
		}
	}
	t.Fatalf("pick distinct port from %d", first)
	return 0
}

const fdRegistryWakeFDInterestBody = `import stdlib/net as net;

async fn accept_one(handle: int) -> int {
    let listener: TcpListener = { __opaque: handle };
    let accept_res = net.accept(&listener).await();
    compare accept_res {
        Success(res) => {
            compare res {
                Success(conn) => {
                    let _ = net.close_conn(own conn);
                    return 0;
                }
                _ => return 1;
            };
        }
        Cancelled() => return 90;
    };
    return 91;
}

async fn serve_pair(first: TcpListener, second: TcpListener, delay_ms: uint) -> int {
    let first_handle: int = first.__opaque;
    let first_task = spawn accept_one(first_handle);
    sleep(delay_ms).await();

    let second_handle: int = second.__opaque;
    let second_task = spawn accept_one(second_handle);

    let first_res = first_task.await();
    let first_code = compare first_res { Success(code) => code; Cancelled() => 90; };
    let second_res = second_task.await();
    let second_code = compare second_res { Success(code) => code; Cancelled() => 91; };
    let _ = net.close_listener(own first);
    let _ = net.close_listener(own second);
    if first_code != 0 { return 2; }
    if second_code != 0 { return 3; }
    print("ok");
    return 0;
}

@entrypoint("argv")
fn main(first_port: uint, second_port: uint, delay_ms: uint) -> int {
    let first_res = net.listen("127.0.0.1", first_port);
    compare first_res {
        Success(first) => {
            let second_res = net.listen("127.0.0.1", second_port);
            compare second_res {
                Success(second) => {
                    let result = serve_pair(first, second, delay_ms).await();
                    return compare result { Success(code) => code; Cancelled() => 99; };
                }
                _ => return 2;
            };
        }
        _ => return 1;
    };
    return 3;
}
`

func TestRuntimeV2FDRegistryWakeFDObservedForInterestAddedDuringPoll(t *testing.T) {
	ensureLLVMToolchain(t)
	firstPort := pickFreePort(t)
	secondPort := pickDistinctFDRegistryPort(t, firstPort)
	source := fdRegistryWakeFDInterestBody
	srv := startFDRegistryServer(t, source, mtEnv(t),
		strconv.Itoa(firstPort), strconv.Itoa(secondPort), "200")

	time.Sleep(800 * time.Millisecond)
	signalFDRegistryTrace(t, srv)
	dialAndCloseFDRegistryPorts(t, srv, firstPort, secondPort)

	stderr := srv.waitExitZero(10 * time.Second)
	requireFDRegistryWakeFDTrace(t, stderr, "sigusr1", 1, 2)
	requireFDRegistryWakeFDTrace(t, stderr, "exit", 1, 2)
	if !strings.Contains(srv.outBuf.String(), "ok") {
		t.Fatalf("unexpected stdout: %q\nstderr:\n%s", srv.outBuf.String(), stderr)
	}
}

const fdRegistryCancelReadWakeFDBody = `import stdlib/net as net;

async fn read_until_cancel(handle: int) -> int {
    let conn: TcpConn = { __opaque: handle };
    let read_res = net.read_some(&conn, 1:uint).await();
    return compare read_res {
        Success(_) => 1;
        Cancelled() => 0;
    };
}

async fn wait_gate(gate_listener: TcpListener) -> int {
    let gate_res = net.accept(&gate_listener).await();
    compare gate_res {
        Success(conn_res) => {
            compare conn_res {
                Success(conn) => {
                    let _ = net.close_conn(own conn);
                    let _ = net.close_listener(own gate_listener);
                    return 0;
                }
                _ => return 1;
            };
        }
        Cancelled() => return 90;
    };
    return 91;
}

async fn serve(data_listener: TcpListener, gate_listener: TcpListener) -> int {
    let accept_res = net.accept(&data_listener).await();
    compare accept_res {
        Success(conn_res) => {
            compare conn_res {
                Success(conn) => {
                    let handle: int = conn.__opaque;
                    let waiter = spawn read_until_cancel(handle);
                    checkpoint().await();
                    checkpoint().await();
                    let gate_res = wait_gate(gate_listener).await();
                    let gate_code = compare gate_res { Success(code) => code; Cancelled() => 90; };
                    if gate_code != 0 { let _ = net.close_conn(own conn); return 5; }
                    waiter.cancel();
                    let waiter_res = waiter.await();
                    let cancelled: bool = compare waiter_res { Success(code) => code == 0; Cancelled() => true; };
                    let _ = net.close_conn(own conn);
                    let _ = net.close_listener(own data_listener);
                    if !cancelled { print("cancel_bad_result"); return 3; }
                    print("ok");
                    return 0;
                }
                _ => return 1;
            };
        }
        Cancelled() => return 1;
    };
    return 1;
}

@entrypoint("argv")
fn main(data_port: uint, gate_port: uint) -> int {
    let data_res = net.listen("127.0.0.1", data_port);
    compare data_res {
        Success(data_listener) => {
            let gate_res = net.listen("127.0.0.1", gate_port);
            compare gate_res {
                Success(gate_listener) => {
                    let result = serve(data_listener, gate_listener).await();
                    return compare result { Success(code) => code; Cancelled() => 99; };
                }
                _ => return 2;
            };
        }
        _ => return 1;
    };
    return 3;
}
`

func TestRuntimeV2FDRegistryCancelledInterestWakesPoller(t *testing.T) {
	ensureLLVMToolchain(t)
	port := pickFreePort(t)
	gatePort := pickDistinctFDRegistryPort(t, port)
	source := fdRegistryCancelReadWakeFDBody
	srv := startFDRegistryLiveTraceServer(
		t,
		source,
		mtEnv(t),
		strconv.Itoa(port),
		strconv.Itoa(gatePort),
	)

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		srv.fail("dial cancel wake-fd server: %v", err)
	}
	defer conn.Close()

	time.Sleep(800 * time.Millisecond)
	srv.signalTrace()
	baselineLine := srv.waitTrace("sigusr1", 5*time.Second)
	dialAndCloseFDRegistryPorts(t, srv, gatePort)

	stderr := srv.waitExitZero(10 * time.Second)
	requireFDRegistryWakeFDDeltaFromBaseline(t, baselineLine, stderr, "cancellation")
	if !strings.Contains(srv.outBuf.String(), "ok") {
		t.Fatalf("unexpected stdout: %q\nstderr:\n%s", srv.outBuf.String(), stderr)
	}
}
