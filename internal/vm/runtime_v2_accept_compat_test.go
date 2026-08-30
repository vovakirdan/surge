package vm_test

import (
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const runtimeV2AcceptPingSource = `import stdlib/net as net;

fn pong() -> byte[] {
    let mut out: byte[] = [];
    out.push(80:byte);
    out.push(79:byte);
    out.push(78:byte);
    out.push(71:byte);
    out.push(10:byte);
    return out;
}

async fn serve_one(listener: TcpListener, count: uint) -> int {
    let accept_task = net.accept(&listener).await();
    compare accept_task {
        Success(conn_res) => {
            compare conn_res {
                Success(conn) => {
                    let mut seen: uint = 0:uint;
                    while seen < count {
                        let read_task = net.read_some(&conn, 16:uint).await();
                        let read_ok: bool = compare read_task {
                            Success(read_res) => compare read_res {
                                Success(bytes) => bytes.__len() != 0:uint;
                                _ => false;
                            };
                            Cancelled() => false;
                        };
                        if !read_ok {
                            let _ = net.close_conn(own conn);
                            return 2;
                        }
                        let write_task = net.write_all(&conn, pong()).await();
                        let write_ok: bool = compare write_task {
                            Success(write_res) => compare write_res {
                                Success(_) => true;
                                _ => false;
                            };
                            Cancelled() => false;
                        };
                        if !write_ok {
                            let _ = net.close_conn(own conn);
                            return 3;
                        }
                        seen = seen + 1:uint;
                    }
                    let _ = net.close_conn(own conn);
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
fn main(port: uint, count: uint) -> int {
    let listen_res = net.listen("127.0.0.1", port);
    compare listen_res {
        Success(listener) => {
            let result = serve_one(listener, count).await();
            return compare result {
                Success(code) => code;
                Cancelled() => 99;
            };
        }
        _ => return 1;
    };
    return 1;
}
`

type runtimeV2AcceptServer struct {
	t      *testing.T
	cmd    *exec.Cmd
	outBuf *lockedBuffer
	errBuf *lockedBuffer
	waitCh chan error
}

func startRuntimeV2AcceptServer(
	t *testing.T,
	source string,
	env []string,
	args ...string,
) *runtimeV2AcceptServer {
	t.Helper()
	outputPath := buildLLVMProgramFromSource(t, source)
	cmd := exec.Command(outputPath, args...)
	cmd.Env = env
	// Locked, not bytes.Buffer: fail() reads these WHILE the fixture is still
	// running, to wait for the live trace dump, and exec fills them from a
	// goroutine of its own.
	outBuf, errBuf := &lockedBuffer{}, &lockedBuffer{}
	cmd.Stdout = outBuf
	cmd.Stderr = errBuf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start Runtime V2 accept fixture: %v", err)
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()
	return &runtimeV2AcceptServer{t: t, cmd: cmd, outBuf: outBuf, errBuf: errBuf, waitCh: waitCh}
}

// fail reports a failure with everything the fixture can still be made to say.
//
// It asks for the live trace BEFORE killing, and that ordering is the whole
// point. These fixtures run with SURGE_TRACE_EXEC=1 and the dump that would
// name which lane never answered is written by the process itself, so killing
// first leaves a report with two empty sections where the evidence was. That
// is the third time the same shape has cost a diagnosis here: RV2-DEBT-310 (a
// trace dump asked for and never waited for), RV2-DEBT-311 (a valgrind run
// killed on timeout), and RV2-DEBT-312, whose first occurrence -- one HTTP
// client of eight timing out at shards-8 -- printed an empty stderr precisely
// because the server had not exited.
func (s *runtimeV2AcceptServer) fail(format string, args ...any) {
	s.t.Helper()
	note := s.requestLiveTrace(5 * time.Second)
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	<-s.waitCh
	s.t.Fatalf(format+"\n%s\nstdout:\n%s\nstderr:\n%s",
		append(args, note, s.outBuf.String(), s.errBuf.String())...)
}

// requestLiveTrace signals the fixture for a trace dump and waits for it,
// bounded by budget. It returns a note saying which of the possible things
// happened, because they are different diagnoses and an empty section cannot
// tell them apart.
//
// The handler is installed only when SURGE_TRACE_EXEC is set (see
// rt_exec_trace_init), and SIGUSR1's default disposition is to terminate, so
// an untraced fixture dies here instead of dumping. That is harmless -- the
// caller was about to kill it -- and the wait ends on the exit rather than
// burning the budget.
//
// A traced fixture that does not dump is itself a finding: the dump is drained
// at a safepoint (rt_trace_drain_signal_dump), so silence means no lane
// reached one, which is what a wedged runtime looks like from outside.
func (s *runtimeV2AcceptServer) requestLiveTrace(budget time.Duration) string {
	if s.cmd.Process == nil {
		return "live trace: fixture never started"
	}
	if exited, _ := s.peekExit(); exited {
		return "live trace: fixture had already exited"
	}
	if err := s.cmd.Process.Signal(syscall.SIGUSR1); err != nil {
		return fmt.Sprintf("live trace: signal failed: %v", err)
	}
	const awaited = "reason=sigusr1"
	deadline := time.Now().Add(budget)
	for {
		if strings.Contains(s.errBuf.String(), awaited) {
			return "live trace: dump arrived, reason=sigusr1 is in the stderr below"
		}
		if exited, _ := s.peekExit(); exited {
			return "live trace: fixture exited on the signal without dumping (untraced, or it died first)"
		}
		if time.Now().After(deadline) {
			return fmt.Sprintf(
				"live trace: NO dump within %s of SIGUSR1 while the fixture was still alive -- no lane reached a safepoint", budget)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// peekExit reports whether the fixture has exited without consuming the
// result, so fail() and waitExitZero() can still take it.
func (s *runtimeV2AcceptServer) peekExit() (bool, error) {
	select {
	case err := <-s.waitCh:
		s.waitCh <- err
		return true, err
	default:
		return false, nil
	}
}

func (s *runtimeV2AcceptServer) waitExitZero(timeout time.Duration) (string, string) {
	s.t.Helper()
	select {
	case err := <-s.waitCh:
		if err != nil {
			s.t.Fatalf("Runtime V2 accept fixture exit: %v\nstdout:\n%s\nstderr:\n%s",
				err, s.outBuf.String(), s.errBuf.String())
		}
	case <-time.After(timeout):
		_ = s.cmd.Process.Kill()
		<-s.waitCh
		s.t.Fatalf("Runtime V2 accept fixture timed out\nstdout:\n%s\nstderr:\n%s",
			s.outBuf.String(), s.errBuf.String())
	}
	return s.outBuf.String(), s.errBuf.String()
}

func runtimeV2AcceptEnv(t *testing.T) []string {
	t.Helper()
	return envWithStdlib(repoRoot(t))
}

// TestRuntimeV2AcceptFixtureAnswersALiveTraceRequest is the stand behind
// fail()'s promise. fail() cannot be asserted directly -- it ends the test --
// so the mechanism it depends on is exercised on its own: a healthy,
// still-running fixture must answer SIGUSR1 with a dump, and the wait must see
// it. If the signal is dropped, or the wait is removed and the buffer read
// before the handler runs, this goes red with the note the report would have
// carried instead. Without it, the only proof that a failure now says
// something would be the next failure.
func TestRuntimeV2AcceptFixtureAnswersALiveTraceRequest(t *testing.T) {
	ensureLLVMToolchain(t)

	port := pickFreePort(t)
	env := overrideEnvVar(runtimeV2AcceptEnv(t), "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_TRACE_EXEC", "1")
	srv := startRuntimeV2AcceptServer(t, runtimeV2AcceptPingSource, env, strconv.Itoa(port), "4")

	// Reach the fixture first: a dump requested before the runtime is up
	// would measure startup, not the answer.
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		srv.fail("dial live-trace fixture: %v", err)
	}
	defer func() { _ = conn.Close() }()

	note := srv.requestLiveTrace(10 * time.Second)
	if !strings.Contains(note, "dump arrived") {
		t.Fatalf("live trace was not answered by a running fixture: %s\nstderr:\n%s", note, srv.errBuf.String())
	}
	if !strings.Contains(srv.errBuf.String(), "reason=sigusr1") {
		t.Fatalf("note claims a dump arrived but stderr carries none:\n%s", srv.errBuf.String())
	}

	if srv.cmd.Process != nil {
		_ = srv.cmd.Process.Kill()
	}
	<-srv.waitCh
}

func TestRuntimeV2AcceptShardOneNativeNetCompatibility(t *testing.T) {
	ensureLLVMToolchain(t)

	port := pickFreePort(t)
	env := overrideEnvVar(runtimeV2AcceptEnv(t), "SURGE_SHARDS", "1")
	srv := startRuntimeV2AcceptServer(
		t,
		runtimeV2AcceptPingSource,
		env,
		strconv.Itoa(port),
		"4",
	)

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		srv.fail("dial accept compatibility fixture: %v", err)
	}
	if err = conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		_ = conn.Close()
		srv.fail("set deadline: %v", err)
	}
	for i := range 4 {
		if _, err = conn.Write([]byte("PING\n")); err != nil {
			_ = conn.Close()
			srv.fail("write ping %d: %v", i, err)
		}
		var buf [5]byte
		if _, err = io.ReadFull(conn, buf[:]); err != nil {
			_ = conn.Close()
			srv.fail("read pong %d: %v", i, err)
		}
		if string(buf[:]) != "PONG\n" {
			_ = conn.Close()
			srv.fail("unexpected response %d: %q", i, string(buf[:]))
		}
	}
	_ = conn.Close()

	srv.waitExitZero(10 * time.Second)
}
