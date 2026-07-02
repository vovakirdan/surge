//go:build runtime_v2_pending

package vm_test

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Epic 4 Task 3 (FD lifecycle contract tests). These tests pin the net fd
// lifecycle behavior the persistent fd registry migration (Tasks 5-11) must
// preserve; all four are green today and must stay green after the waiter-scan
// poll rebuild path is replaced. They assert only migration-durable counters:
// io_direct_waits, io_poll_calls, io_poll_net_ready, io_waiter_completed, and
// io_poll_waiters_max as the max distinct-fd-row count per poll set build.

type fdRegistryServer struct {
	t      *testing.T
	cmd    *exec.Cmd
	outBuf *bytes.Buffer
	errBuf *bytes.Buffer
	waitCh chan error
}

// startFDRegistryServer builds the LLVM-native fixture and starts it with
// SURGE_TRACE_EXEC=1 so the TRACE_NET reason=exit line is emitted.
func startFDRegistryServer(t *testing.T, source string, env []string, args ...string) *fdRegistryServer {
	t.Helper()
	outputPath := buildLLVMProgramFromSource(t, source)
	cmd := exec.Command(outputPath, args...)
	cmd.Env = overrideEnvVar(env, "SURGE_TRACE_EXEC", "1")
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fd registry fixture: %v", err)
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()
	return &fdRegistryServer{t: t, cmd: cmd, outBuf: &outBuf, errBuf: &errBuf, waitCh: waitCh}
}

func (s *fdRegistryServer) fail(format string, args ...any) {
	s.t.Helper()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	<-s.waitCh
	s.t.Fatalf(format+"\nstdout:\n%s\nstderr:\n%s", append(args, s.outBuf.String(), s.errBuf.String())...)
}

// waitExitZero requires a clean exit and returns the captured stderr so the
// caller can read the TRACE_NET reason=exit counters.
func (s *fdRegistryServer) waitExitZero(timeout time.Duration) string {
	s.t.Helper()
	select {
	case err := <-s.waitCh:
		if err != nil {
			s.t.Fatalf("fd registry fixture exit: %v\nstdout:\n%s\nstderr:\n%s",
				err, s.outBuf.String(), s.errBuf.String())
		}
	case <-time.After(timeout):
		_ = s.cmd.Process.Kill()
		<-s.waitCh
		s.t.Fatalf("fd registry fixture timed out\nstdout:\n%s\nstderr:\n%s",
			s.outBuf.String(), s.errBuf.String())
	}
	return s.errBuf.String()
}

// requireFDRegistryExitTrace checks the migration-durable counters on the
// TRACE_NET reason=exit line. io_poll_waiters_max==1 is the core contract:
// every poll set the fixture built held exactly one distinct fd row.
func requireFDRegistryExitTrace(t *testing.T, stderr string, minDirectWaits, minCompleted uint64) {
	t.Helper()
	values, line := runtimeV2NetTraceValues(t, stderr, "exit")
	if got := values["io_poll_waiters_max"]; got != 1 {
		t.Fatalf("expected io_poll_waiters_max=1 (one distinct fd row per poll build), got %d:\n%s",
			got, line)
	}
	if values["io_poll_calls"] == 0 {
		t.Fatalf("expected io_poll_calls>=1 (fixture must exercise the poller):\n%s", line)
	}
	if values["io_poll_net_ready"] == 0 {
		t.Fatalf("expected io_poll_net_ready>=1 (poll must observe fd readiness):\n%s", line)
	}
	if got := values["io_direct_waits"]; got < minDirectWaits {
		t.Fatalf("expected io_direct_waits>=%d (parked net interest registrations), got %d:\n%s",
			minDirectWaits, got, line)
	}
	if got := values["io_waiter_completed"]; got < minCompleted {
		t.Fatalf("expected io_waiter_completed>=%d (readiness completions), got %d:\n%s",
			minCompleted, got, line)
	}
}

// fdRegistryPrelude is shared by fixtures 1-3: read/write helpers over a
// copied conn handle, mirroring the stdlib net wrapper result shapes.
const fdRegistryPrelude = `import stdlib/net as net;

async fn read_nonempty(handle: int) -> bool {
    let conn: TcpConn = { __opaque: handle };
    let read_res = net.read_some(&conn, 16:uint).await();
    return compare read_res {
        Success(res) => compare res { Success(bytes) => bytes.__len() != 0:uint; _ => false; };
        Cancelled() => false;
    };
}

async fn write_all_ok(handle: int, data: byte[]) -> bool {
    let conn: TcpConn = { __opaque: handle };
    let write_res = net.write_all(&conn, data).await();
    return compare write_res {
        Success(res) => compare res { Success(_) => true; _ => false; };
        Cancelled() => false;
    };
}
`

