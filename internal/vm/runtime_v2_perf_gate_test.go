//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// performance CI gate. This is the per-commit half of the performance contract.
// Performance Contract: a deterministic trace-counter gate on a fixed net
// workload, wired into `make runtime-v2-perf-check` (and thus
// `make runtime-v2-check`). It replaces the pre-F2, wall-clock scaling judgment
// with counter thresholds that do not depend on host load or core count.
//
// Why trace counters, not wall-clock:
//   - The closeout put the 8-shard/1024 row at control_lock_acquired ~=
//     26.4 per request; moved task create/join/completion and same-owner
//     scope bookkeeping off the control lane. Control-lock acquisitions per
//     request are a LOGICAL property of request processing (how many times the
//     control lane is taken to serve N requests), independent of wall-clock time,
//     host load, or worker count. A wall-clock throughput/ratio gate is WSL2- and
//     CI-load-fragile and, post-F2, client-bound (the Python 1024-thread client
//     saturates ~8.5k req/s while server workers sit near 40% CPU), so it cannot
//     cleanly attribute a regression to the runtime.
//   - The tagged per-site lifecycle counters (ctrl_create/join_poll/completion/
//     scope/handle) are bit-stable across runs and across client patterns (
//     evidence, re-confirmed at HEAD: ctrl_scope/ctrl_handle/
//     ctrl_await_compat are identical to the count across five gate-workload runs).
//     The untagged RT_CTRL_SITE_OTHER residual (net/accept/io-drain/shutdown, not
//     migration surface) jitters with scheduling, so the precise gate is
//     the tagged lifecycle sum, with the contract-literal steady-state metric as a
//     generous backstop.
//
// Metric definitions for the recorded gate inputs:
//   - ctrl_await_compat is a HARNESS-STRUCTURAL artifact: every multi-worker Surge
//     program parks a root external awaiter (@entrypoint main awaiting serve_many)
//     for its whole lifetime, so ~1 completion per net-wrapper child serializes on
//     control as external-await compat (done_waiters>0), NOT worker steady-state
//     completion cost. It is reported as its own column and EXCLUDED from the
//     steady-state metric.
//   - steady-state-control = control_lock_acquired - ctrl_await_compat (
//     primary control-per-request metric).
//   - lifecycle-control = ctrl_create + ctrl_join_poll + ctrl_completion +
//     ctrl_scope + ctrl_handle (the tagged, bit-stable steady-path lifecycle
//     acquisitions; the precise regression detector for migrated surface).
//
// Fairness (F2, RV2-DEBT-015): placement_adoptions>0 proves join-consume placement
// adoption is firing, i.e. the pre-F2 placement funnel (all durable tasks pinned to
// shard 0) has not regressed; accept_owner_active_shards>=2 proves accepts are
// distributed, not funneled. These deterministic counters replace the host-fragile
// "8-shard total <= 1-shard total" wall-clock check for per-commit CI. The full
// sustained scaling / >=1s-stall acceptance stays a manual/nightly runbook
// (scripts/run_stallrepro.sh + scripts/cpu_validate.sh + the full 8x1024 matrix),
// documented in 08-tasks/12-performance-benchmark-and-ci-gate.md.
//
// Thresholds are ceilings set with headroom above the measured HEAD (8c89f358)
// gate-workload values so a real regression toward the 26.4/req baseline fails
// while deterministic-counter noise and core-count variance do not flake. Measured
// at HEAD on the reference host (8 shards x 128 conns x 8 req = 1024 requests):
// lifecycle-control ~= 6.25/req (bit-stable), steady-state-control ~= 12.0/req
// (<=1.5% run jitter under a fixed client), placement_adoptions ~= 253,
// accept_owner_active_shards = 8. See 08-evidence.md for the full record.
const (
	perfGateShards       = 8
	perfGateConnections  = 128
	perfGateRequests     = 8
	perfGateTotalRequest = perfGateConnections * perfGateRequests

	// Ceiling on the tagged steady-path lifecycle control acquisitions per
	// request (bit-stable; measured ~6.25/req). A regression that reintroduces
	// control-lane traffic on task create/join/completion/scope/handle lifts this
	// immediately. Well below the 26.4/req baseline.
	maxLifecycleControlPerReq = 9.0

	// Ceiling on the contract-literal steady-state control per request
	// (control_lock_acquired - ctrl_await_compat; measured ~12.0/req under this
	// gate's client). Generous headroom absorbs the untagged OTHER residual's
	// scheduling/core-count jitter while still failing on a regression toward the
	// 26.4/req baseline.
	maxSteadyStateControlPerReq = 20.0
)

