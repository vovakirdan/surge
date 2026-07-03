//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

const runtimeV2AcceptShardConfigProbeSource = `@entrypoint
fn main() -> int {
    let workers: uint = rt_worker_count();
    if workers == 0:uint {
        return 2;
    }
    return 0;
}
`

const runtimeV2AcceptBurstSource = `import stdlib/net as net;

fn pong() -> byte[] {
    let mut out: byte[] = [];
    out.push(80:byte);
    out.push(79:byte);
    out.push(78:byte);
    out.push(71:byte);
    out.push(10:byte);
    return out;
}

async fn serve_many(listener: TcpListener, total: uint) -> int {
    let mut accepted: uint = 0:uint;
    while accepted < total {
        let accept_task = net.accept(&listener).await();
        let code: int = compare accept_task {
            Success(conn_res) => compare conn_res {
                Success(conn) => {
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
                    let _ = net.close_conn(own conn);
                    0;
                };
                _ => 1;
            };
            Cancelled() => 1;
        };
        if code != 0 {
            return code;
        }
        accepted = accepted + 1:uint;
    }
    return 0;
}

@entrypoint("argv")
fn main(port: uint, clients: uint) -> int {
    let listen_res = net.listen("127.0.0.1", port);
    compare listen_res {
        Success(listener) => {
            let result = serve_many(listener, clients).await();
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

func runRuntimeV2AcceptConfigProbe(t *testing.T, env []string) runResult {
	t.Helper()
	outputPath := buildLLVMProgramFromSource(t, runtimeV2AcceptShardConfigProbeSource)
	_, res := runBinaryWithTimeout(t, outputPath, env, 10*time.Second)
	return res
}

func runRuntimeV2AcceptBurst(t *testing.T, shards int, clients int) string {
	t.Helper()
	port := pickFreePort(t)
	env := runtimeV2AcceptEnv(t)
	env = overrideEnvVar(env, "SURGE_SHARDS", strconv.Itoa(shards))
	env = overrideEnvVar(env, "SURGE_THREADS", strconv.Itoa(shards))
	env = overrideEnvVar(env, "SURGE_TRACE_EXEC", "1")
	srv := startRuntimeV2AcceptServer(
		t,
		runtimeV2AcceptBurstSource,
		env,
		strconv.Itoa(port),
		strconv.Itoa(clients),
	)

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	errCh := make(chan error, clients)
	for i := range clients {
		go func(id int) {
			conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
			if err != nil {
				errCh <- fmt.Errorf("client %d dial: %w", id, err)
				return
			}
			defer conn.Close()
			if err = conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
				errCh <- fmt.Errorf("client %d deadline: %w", id, err)
				return
			}
			if _, err = conn.Write([]byte("PING\n")); err != nil {
				errCh <- fmt.Errorf("client %d write: %w", id, err)
				return
			}
			var buf [5]byte
			if _, err = io.ReadFull(conn, buf[:]); err != nil {
				errCh <- fmt.Errorf("client %d read: %w", id, err)
				return
			}
			if string(buf[:]) != "PONG\n" {
				errCh <- fmt.Errorf("client %d response: %q", id, string(buf[:]))
				return
			}
			errCh <- nil
		}(i)
	}
	for range clients {
		if err := <-errCh; err != nil {
			srv.fail("accept burst failed: %v", err)
		}
	}
	_, stderr := srv.waitExitZero(20 * time.Second)
	return stderr
}

func requireRuntimeV2AcceptNetFieldAtLeast(
	t *testing.T,
	values map[string]uint64,
	line string,
	field string,
	min uint64,
	owner string,
) {
	t.Helper()
	got, ok := values[field]
	if !ok {
		t.Fatalf("missing %s in TRACE_NET exit line; %s must add this accept-ownership proof field:\n%s",
			field, owner, line)
	}
	if got < min {
		t.Fatalf("expected %s>=%d, got %d; %s owns this accept-ownership contract:\n%s",
			field, min, got, owner, line)
	}
}

func TestRuntimeV2AcceptShardConfigInitializesRequestedShardCount(t *testing.T) {
	ensureLLVMToolchain(t)

	env := runtimeV2AcceptEnv(t)
	env = overrideEnvVar(env, "SURGE_SHARDS", "4")
	env = overrideEnvVar(env, "SURGE_TRACE_EXEC", "1")
	res := runRuntimeV2AcceptConfigProbe(t, env)
	if res.exitCode != 0 {
		t.Fatalf("shard config probe failed before contract assertions (exit=%d)\nstdout:\n%s\nstderr:\n%s",
			res.exitCode, res.stdout, res.stderr)
	}
	values := parseExecTrace(t, res.stderr)
	got, ok := values["runtime_shards"]
	if !ok {
		t.Fatalf("missing runtime_shards in TRACE_EXEC; Task 6 defines shard_count and Task 12 exposes it\nstderr:\n%s",
			res.stderr)
	}
	if got != 4 {
		t.Fatalf("expected runtime_shards=4, got %d\nstderr:\n%s", got, res.stderr)
	}
}

func TestRuntimeV2AcceptRejectsInvalidShardConfig(t *testing.T) {
	ensureLLVMToolchain(t)

	for _, value := range []string{"0", "not-a-number", "999999"} {
		t.Run(value, func(t *testing.T) {
			env := overrideEnvVar(runtimeV2AcceptEnv(t), "SURGE_SHARDS", value)
			res := runRuntimeV2AcceptConfigProbe(t, env)
			if res.exitCode == 0 {
				t.Fatalf("expected explicit SURGE_SHARDS rejection for %q; Task 6 owns this diagnostic\nstdout:\n%s\nstderr:\n%s",
					value, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stderr, "SURGE_SHARDS") {
				t.Fatalf("expected SURGE_SHARDS in rejection diagnostic for %q\nstderr:\n%s",
					value, res.stderr)
			}
		})
	}
}

func TestRuntimeV2AcceptRejectsConflictingThreadCount(t *testing.T) {
	ensureLLVMToolchain(t)

	env := runtimeV2AcceptEnv(t)
	env = overrideEnvVar(env, "SURGE_SHARDS", "4")
	env = overrideEnvVar(env, "SURGE_THREADS", "2")
	res := runRuntimeV2AcceptConfigProbe(t, env)
	if res.exitCode == 0 {
		t.Fatalf("expected SURGE_SHARDS/SURGE_THREADS conflict rejection; Task 6/7 own this boundary\nstdout:\n%s\nstderr:\n%s",
			res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "SURGE_SHARDS") || !strings.Contains(res.stderr, "SURGE_THREADS") {
		t.Fatalf("expected both env names in conflict diagnostic\nstderr:\n%s", res.stderr)
	}
}

func TestRuntimeV2AcceptDistributionAcrossOwnerShards(t *testing.T) {
	ensureLLVMToolchain(t)

	stderr := runRuntimeV2AcceptBurst(t, 4, 64)
	values, line := runtimeV2NetTraceValues(t, stderr, "exit")
	requireRuntimeV2AcceptNetFieldAtLeast(
		t,
		values,
		line,
		"accept_owner_active_shards",
		2,
		"Task 9/12",
	)
}

func TestRuntimeV2AcceptOwnerShardLifecycleTraceContract(t *testing.T) {
	ensureLLVMToolchain(t)

	stderr := runRuntimeV2AcceptBurst(t, 4, 8)
	values, line := runtimeV2NetTraceValues(t, stderr, "exit")
	cases := []struct {
		name  string
		field string
		min   uint64
		owner string
	}{
		{
			name:  "readiness uses owner shard registry",
			field: "fd_owner_registry_rows",
			min:   1,
			owner: "Task 8/11/12",
		},
		{
			name:  "close wakes owner shard waiters",
			field: "close_owner_wakeups",
			min:   1,
			owner: "Task 8/11/12",
		},
		{
			name:  "cancellation cleanup has owner shard counter",
			field: "cancel_owner_cleanup",
			min:   0,
			owner: "Task 11/12",
		},
		{
			name:  "shutdown wakes every shard poller",
			field: "shutdown_poller_wakeups",
			min:   4,
			owner: "Task 10/11/12",
		},
		{
			name:  "non-owner connection use is visible",
			field: "non_owner_conn_denied",
			min:   0,
			owner: "Task 7/9/12",
		},
		{
			name:  "listener group close covers every member",
			field: "listener_group_members_closed",
			min:   4,
			owner: "Task 8/9/11/12",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireRuntimeV2AcceptNetFieldAtLeast(t, values, line, tc.field, tc.min, tc.owner)
		})
	}
}