// fdRegistryMain is the shared argv entrypoint: fixtures define
// serve_one(listener, arg) and receive port plus one numeric argument.
const fdRegistryMain = `
@entrypoint("argv")
fn main(port: uint, arg: uint) -> int {
    let listen_res = net.listen("127.0.0.1", port);
    compare listen_res {
        Success(listener) => {
            let result = serve_one(listener, arg).await();
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

// fdRegistryPingBody serves arg sequential read/write rounds on one accepted
// conn, so at most one net interest is parked at a time.
const fdRegistryPingBody = `
fn pong() -> byte[] {
    let mut out: byte[] = [];
    out.push(80:byte);
    out.push(79:byte);
    out.push(78:byte);
    out.push(71:byte);
    out.push(10:byte);
    return out;
}

async fn serve_one(listener: TcpListener, arg: uint) -> int {
    let accept_res = net.accept(&listener).await();
    compare accept_res {
        Success(conn_res) => {
            compare conn_res {
                Success(conn) => {
                    let handle: int = conn.__opaque;
                    let mut seen: uint = 0:uint;
                    while seen < arg {
                        let read_task = read_nonempty(handle).await();
                        let read_ok: bool = compare read_task { Success(flag) => flag; Cancelled() => false; };
                        if !read_ok { let _ = net.close_conn(own conn); return 2; }
                        let write_task = write_all_ok(handle, pong()).await();
                        let write_ok: bool = compare write_task { Success(flag) => flag; Cancelled() => false; };
                        if !write_ok { let _ = net.close_conn(own conn); return 3; }
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
`

// TestRuntimeV2FDRegistryRepeatedReadinessSingleFD: repeated readiness cycles
// on one fd never accumulate duplicate fd rows. The idle gap before the final
// round forces at least one deterministic re-park of the same read interest.
func TestRuntimeV2FDRegistryRepeatedReadinessSingleFD(t *testing.T) {
	ensureLLVMToolchain(t)
	port := pickFreePort(t)
	rounds := 12
	source := fdRegistryPrelude + fdRegistryPingBody + fdRegistryMain
	srv := startFDRegistryServer(t, source, mtEnv(t), strconv.Itoa(port), strconv.Itoa(rounds))

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		srv.fail("dial repeated readiness server: %v", err)
	}
	if err = conn.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		_ = conn.Close()
		srv.fail("set deadline: %v", err)
	}
	for i := 0; i < rounds; i++ {
		if i == rounds-1 {
			// Idle gap: the server must re-park read interest on the same fd
			// instead of finishing on the ready-now fast path.
			time.Sleep(250 * time.Millisecond)
		}
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

	stderr := srv.waitExitZero(10 * time.Second)
	requireFDRegistryExitTrace(t, stderr, 1, 1)
}

// fdRegistryReadWriteBody parks a reader task (read interest) and a bulk
// writer task (write interest, arg payload bytes) on the same accepted fd.
const fdRegistryReadWriteBody = `
async fn read_one(handle: int) -> int {
    let read_task = read_nonempty(handle).await();
    let read_ok: bool = compare read_task { Success(flag) => flag; Cancelled() => false; };
    if !read_ok { return 1; }
    return 0;
}

async fn write_bulk(handle: int, total: uint) -> int {
    let payload: byte[] = Array::<byte>.with_len(total);
    let write_task = write_all_ok(handle, payload).await();
    let write_ok: bool = compare write_task { Success(flag) => flag; Cancelled() => false; };
    if !write_ok { return 1; }
    return 0;
}

async fn serve_one(listener: TcpListener, arg: uint) -> int {
    let accept_res = net.accept(&listener).await();
    compare accept_res {
        Success(conn_res) => {
            compare conn_res {
                Success(conn) => {
                    let handle: int = conn.__opaque;
                    let reader = spawn read_one(handle);
                    checkpoint().await();
                    let writer = spawn write_bulk(handle, arg);
                    let writer_res = writer.await();
                    let writer_code = compare writer_res { Success(code) => code; Cancelled() => 90; };
                    let reader_res = reader.await();
                    let reader_code = compare reader_res { Success(code) => code; Cancelled() => 91; };
                    let _ = net.close_conn(own conn);
                    if writer_code != 0 { return 2; }
                    if reader_code != 0 { return 3; }
                    return 0;
                }
                _ => return 1;
            };
        }
        Cancelled() => return 1;
    };
    return 1;
}
`

// TestRuntimeV2FDRegistryReadWriteInterestSharesFDRow: read and write
// interest parked on the same fd collapse into one fd row per poll build.
// The 32MB payload far exceeds loopback socket buffering, so the writer is
// guaranteed to park on write interest while the reader stays parked on read
// interest for the same fd.
func TestRuntimeV2FDRegistryReadWriteInterestSharesFDRow(t *testing.T) {
	ensureLLVMToolchain(t)
	port := pickFreePort(t)
	const payloadLen = 32 << 20
	source := fdRegistryPrelude + fdRegistryReadWriteBody + fdRegistryMain
	env := overrideEnvVar(mtEnv(t), "SURGE_THREADS", "1")
	srv := startFDRegistryServer(t, source, env, strconv.Itoa(port), strconv.Itoa(payloadLen))

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		srv.fail("dial read+write interest server: %v", err)
	}
	if err = conn.SetDeadline(time.Now().Add(60 * time.Second)); err != nil {
		_ = conn.Close()
		srv.fail("set deadline: %v", err)
	}
	// Park window: the writer fills the socket buffers and parks on write
	// interest while the reader is already parked on read interest. Exit
	// correctness never depends on this sleep; both tasks cannot complete
	// before the client drains and sends the release byte below.
	time.Sleep(300 * time.Millisecond)
	if _, err = io.CopyN(io.Discard, conn, payloadLen); err != nil {
		_ = conn.Close()
		srv.fail("drain %d payload bytes: %v", payloadLen, err)
	}
	if _, err = conn.Write([]byte("G")); err != nil {
		_ = conn.Close()
		srv.fail("write reader release byte: %v", err)
	}
	_ = conn.Close()

	stderr := srv.waitExitZero(30 * time.Second)
	requireFDRegistryExitTrace(t, stderr, 2, 2)
}

// fdRegistryDuplicateReadersBody parks two reader tasks on the same fd read
// interest; each acknowledges one nonempty read with a single ack byte.
const fdRegistryDuplicateReadersBody = `
fn ack() -> byte[] {
    let mut out: byte[] = [];
    out.push(75:byte);
    return out;
}

async fn read_and_ack(handle: int) -> int {
    let read_task = read_nonempty(handle).await();
    let read_ok: bool = compare read_task { Success(flag) => flag; Cancelled() => false; };
    if !read_ok { return 1; }
    let write_task = write_all_ok(handle, ack()).await();
    let write_ok: bool = compare write_task { Success(flag) => flag; Cancelled() => false; };
    if !write_ok { return 2; }
    return 0;
}

async fn serve_one(listener: TcpListener, arg: uint) -> int {
    let _ = arg;
    let accept_res = net.accept(&listener).await();
    compare accept_res {
        Success(conn_res) => {
            compare conn_res {
                Success(conn) => {
                    let handle: int = conn.__opaque;
                    let first = spawn read_and_ack(handle);
                    let second = spawn read_and_ack(handle);
                    let first_res = first.await();
                    let first_code = compare first_res { Success(code) => code; Cancelled() => 90; };
                    let second_res = second.await();
                    let second_code = compare second_res { Success(code) => code; Cancelled() => 91; };
                    let _ = net.close_conn(own conn);
                    if first_code != 0 { return 2; }
                    if second_code != 0 { return 3; }
                    return 0;
                }
                _ => return 1;
            };
        }
        Cancelled() => return 1;
    };
    return 1;
}
`

// TestRuntimeV2FDRegistryDuplicateReadWaitersBothComplete: current semantics
// allow two waiters parked on the same fd read interest; both must complete,
// and the duplicate interest still collapses to one fd row per poll build.
// The one-byte-then-ack handshake is order-independent, so it stays correct
// under wake-all today and under any future wake-one registry semantics.
func TestRuntimeV2FDRegistryDuplicateReadWaitersBothComplete(t *testing.T) {
	ensureLLVMToolchain(t)
	port := pickFreePort(t)
	source := fdRegistryPrelude + fdRegistryDuplicateReadersBody + fdRegistryMain
	env := overrideEnvVar(mtEnv(t), "SURGE_THREADS", "1")
	srv := startFDRegistryServer(t, source, env, strconv.Itoa(port), "0")

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		srv.fail("dial duplicate readers server: %v", err)
	}
	if err = conn.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		_ = conn.Close()
		srv.fail("set deadline: %v", err)
	}
	// Park window: both readers register duplicate interest on the same fd
	// before any data arrives. Correctness never depends on this sleep; the
	// readers cannot complete before the bytes below are sent.
	time.Sleep(250 * time.Millisecond)
	for i := 0; i < 2; i++ {
		if _, err = conn.Write([]byte{'A' + byte(i)}); err != nil {
			_ = conn.Close()
			srv.fail("write wake byte %d: %v", i, err)
		}
		var ackBuf [1]byte
		if _, err = io.ReadFull(conn, ackBuf[:]); err != nil {
			_ = conn.Close()
			srv.fail("read ack %d: %v", i, err)
		}
		if ackBuf[0] != 'K' {
			_ = conn.Close()
			srv.fail("unexpected ack %d: %q", i, string(ackBuf[:]))
		}
	}
	_ = conn.Close()

	stderr := srv.waitExitZero(10 * time.Second)
	requireFDRegistryExitTrace(t, stderr, 2, 2)
}

// fdRegistryClosedFDSource is self-contained: it listens, connects to itself,
// then proves that read, write, re-close, and accept through handle copies of
// closed endpoints fail fast with a synchronous NetError (code>=2: neither
// success nor would-block) instead of parking net interest. Today the copies
// observe NET_ERR_IO(8) via EBADF (a handle copy clones the NetConn view: fd
// number without the closed flag); a generation-guarded registry may tighten
// this to NET_ERR_NOT_CONNECTED(5) in Task 9, which this test permits.
const fdRegistryClosedFDSource = `import stdlib/net as net;

fn probe() -> byte[] {
    let mut out: byte[] = [];
    out.push(88:byte);
    return out;
}

@entrypoint
fn main() -> int {
    let listen_res = net.listen("127.0.0.1", %[1]d:uint);
    compare listen_res {
        Success(listener) => {
            let conn_res = net.connect("127.0.0.1", %[1]d:uint);
            compare conn_res {
                Success(conn) => {
                    let read_copy: TcpConn = { __opaque: conn.__opaque };
                    let write_copy: TcpConn = { __opaque: conn.__opaque };
                    let close_copy: TcpConn = { __opaque: conn.__opaque };
                    let close_res = net.close_conn(own conn);
                    let close_ok: bool = compare close_res { Success(_) => true; _ => false; };
                    if !close_ok { return 11; }
                    let read_res = net.read_some(&read_copy, 4:uint).await();
                    let read_code: uint = compare read_res {
                        Success(res) => compare res { Success(_) => 0:uint; err => err.code; };
                        Cancelled() => 900:uint;
                    };
                    if read_code < 2:uint { return 12; }
                    let write_res = net.write_all(&write_copy, probe()).await();
                    let write_code: uint = compare write_res {
                        Success(res) => compare res { Success(_) => 0:uint; err => err.code; };
                        Cancelled() => 900:uint;
                    };
                    if write_code < 2:uint { return 13; }
                    let reclose_res = net.close_conn(own close_copy);
                    let reclose_code: uint = compare reclose_res { Success(_) => 0:uint; err => err.code; };
                    if reclose_code < 2:uint { return 14; }
                    let accept_copy: TcpListener = { __opaque: listener.__opaque };
                    let lclose_res = net.close_listener(own listener);
                    let lclose_ok: bool = compare lclose_res { Success(_) => true; _ => false; };
                    if !lclose_ok { return 15; }
                    let accept_res = net.accept(&accept_copy).await();
                    let accept_code: uint = compare accept_res {
                        Success(res) => compare res { Success(_) => 0:uint; err => err.code; };
                        Cancelled() => 900:uint;
                    };
                    if accept_code < 2:uint { return 16; }
                    print("ok");
                    return 0;
                }
                _ => return 17;
            };
        }
        _ => return 18;
    };
    return 19;
}
`

// TestRuntimeV2FDRegistryClosedFDFailsFast: operations on closed net handles
// must fail fast with a synchronous NetError and never register net interest.
// The program is self-contained; distinct exit codes identify the first
// failed expectation, and the run timeout is the no-park proof.
func TestRuntimeV2FDRegistryClosedFDFailsFast(t *testing.T) {
	ensureLLVMToolchain(t)
	port := pickFreePort(t)
	source := fmt.Sprintf(fdRegistryClosedFDSource, port)
	outputPath := buildLLVMProgramFromSource(t, source)
	dur, res := runBinaryWithTimeout(t, outputPath, mtEnv(t), 10*time.Second)
	if res.exitCode != 0 {
		t.Fatalf("closed-fd fixture failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
			res.exitCode, dur, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "ok") {
		t.Fatalf("unexpected stdout: %q", res.stdout)
	}
}