// perfGateServerSource is a direct-mode request/reply server faithful to
// benchmarks/native/net_request_reply (serve_many -> @local spawn serve_conn ->
// serve_direct with net.read_some/net.write_all), trimmed to the direct path. It
// drives the same control-lane sites as the net benchmark: the per-connection
// serve_conn scope (ctrl_scope), the net-wrapper child completions/frees
// (ctrl_handle), the join-consume placement adoption (placement_adoptions), and
// the @entrypoint main external await of serve_many (ctrl_await_compat).
const perfGateServerSource = `import stdlib/net as net;

fn append_string_bytes(out: &mut byte[], s: &string) -> nothing {
    let view = s.bytes();
    let length: int = view.__len() to int;
    let mut i: int = 0;
    while i < length {
        out.push(view[i]);
        i = i + 1;
    }
    return nothing;
}

fn fixed_response() -> byte[] {
    let mut out: byte[] = [];
    append_string_bytes(&mut out, "VALUE {\"v\":\"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\"}\n");
    return out;
}

async fn write_owned(conn: &TcpConn, data: byte[]) -> bool {
    let write_res = net.write_all(conn, data).await();
    compare write_res {
        Success(net_res) => {
            compare net_res {
                Success(_) => { return true; }
                err => { print("write_err=" + (err.code to string)); return false; }
            };
        }
        Cancelled() => { print("write_cancelled"); return false; }
    };
    return false;
}

fn bool_result_ok(res: TaskResult<bool>) -> bool {
    compare res {
        Success(ok) => { return ok; }
        Cancelled() => { return false; }
    };
    return false;
}

fn int_result_code(res: TaskResult<int>) -> int {
    compare res {
        Success(code) => { return code; }
        Cancelled() => { return 1; }
    };
    return 1;
}

async fn serve_direct(conn: TcpConn) -> int {
    while true {
        let read_res = net.read_some(&conn, 4096:uint).await();
        compare read_res {
            Success(net_res) => {
                compare net_res {
                    Success(bytes) => {
                        if bytes.__len() == 0:uint {
                            let _ = net.close_conn(own conn);
                            return 0;
                        }
                        let length: int = bytes.__len() to int;
                        let mut i: int = 0;
                        while i < length {
                            if !bool_result_ok(write_owned(&conn, fixed_response()).await()) {
                                let _ = net.close_conn(own conn);
                                return 1;
                            }
                            i = i + 1;
                        }
                    }
                    err => {
                        print("read_err=" + (err.code to string));
                        let _ = net.close_conn(own conn);
                        return 1;
                    }
                };
            }
            Cancelled() => {
                print("read_cancelled");
                let _ = net.close_conn(own conn);
                return 1;
            }
        };
    }
    return 0;
}

async fn serve_many(listener: TcpListener, connections: uint) -> int {
    let mut tasks: Task<int>[] = [];
    let mut accepted: uint = 0:uint;
    while accepted < connections {
        let accept_res = net.accept(&listener).await();
        compare accept_res {
            Success(net_res) => {
                compare net_res {
                    Success(conn) => {
                        let task: Task<int> = @local spawn serve_direct(conn);
                        tasks.push(task);
                    }
                    err => {
                        print("accept_err=" + (err.code to string));
                        let _ = net.close_listener(own listener);
                        return 1;
                    }
                };
            }
            Cancelled() => {
                print("accept_cancelled");
                let _ = net.close_listener(own listener);
                return 1;
            }
        };
        accepted = accepted + 1:uint;
    }

    let mut result: int = 0;
    while tasks.__len() != 0:uint {
        let task = tasks.pop().safe();
        let code = int_result_code(task.await());
        if code != 0 && result == 0 {
            result = code;
        }
    }
    let _ = net.close_listener(own listener);
    return result;
}

@entrypoint("argv")
fn main(port: uint, connections: uint = 1:uint) -> int {
    let listen_res = net.listen("127.0.0.1", port);
    compare listen_res {
        Success(listener) => {
            print("ready");
            return int_result_code(serve_many(listener, connections).await());
        }
        err => {
            print("listen_err=" + (err.code to string));
            return 1;
        }
    };
    return 1;
}
`

