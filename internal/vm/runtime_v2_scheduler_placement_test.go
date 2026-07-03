//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestRuntimeV2SchedulerPlacementWorkerShape(t *testing.T) {
	binPath := buildRuntimeV2SchedulerPlacementHarness(t)
	cases := []struct {
		name string
		env  []string
	}{
		{
			name: "threads-equal-shards",
			env:  schedulerPlacementEnv("SURGE_SHARDS=4", "SURGE_THREADS=4", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "threads-unset",
			env:  schedulerPlacementEnv("SURGE_SHARDS=4", "SURGE_THREADS=", "SURGE_BLOCKING_THREADS=1"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, exitCode := runSchedulerPlacementHarness(t, binPath, "worker-shape", tc.env)
			if exitCode != 0 {
				t.Fatalf("worker shape harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					exitCode, stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2SchedulerPlacementNoStealPolicy(t *testing.T) {
	binPath := buildRuntimeV2SchedulerPlacementHarness(t)
	env := schedulerPlacementEnv("SURGE_SCHED_TRACE=1")
	stdout, stderr, exitCode := runSchedulerPlacementHarness(t, binPath, "no-steal", env)
	if exitCode != 0 {
		t.Fatalf("no-steal harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	trace := parseSchedTrace(t, stderr)
	if trace.steal != 0 {
		t.Fatalf("denied no-steal decision must not be reported as a successful steal; steal=%d\nstderr:\n%s",
			trace.steal, stderr)
	}
	if trace.tier1StealDenied == 0 {
		t.Fatalf("expected denied Tier 1 steal evidence in SCHED_TRACE\nstderr:\n%s", stderr)
	}
}

func TestRuntimeV2SchedulerPlacementNoStealWorkerPath(t *testing.T) {
	binPath := buildRuntimeV2SchedulerPlacementHarness(t)
	env := schedulerPlacementEnv(
		"SURGE_SHARDS=2",
		"SURGE_THREADS=2",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SCHED_TRACE=1",
	)
	stdout, stderr, exitCode := runSchedulerPlacementHarness(t, binPath, "no-steal-worker-path", env)
	if exitCode != 0 {
		t.Fatalf("no-steal worker-path harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	trace := parseSchedTrace(t, stderr)
	if trace.steal != 0 {
		t.Fatalf("connection-owned shard task was stolen across shard boundary; SCHED_TRACE steal=%d\nstderr:\n%s",
			trace.steal, stderr)
	}
	if trace.connOwnerLocal == 0 {
		t.Fatalf("expected connection-owned task to run on its owner shard in SCHED_TRACE\nstderr:\n%s", stderr)
	}
	if trace.connOwnerMismatch != 0 {
		t.Fatalf("connection-owned task ran on a non-owner shard; conn_owner_mismatch=%d\nstderr:\n%s",
			trace.connOwnerMismatch, stderr)
	}
}

func TestRuntimeV2SchedulerPlacementInvalidOwnerFailsClosed(t *testing.T) {
	binPath := buildRuntimeV2SchedulerPlacementHarness(t)
	env := schedulerPlacementEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runSchedulerPlacementHarness(t, binPath, "invalid-owner", env)
	if exitCode == 0 {
		t.Fatalf("invalid-owner harness unexpectedly passed\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "invalid task owner shard") {
		t.Fatalf("missing invalid owner panic\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestRuntimeV2SchedulerPlacementParkedWithWorkInvariant(t *testing.T) {
	binPath := buildRuntimeV2SchedulerPlacementHarness(t)
	env := schedulerPlacementEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runSchedulerPlacementHarness(t, binPath, "parked-clean", env)
	if exitCode != 0 {
		t.Fatalf("parked-clean harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}

	stdout, stderr, exitCode = runSchedulerPlacementHarness(t, binPath, "parked-violation", env)
	if exitCode == 0 {
		t.Fatalf("parked-violation harness unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if !strings.Contains(stderr, "parked-with-work invariant violated") {
		t.Fatalf("missing parked-with-work panic\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func buildRuntimeV2SchedulerPlacementHarness(t *testing.T) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping native runtime scheduler placement test")
	}

	root := repoRoot(t)
	tmpDir := t.TempDir()
	harnessPath := filepath.Join(tmpDir, "scheduler_placement_harness.c")
	binPath := filepath.Join(tmpDir, "scheduler_placement_harness")
	if writeErr := os.WriteFile(harnessPath, []byte(schedulerPlacementHarness), 0o600); writeErr != nil {
		t.Fatalf("write harness: %v", writeErr)
	}

	sources, globErr := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if globErr != nil {
		t.Fatalf("glob runtime sources: %v", globErr)
	}
	sort.Strings(sources)

	args := []string{
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Werror",
		"-pthread",
		"-I" + filepath.Join(root, "runtime", "native"),
		"-o",
		binPath,
		harnessPath,
	}
	for _, src := range sources {
		if filepath.Base(src) == "rt_entry.c" {
			continue
		}
		args = append(args, src)
	}

	buildCmd := exec.Command(clang, args...)
	buildCmd.Dir = root
	buildOut, buildErr, buildCode := runCommand(t, buildCmd, "")
	if buildCode != 0 {
		t.Fatalf("build harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			buildCode, buildOut, buildErr)
	}
	return binPath
}

func runSchedulerPlacementHarness(
	t *testing.T,
	binPath string,
	mode string,
	env []string,
) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binPath, mode)
	cmd.Env = env
	return runCommand(t, cmd, "")
}

func schedulerPlacementEnv(values ...string) []string {
	env := os.Environ()
	for _, value := range values {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) != 2 {
			continue
		}
		env = overrideEnvVar(env, parts[0], parts[1])
	}
	return env
}

const schedulerPlacementHarness = `
#define _POSIX_C_SOURCE 199309L
#include "rt_async_internal.h"

#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

	int rt_argc = 0;
	char** rt_argv_raw = NULL;

	enum {
	    POLL_KIND_GENERIC = 0,
	    POLL_KIND_GATE = 1001,
	    POLL_KIND_TARGET = 1002
	};

	static _Atomic uint32_t gate_started;
	static _Atomic uint32_t release_gate;
	static _Atomic uint32_t target_polled;
	static _Atomic uint32_t wrong_shard_poll;

	static void sleep_us(unsigned long micros) {
	    struct timespec ts;
	    ts.tv_sec = (time_t)(micros / 1000000UL);
	    ts.tv_nsec = (long)((micros % 1000000UL) * 1000UL);
	    while (nanosleep(&ts, &ts) != 0) {
	    }
	}

	void __surge_poll_call(uint64_t id) {
	    if (id == POLL_KIND_GATE) {
	        atomic_store_explicit(&gate_started, 1, memory_order_release);
	        while (atomic_load_explicit(&release_gate, memory_order_acquire) == 0) {
	            sleep_us(1000);
	        }
	        rt_async_return(NULL, 0);
	    }
	    if (id == POLL_KIND_TARGET) {
	        const rt_task* task = rt_current_task();
	        uint32_t worker_shard = rt_debug_current_worker_shard_id();
	        if (task == NULL || worker_shard != task->owner_shard_id) {
	            atomic_store_explicit(&wrong_shard_poll, 1, memory_order_release);
	        }
	        atomic_fetch_add_explicit(&target_polled, 1, memory_order_acq_rel);
	        rt_async_return(NULL, 0);
	    }
	    rt_async_return(NULL, 0);
	}

uint64_t __surge_blocking_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
    return 0;
}

static int fail(const char* msg) {
    if (msg != NULL) {
        fputs(msg, stderr);
        fputc('\n', stderr);
    }
    return 1;
}

	static int wait_for_u32(_Atomic uint32_t* value, uint32_t want, uint32_t attempts) {
	    for (uint32_t i = 0; i < attempts; i++) {
	        if (atomic_load_explicit(value, memory_order_acquire) >= want) {
	            return 1;
	        }
	        sleep_us(1000);
	    }
	    return 0;
	}

	static rt_task* alloc_ready_task(rt_executor* ex, int64_t poll_fn_id) {
	    uint64_t id = ex->next_id++;
	    ensure_task_cap(ex, id);
	    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        return NULL;
    }
	    memset(task, 0, sizeof(*task));
	    task->id = id;
	    task->poll_fn_id = poll_fn_id;
	    task->kind = TASK_KIND_USER;
	    task_status_store(task, TASK_READY);
    task_cancelled_store(task, 0);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    ex->tasks[id] = task;
    return task;
}

static int worker_shape(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return fail("missing executor");
    }
    rt_lock(ex);
    rt_runtime* runtime = rt_executor_runtime(ex);
    if (rt_runtime_shard_count(runtime) != 4) {
        rt_unlock(ex);
        return fail("runtime shard count mismatch");
    }
    if (rt_worker_count() != 4) {
        rt_unlock(ex);
        return fail("total worker count mismatch");
    }
    rt_heap_accounting* accounting = rt_executor_heap_accounting(ex);
    if (accounting == NULL || accounting->worker_cell_count != 4) {
        rt_unlock(ex);
        return fail("worker heap cell count mismatch");
    }
    for (size_t i = 0; i < 4; i++) {
        rt_shard* shard = rt_runtime_shard(runtime, i);
        rt_scheduler* scheduler = rt_shard_scheduler(shard);
        if (shard == NULL || scheduler == NULL || scheduler->worker_count != 1 ||
            scheduler->worker_ctxs == NULL) {
            rt_unlock(ex);
            return fail("missing shard worker context");
        }
        if (!rt_debug_validate_worker_ctx(ex, (uint32_t)i, 0, (uint32_t)i)) {
            rt_unlock(ex);
            return fail("worker context metadata mismatch");
        }
    }
    rt_unlock(ex);
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int no_steal(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return fail("missing executor");
    }
    rt_task task;
    memset(&task, 0, sizeof(task));
    task.kind = TASK_KIND_USER;
    if (!rt_task_can_steal_from_shard(&task, 0)) {
        return fail("generic unowned task should be stealable");
    }
    rt_task_set_placement(&task, 1, TASK_PLACEMENT_CONNECTION);
    if (rt_task_can_steal_from_shard_or_trace_denied(&task, 0)) {
        return fail("connection-owned task escaped owner shard");
    }
    if (!rt_task_can_steal_from_shard(&task, 1)) {
        return fail("connection-owned task should be stealable by owner shard");
    }
    rt_task_set_placement(&task, 1, TASK_PLACEMENT_GENERIC);
    if (!rt_task_can_steal_from_shard(&task, 0)) {
        return fail("generic owned task should preserve compatibility stealing");
    }
    rt_sched_trace_dump();
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

	static int no_steal_worker_path(void) {
	    rt_executor* ex = ensure_exec();
	    if (ex == NULL) {
	        return fail("missing executor");
	    }
	    atomic_store_explicit(&gate_started, 0, memory_order_relaxed);
	    atomic_store_explicit(&release_gate, 0, memory_order_relaxed);
	    atomic_store_explicit(&target_polled, 0, memory_order_relaxed);
	    atomic_store_explicit(&wrong_shard_poll, 0, memory_order_relaxed);

	    rt_lock(ex);
	    rt_task* gate = alloc_ready_task(ex, POLL_KIND_GATE);
	    if (gate == NULL) {
	        rt_unlock(ex);
	        return fail("gate task allocation failed");
	    }
	    rt_task_set_placement(gate, 1, TASK_PLACEMENT_CONNECTION);
	    ready_push(ex, gate->id);
	    rt_unlock(ex);

	    if (!wait_for_u32(&gate_started, 1, 1000)) {
	        (void)rt_executor_request_shutdown(ex);
	        return fail("gate task did not start");
	    }

	    rt_lock(ex);
	    rt_task* target = alloc_ready_task(ex, POLL_KIND_TARGET);
	    if (target == NULL) {
	        rt_unlock(ex);
	        (void)rt_executor_request_shutdown(ex);
	        return fail("target task allocation failed");
	    }
	    rt_task_set_placement(target, 1, TASK_PLACEMENT_CONNECTION);
	    ready_push(ex, target->id);
	    rt_unlock(ex);

	    sleep_us(100000);
	    if (atomic_load_explicit(&target_polled, memory_order_acquire) != 0) {
	        (void)rt_executor_request_shutdown(ex);
	        return fail("target ran while owner shard worker was still busy");
	    }

	    atomic_store_explicit(&release_gate, 1, memory_order_release);
	    if (!wait_for_u32(&target_polled, 1, 1000)) {
	        (void)rt_executor_request_shutdown(ex);
	        return fail("target task did not run after owner shard was released");
	    }
	    if (atomic_load_explicit(&wrong_shard_poll, memory_order_acquire) != 0) {
	        (void)rt_executor_request_shutdown(ex);
	        return fail("connection-owned task ran on a non-owner shard");
	    }
	    rt_sched_trace_dump();
	    (void)rt_executor_request_shutdown(ex);
	    return 0;
	}

	static int invalid_owner(void) {
	    rt_executor* ex = ensure_exec();
	    if (ex == NULL) {
	        return fail("missing executor");
	    }
	    rt_lock(ex);
	    rt_task* task = alloc_ready_task(ex, POLL_KIND_GENERIC);
	    if (task == NULL) {
	        rt_unlock(ex);
	        return fail("task allocation failed");
	    }
	    rt_task_set_placement(task, 99, TASK_PLACEMENT_CONNECTION);
	    ready_push(ex, task->id);
	    rt_unlock(ex);
	    return fail("invalid owner placement did not fail closed");
	}

	static int parked_clean(void) {
	    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return fail("missing executor");
    }
    rt_lock(ex);
    rt_debug_assert_no_parked_with_work(ex, 1);
    rt_unlock(ex);
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int parked_violation(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return fail("missing executor");
	    }
	    rt_lock(ex);
	    rt_task* task = alloc_ready_task(ex, POLL_KIND_GENERIC);
	    if (task == NULL) {
	        rt_unlock(ex);
	        return fail("task allocation failed");
    }
    rt_task_set_placement(task, 1, TASK_PLACEMENT_CONNECTION);
    ready_push(ex, task->id);
    rt_debug_assert_no_parked_with_work(ex, 1);
    rt_unlock(ex);
    return fail("parked-with-work assertion did not fire");
}

int main(int argc, char** argv) {
    if (argc != 2) {
        return fail("usage: scheduler_placement_harness <mode>");
    }
    if (strcmp(argv[1], "worker-shape") == 0) {
        return worker_shape();
    }
	    if (strcmp(argv[1], "no-steal") == 0) {
	        return no_steal();
	    }
	    if (strcmp(argv[1], "no-steal-worker-path") == 0) {
	        return no_steal_worker_path();
	    }
	    if (strcmp(argv[1], "invalid-owner") == 0) {
	        return invalid_owner();
	    }
	    if (strcmp(argv[1], "parked-clean") == 0) {
	        return parked_clean();
    }
    if (strcmp(argv[1], "parked-violation") == 0) {
        return parked_violation();
    }
    return fail("unknown mode");
}
`
