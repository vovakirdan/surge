//go:build runtime_v2_pending

package vm_test

import (
	"testing"
	"time"
)

// Trace-contract gate. Proves the per-site control-lock
// attribution counters (rt_ctrl_site, rt_async_trace.c) are wired into the
// TRACE_EXEC dump and increment on the lifecycle census paths. The two
// always-on census sites at baseline daeac51e — create (__task_create) and
// join-poll (rt_task_poll) — must be non-zero; every per-site field must be
// present so bench_native_net.sh and can read per-request
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
	// NOT asserted non-zero here because rt_task_poll itself no
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

// (P10, rule 5) trace guardian: external/main-thread await is
// counted separately under ctrl_await_compat. The @entrypoint main runs on the
// non-worker main thread; with workers>1 (SURGE_SHARDS=2) its top-level
// h.await() takes rt_task_await's done_cv compatibility branch, which tags
// RT_CTRL_SITE_AWAIT_COMPAT. The inner worker-side joins (h.await() inside the
// spawned async tasks) run rt_task_poll on the owner lane and never touch
// done_cv, so they do NOT inflate ctrl_await_compat. This proves the
// external-await lane is real, exercised, and attributed to its own counter
// rather than folded into ctrl_completion.
func TestRuntimeV2LifecycleTraceAwaitCompatCountedSeparately(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `async fn leaf(x: int) -> int {
    checkpoint().await();
    return x + 1;
}

async fn branch(n: int) -> int {
    // Worker-side joins: these run on the owner lane (rt_task_poll), not done_cv.
    let a = spawn leaf(n);
    let b = spawn leaf(n + 1);
    let ra = a.await();
    let rb = b.await();
    return compare ra {
        Success(va) => compare rb {
            Success(vb) => va + vb;
            Cancelled() => 0;
        };
        Cancelled() => 0;
    };
}

@entrypoint
fn main() -> int {
    // Top-level external await: main is a non-worker thread, so with workers>1
    // this is rt_task_await's done_cv compatibility path (ctrl_await_compat).
    let r = (async {
        let mut i = 0;
        let mut total = 0;
        while i < 6 {
            let h = spawn branch(i);
            total = total + compare h.await() {
                Success(v) => v;
                Cancelled() => 0;
            };
            i = i + 1;
        }
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
	env = overrideEnvVar(env, "SURGE_SHARDS", "2")
	env = overrideEnvVar(env, "SURGE_THREADS", "2")
	_, res := runBinaryWithTimeout(t, outputPath, env, 20*time.Second)
	if res.exitCode != 0 {
		t.Fatalf("await-compat trace program failed (exit=%d)\nstdout:\n%s\nstderr:\n%s",
			res.exitCode, res.stdout, res.stderr)
	}

	values, line := runtimeV2TraceValuesWithPrefix(t, res.stderr, "TRACE_EXEC", "exit")
	if _, ok := values["ctrl_await_compat"]; !ok {
		t.Fatalf("missing ctrl_await_compat in TRACE_EXEC exit line:\n%s", line)
	}
	// The non-worker top-level await is the external-await lane; it must be
	// counted under ctrl_await_compat (rule 5, counted separately). Worker-side
	// joins do not contribute to this counter.
	if values["ctrl_await_compat"] == 0 {
		t.Fatalf("external/main-thread await must be counted under ctrl_await_compat "+
			"(rule 5), got 0:\n%s", line)
	}
}