// TestRuntimeV2PerfControlLaneGate is the per-commit performance
// gate. It runs a fixed 8-shard net workload, drives it to completion, and asserts
// the deterministic control-lane and fairness counters against the performance contract.
// Performance Contract. Wall-clock timing is deliberately NOT asserted.
func TestRuntimeV2PerfControlLaneGate(t *testing.T) {
	ensureLLVMToolchain(t)

	port := pickFreePort(t)
	env := runtimeV2AcceptEnv(t)
	env = overrideEnvVar(env, "SURGE_SHARDS", strconv.Itoa(perfGateShards))
	env = overrideEnvVar(env, "SURGE_THREADS", strconv.Itoa(perfGateShards))
	env = overrideEnvVar(env, "SURGE_TRACE_EXEC", "1")
	env = overrideEnvVar(env, "SURGE_SCHED_TRACE", "1")

	srv := startRuntimeV2AcceptServer(
		t,
		perfGateServerSource,
		env,
		strconv.Itoa(port),
		strconv.Itoa(perfGateConnections),
	)

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if err := drivePerfGateClients(addr); err != nil {
		srv.fail("perf gate client: %v", err)
	}

	// serve_many completes and the process exits with its TRACE_EXEC dump once all
	// connections are closed by the client above.
	_, stderr := srv.waitExitZero(60 * time.Second)

	values, line := runtimeV2TraceValuesWithPrefix(t, stderr, "TRACE_EXEC", "exit")
	netValues, netLine := runtimeV2TraceValuesWithPrefix(t, stderr, "TRACE_NET", "exit")

	requireField := func(vals map[string]uint64, field, src, ln string) uint64 {
		v, ok := vals[field]
		if !ok {
			t.Fatalf("missing %s in %s exit line:\n%s", field, src, ln)
		}
		return v
	}

	control := requireField(values, "control_lock_acquired", "TRACE_EXEC", line)
	awaitCompat := requireField(values, "ctrl_await_compat", "TRACE_EXEC", line)
	create := requireField(values, "ctrl_create", "TRACE_EXEC", line)
	joinPoll := requireField(values, "ctrl_join_poll", "TRACE_EXEC", line)
	completion := requireField(values, "ctrl_completion", "TRACE_EXEC", line)
	scope := requireField(values, "ctrl_scope", "TRACE_EXEC", line)
	handle := requireField(values, "ctrl_handle", "TRACE_EXEC", line)
	adoptions := requireField(values, "placement_adoptions", "TRACE_EXEC", line)
	activeAcceptShards := requireField(netValues, "accept_owner_active_shards", "TRACE_NET", netLine)

	reqs := float64(perfGateTotalRequest)

	// Attribution invariant: ctrl_await_compat cannot exceed the total; the tagged
	// sites partition a subset of control_lock_acquired.
	if awaitCompat > control {
		t.Fatalf("ctrl_await_compat %d exceeds control_lock_acquired %d:\n%s", awaitCompat, control, line)
	}

	lifecycleControl := create + joinPoll + completion + scope + handle
	lifecyclePerReq := float64(lifecycleControl) / reqs
	steadyStatePerReq := float64(control-awaitCompat) / reqs

	t.Logf("Performance gate (8 shards x %d conns x %d req = %d requests):",
		perfGateConnections, perfGateRequests, perfGateTotalRequest)
	t.Logf("  control_lock_acquired=%d (%.3f/req) ctrl_await_compat=%d (%.3f/req)",
		control, float64(control)/reqs, awaitCompat, float64(awaitCompat)/reqs)
	t.Logf("  steady-state-control=%d (%.3f/req, ceiling %.1f)",
		control-awaitCompat, steadyStatePerReq, maxSteadyStateControlPerReq)
	t.Logf("  lifecycle-control=%d (%.3f/req, ceiling %.1f) [create=%d join_poll=%d completion=%d scope=%d handle=%d]",
		lifecycleControl, lifecyclePerReq, maxLifecycleControlPerReq,
		create, joinPoll, completion, scope, handle)
	t.Logf("  placement_adoptions=%d accept_owner_active_shards=%d", adoptions, activeAcceptShards)

	// (1) Precise, bit-stable regression detector: tagged steady-path lifecycle
	// control per request. moved task create/join/completion and same-owner
	// scope bookkeeping off the control lane; a regression relifts these.
	if lifecyclePerReq > maxLifecycleControlPerReq {
		t.Fatalf("lifecycle control per request %.3f exceeds ceiling %.1f "+
			"(performance contract regression: task create/join/completion/scope/handle "+
			"control-lane traffic increased)\n%s",
			lifecyclePerReq, maxLifecycleControlPerReq, line)
	}

	// (2) Contract-literal backstop: steady-state control per request must stay
	// materially below the closeout baseline of ~26.4/req.
	if steadyStatePerReq > maxSteadyStateControlPerReq {
		t.Fatalf("steady-state control per request %.3f exceeds ceiling %.1f "+
			"(control_lock_acquired=%d - ctrl_await_compat=%d; baseline was ~26.4/req)\n%s",
			steadyStatePerReq, maxSteadyStateControlPerReq, control, awaitCompat, line)
	}

	// (3) Fairness (RV2-DEBT-015 / F2): join-consume placement adoption must be
	// firing, proving the pre-F2 placement funnel (all durable tasks on shard 0)
	// has not regressed.
	if adoptions == 0 {
		t.Fatalf("placement_adoptions=0: F2 placement adoption not firing, "+
			"the RV2-DEBT-015 placement funnel may have regressed\n%s", line)
	}

	// (4) Accepts must be distributed across shards, not funneled onto one.
	if activeAcceptShards < 2 {
		t.Fatalf("accept_owner_active_shards=%d: accepts funneled onto <2 shards\n%s",
			activeAcceptShards, netLine)
	}
}

