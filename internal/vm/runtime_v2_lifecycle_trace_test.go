//go:build runtime_v2_pending

package vm_test

import (
	"testing"
	"time"
)

// Epic 8 Task 5 trace-contract gate. Proves the per-site control-lock
// attribution counters (rt_ctrl_site, rt_async_trace.c) are wired into the
// TRACE_EXEC dump and increment on the lifecycle census paths. The two
// always-on census sites at baseline daeac51e — create (__task_create) and
// join-poll (rt_task_poll) — must be non-zero; every per-site field must be
// present so bench_native_net.sh and Tasks 6-10 can read per-request
// attribution. The sum of the six sites can never exceed the global
// control_lock_acquired total (attribution is a strict subset; the residual is
// the untagged RT_CTRL_SITE_OTHER acquisitions).
func TestRuntimeV2LifecycleTraceControlSiteContract(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `async fn work(x: int) -> int {
    checkpoint().await();
    return x + 1;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut i = 0;
        let mut total = 0;
        while i < 8 {
            let h = spawn work(i);
            let res = h.await();
            total = total + compare res {
                Success(v) => v;
                Cancelled() => 0;
            };
            i = i + 1;
        }
        let c = spawn work(100);
        c.cancel();
        let _ = c.await();
        print("ok", "\n");
        return total;
    }).await();

    return compare r {
        Success(_) => 0;
        Cancelled() => 99;
    };
}
`

	outputPath := buildLLVMProgramFromSource(t, source)
	env := overrideEnvVar(mtEnv(t), "SURGE_TRACE_EXEC", "1")
	_, res := runBinaryWithTimeout(t, outputPath, env, 20*time.Second)
	if res.exitCode != 0 {
		t.Fatalf("lifecycle trace program failed (exit=%d)\nstdout:\n%s\nstderr:\n%s",
			res.exitCode, res.stdout, res.stderr)
	}

	values, line := runtimeV2TraceValuesWithPrefix(t, res.stderr, "TRACE_EXEC", "exit")

	perSite := []string{
		"ctrl_create",
		"ctrl_join_poll",
		"ctrl_completion",
		"ctrl_scope",
		"ctrl_await_compat",
		"ctrl_handle",
	}
	for _, field := range perSite {
		if _, ok := values[field]; !ok {
			t.Fatalf("missing per-site field %s in TRACE_EXEC exit line:\n%s", field, line)
		}
	}
	if _, ok := values["control_lock_acquired"]; !ok {
		t.Fatalf("missing control_lock_acquired in TRACE_EXEC exit line:\n%s", line)
	}

	// ctrl_create is the one always-on census site: segment growth fires at
	// least once per process (the very first id allocated). ctrl_join_poll is
	// NOT asserted non-zero here since Epic 8 Task 7: rt_task_poll itself no
	// longer takes control at all, and this program spawns no
	// TASK_PLACEMENT_CONNECTION tasks, so the only remaining ctrl_join_poll
	// source (the F2 placement-adoption fallback, rt_task_poll_adopt_placement)
	// never fires here - ctrl_join_poll is genuinely 0 in this scenario, which
	// is the correct post-Task-7 behavior, not a missing wire-up.
	if values["ctrl_create"] == 0 {
		t.Fatalf("expected non-zero ctrl_create in TRACE_EXEC exit line:\n%s", line)
	}

	// Attribution invariant: the per-site counters partition a subset of the
	// global control_lock_acquired total; they can never over-count it.
	var siteSum uint64
	for _, field := range perSite {
		siteSum += values[field]
	}
	if siteSum > values["control_lock_acquired"] {
		t.Fatalf("per-site control-lock sum %d exceeds control_lock_acquired %d in line:\n%s",
			siteSum, values["control_lock_acquired"], line)
	}
}
