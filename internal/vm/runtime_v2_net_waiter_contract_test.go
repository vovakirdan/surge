//go:build runtime_v2_pending

package vm_test

import (
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRuntimeV2NetWaiterTraceContract(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `import stdlib/net as net;

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

	outputPath := buildLLVMProgramFromSource(t, source)
	port := pickFreePort(t)
	requestCount := 12
	cmd := exec.Command(outputPath, strconv.Itoa(port), strconv.Itoa(requestCount))
	cmd.Env = overrideEnvVar(mtEnv(t), "SURGE_TRACE_EXEC", "1")
	// The buffers are read WHILE the process is running -- to wait for the live
	// dump below, and on every failure path -- and exec fills them from a
	// goroutine of its own, so a plain bytes.Buffer here is a data race and not
	// merely untidy.
	outBuf, errBuf := &lockedBuffer{}, &lockedBuffer{}
	cmd.Stdout = outBuf
	cmd.Stderr = errBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("start net trace contract server: %v", err)
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	fail := func(format string, args ...any) {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-waitCh
		t.Fatalf(format+"\nstdout:\n%s\nstderr:\n%s", append(args, outBuf.String(), errBuf.String())...)
	}

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		fail("dial net trace contract server: %v", err)
	}
	if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		_ = conn.Close()
		fail("set deadline: %v", err)
	}

	for i := 0; i < requestCount; i++ {
		if _, err = conn.Write([]byte("PING\n")); err != nil {
			_ = conn.Close()
			fail("write ping: %v", err)
		}
		var buf [5]byte
		if _, err = io.ReadFull(conn, buf[:]); err != nil {
			_ = conn.Close()
			fail("read pong: %v", err)
		}
		if string(buf[:]) != "PONG\n" {
			_ = conn.Close()
			fail("unexpected response: %q", string(buf[:]))
		}
		if i == requestCount/2 {
			if err = cmd.Process.Signal(syscall.SIGUSR1); err != nil {
				_ = conn.Close()
				fail("signal live trace: %v", err)
			}
			// AND THEN WAIT FOR THE DUMP, before driving the server towards its
			// exit. A signal is delivered asynchronously and the dump is
			// written by whichever lane next reaches a safepoint; the request
			// loop below is what ends the program. Sending the signal and then
			// asserting the dump at the end asserts that the handler always
			// wins that race, and nothing promises it.
			//
			// IT LOSES UNDER LOAD AND ONLY UNDER LOAD, which is why the race
			// went unnoticed. On the dedicated machine this row is 0 red of 20
			// run by itself on an idle host, and 1 red of 5 inside
			// `make runtime-v2-check`, where nineteen other gates have just
			// run. The command that goes red is the aggregate, so that is the
			// command this fix has to be counted against.
			//
			// Waiting on the line is not a sleep: the deadline only bounds how
			// long a genuine loss takes to report.
			// The prefix is the whole record and not just the reason: this run
			// sets SURGE_TRACE_EXEC too, so TRACE_EXEC carries reason=sigusr1
			// as well, and waiting on the reason alone would let the loop
			// continue before the TRACE_NET line this test asserts exists.
			awaited := "TRACE_NET reason=sigusr1 "
			deadline := time.Now().Add(10 * time.Second)
			for !strings.Contains(errBuf.String(), awaited) {
				if time.Now().After(deadline) {
					_ = conn.Close()
					fail("live TRACE_NET dump never arrived within 10s of SIGUSR1")
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
	_ = conn.Close()

	select {
	case err := <-waitCh:
		if err != nil {
			t.Fatalf("net trace contract server exit: %v\nstdout:\n%s\nstderr:\n%s",
				err, outBuf.String(), errBuf.String())
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-waitCh
		t.Fatalf("net trace contract server timed out\nstdout:\n%s\nstderr:\n%s",
			outBuf.String(), errBuf.String())
	}

	stderr := errBuf.String()
	// The live dump is taken WHILE the runtime works, the exit dump after it
	// has stopped, and only the second is a quiescent cut. See the rebuild
	// check at the bottom of the helper for what that distinction buys.
	requireRuntimeV2NetTraceContract(t, stderr, "sigusr1", false)
	requireRuntimeV2NetTraceContract(t, stderr, "exit", true)
}

func requireRuntimeV2NetTraceContract(t *testing.T, stderr string, reason string, quiescent bool) {
	t.Helper()
	values, line := runtimeV2NetTraceValues(t, stderr, reason)
	for _, field := range []string{
		"runtime_shards",
		"io_poll_calls",
		"io_poll_timeouts",
		"io_poll_wake_fd",
		"io_poll_net_ready",
		"io_poll_errors",
		"io_poll_timeout_last_ms",
		"io_poll_timeout_max_ms",
		"io_poll_waiters_last",
		"io_poll_waiters_max",
		"io_poll_waiters_total",
		"io_direct_waits",
		"io_waiter_scan_entries",
		"io_waiter_net_entries",
		"io_poll_rebuilds",
		"io_poll_allocs",
		"io_poll_dedup_checks",
		"io_waiter_complete_calls",
		"io_waiter_completed",
		"accept_owner_total",
		"accept_owner_active_shards",
		"accept_owner_min",
		"accept_owner_max",
		"accept_owner_imbalance",
		"global_path_fallbacks",
		"fd_ready_batches",
		"fd_ready_batch_fds_total",
		"fd_ready_batch_fds_max",
	} {
		if _, ok := values[field]; !ok {
			t.Fatalf("missing %s in TRACE_NET %s line:\n%s", field, reason, line)
		}
	}
	for _, field := range []string{
		"io_poll_calls",
		"io_poll_net_ready",
		"io_poll_waiters_max",
		"io_poll_waiters_total",
		"io_direct_waits",
		"io_poll_rebuilds",
		"io_waiter_complete_calls",
		"io_waiter_completed",
	} {
		if values[field] == 0 {
			t.Fatalf("expected non-zero %s in TRACE_NET %s line:\n%s", field, reason, line)
		}
	}
	// contract update: the poll set derives from persistent fd
	// registry rows, so the legacy waiter-derived rebuild path (full waiter
	// store scan plus O(n^2) fd dedup) must never run. These counters staying
	// zero is the machine-checkable acceptance evidence for that.
	for _, field := range []string{
		"io_waiter_scan_entries",
		"io_waiter_net_entries",
		"io_poll_dedup_checks",
	} {
		if values[field] != 0 {
			t.Fatalf("expected zero %s (legacy waiter-derived poll rebuild must be unused) "+
				"in TRACE_NET %s line:\n%s", field, reason, line)
		}
	}
	// ONE REBUILD PER POLL CALL -- exactly, but only where the two can be read
	// as a cut. `rt_net_trace_poll_start_enabled` increments rebuilds and then
	// calls on adjacent lines, relaxed, so a dump taken WHILE the runtime works
	// can land between them and read more rebuilds than calls. That is not a
	// defect and a stand must not call it one; it is what the model says about
	// any summed snapshot, that a difference of two counters is exact only over
	// a window nothing else moved in.
	//
	// The bound on a live cut is the number of LANES that can sit inside that
	// pair at once, which is the shard count reported on this very line -- not
	// one. Bounding it at one would only move the flake from "sometimes the
	// dump is missing" to "sometimes two shards polled together", and this row
	// has already paid for one such assumption.
	//
	// The exit dump is taken after the runtime has stopped, so it is a real cut
	// and keeps the exact equality.
	rebuilds, calls := values["io_poll_rebuilds"], values["io_poll_calls"]
	if quiescent && rebuilds != calls {
		t.Fatalf("poll rebuilds must equal poll calls in the quiescent TRACE_NET %s line:\n%s",
			reason, line)
	}
	if !quiescent && (rebuilds < calls || rebuilds > calls+values["runtime_shards"]) {
		t.Fatalf("poll rebuilds %d must be poll calls %d, or at most one per shard more "+
			"while rebuilds are in flight across %d shards, in TRACE_NET %s line:\n%s",
			rebuilds, calls, values["runtime_shards"], reason, line)
	}
	if values["io_poll_waiters_total"] < values["io_poll_calls"] {
		t.Fatalf("poll waiter total must cover at least one registry row per poll call "+
			"in TRACE_NET %s line:\n%s", reason, line)
	}
	if values["io_poll_waiters_max"] < values["io_poll_waiters_last"] {
		t.Fatalf("poll waiter max must be at least the last registry-row snapshot size "+
			"in TRACE_NET %s line:\n%s", reason, line)
	}
}

func runtimeV2NetTraceValues(t *testing.T, stderr string, reason string) (map[string]uint64, string) {
	t.Helper()
	return runtimeV2TraceValuesWithPrefix(t, stderr, "TRACE_NET", reason)
}

func runtimeV2NetShardTraceValues(t *testing.T, stderr string, reason string) (map[string]uint64, string) {
	t.Helper()
	return runtimeV2TraceValuesWithPrefix(t, stderr, "TRACE_NET_SHARDS", reason)
}

func runtimeV2TraceValuesWithPrefix(t *testing.T, stderr string, record string, reason string) (map[string]uint64, string) {
	t.Helper()
	prefix := record + " "
	if reason != "" {
		prefix += "reason=" + reason + " "
	}
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		values := make(map[string]uint64)
		for _, field := range strings.Fields(line) {
			name, raw, ok := strings.Cut(field, "=")
			if !ok || name == "reason" {
				continue
			}
			value, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				t.Fatalf("parse %s field %q in line:\n%s", record, field, line)
			}
			values[name] = value
		}
		return values, line
	}
	t.Fatalf("missing %s reason=%s in stderr:\n%s", record, reason, stderr)
	return nil, ""
}