// drivePerfGateClients opens perfGateConnections connections and issues
// perfGateRequests request/reply rounds on each, then closes them so the server's
// serve_many completes and the process exits with its trace dump.
func drivePerfGateClients(addr string) error {
	const response = "VALUE {\"v\":\"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\"}\n"
	respLen := len(response)

	conns := make([]net.Conn, 0, perfGateConnections)
	deadline := time.Now().Add(20 * time.Second)
	for i := range perfGateConnections {
		conn, err := dialWithRetry(addr, deadline)
		if err != nil {
			for _, c := range conns {
				_ = c.Close()
			}
			return fmt.Errorf("dial %d: %w", i, err)
		}
		conns = append(conns, conn)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, perfGateConnections)
	for i, conn := range conns {
		wg.Add(1)
		go func(id int, c net.Conn) {
			defer wg.Done()
			if err := c.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
				errCh <- fmt.Errorf("client %d deadline: %w", id, err)
				return
			}
			buf := make([]byte, respLen)
			for r := 0; r < perfGateRequests; r++ {
				if _, err := c.Write([]byte("x")); err != nil {
					errCh <- fmt.Errorf("client %d write %d: %w", id, r, err)
					return
				}
				if _, err := readFull(c, buf); err != nil {
					errCh <- fmt.Errorf("client %d read %d: %w", id, r, err)
					return
				}
				if string(buf) != response {
					errCh <- fmt.Errorf("client %d bad response %d: %q", id, r, string(buf))
					return
				}
			}
		}(i, conn)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			for _, c := range conns {
				_ = c.Close()
			}
			return err
		}
	}
	for _, c := range conns {
		_ = c.Close()
	}
	return nil
}

func readFull(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		if n > 0 {
			total += n
		}
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
