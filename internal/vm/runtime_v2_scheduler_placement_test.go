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
			name: "single-shard-has-one-carrier",
			env:  schedulerPlacementEnv("SURGE_SHARDS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "one-carrier-per-shard",
			env:  schedulerPlacementEnv("SURGE_SHARDS=4", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "default-is-one-carrier-per-detected-shard",
			env:  schedulerPlacementEnv("SURGE_SHARDS=", "SURGE_BLOCKING_THREADS=1"),
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

func TestRuntimeV2SchedulerPlacementInvalidOwnerFailsClosed(t *testing.T) {
	binPath := buildRuntimeV2SchedulerPlacementHarness(t)
	env := schedulerPlacementEnv("SURGE_SHARDS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runSchedulerPlacementHarness(t, binPath, "invalid-owner", env)
	if exitCode == 0 {
		t.Fatalf("invalid-owner harness unexpectedly passed\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "invalid task owner shard") {
		t.Fatalf("missing invalid owner panic\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestRuntimeV2SchedulerPlacementMissingOwnerFailsClosed(t *testing.T) {
	binPath := buildRuntimeV2SchedulerPlacementHarness(t)
	env := schedulerPlacementEnv("SURGE_SHARDS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runSchedulerPlacementHarness(t, binPath, "missing-owner", env)
	if exitCode == 0 {
		t.Fatalf("missing-owner harness unexpectedly passed\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "task has no owner shard") {
		t.Fatalf("missing unplaced task panic\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestRuntimeV2SchedulerPlacementMissingScopeOwnerFailsClosed(t *testing.T) {
	binPath := buildRuntimeV2SchedulerPlacementHarness(t)
	env := schedulerPlacementEnv("SURGE_SHARDS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runSchedulerPlacementHarness(t, binPath, "missing-scope-owner", env)
	if exitCode == 0 {
		t.Fatalf("missing-scope-owner harness unexpectedly passed\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "scope has no owner shard") {
		t.Fatalf("missing scope owner panic not reported\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestRuntimeV2SchedulerPlacementInvalidScopeOwnerFailsClosed(t *testing.T) {
	binPath := buildRuntimeV2SchedulerPlacementHarness(t)
	env := schedulerPlacementEnv("SURGE_SHARDS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runSchedulerPlacementHarness(t, binPath, "invalid-scope-owner", env)
	if exitCode == 0 {
		t.Fatalf("invalid-scope-owner harness unexpectedly passed\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "invalid scope owner shard") {
		t.Fatalf("invalid scope owner panic not reported\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestRuntimeV2SchedulerPlacementParkedWithWorkInvariant(t *testing.T) {
	binPath := buildRuntimeV2SchedulerPlacementHarness(t)
	env := schedulerPlacementEnv("SURGE_SHARDS=2", "SURGE_BLOCKING_THREADS=1")
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

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

	int rt_argc = 0;
	char** rt_argv_raw = NULL;

	enum { POLL_KIND_GENERIC = 0 };

	// Drop-dispatch stub: no harness state struct carries a drop obligation
// (drop-fn id 0 never dispatches), so reaching this is a test bug.
void __surge_drop_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

void __surge_drop_result_call(uint64_t id, void* value) {
    (void)id;
    (void)value;
}

void __surge_drop_abandoned_state_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

void __surge_poll_call(uint64_t id) {
	    (void)id;
	    rt_async_return(NULL, &(uint64_t){0});
	}

void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    if (out_dst != NULL) {
            *(uint64_t*)out_dst = 0;
        }
        return;
}

static int fail(const char* msg) {
    if (msg != NULL) {
        fputs(msg, stderr);
        fputc('\n', stderr);
    }
    return 1;
}

	static rt_task* alloc_ready_task(rt_executor* ex, int64_t poll_fn_id) {
	    uint64_t id = ex->next_id++;
	    ensure_task_cap(ex, id);
	    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        return NULL;
    }
	    memset(task, 0, sizeof(*task));
	    // This stand's bodies answer with a machine word, which is exactly what
	    // the opaque-word descriptor describes.
	    (void)rt_value_cell_bind(&task->result, rt_channel_opaque_word_ops());
	    task->id = id;
	    task->poll_fn_id = poll_fn_id;
	    task->kind = TASK_KIND_USER;
	    task_status_store(task, TASK_READY);
    task_cancel_gate_init(task);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    rt_task_entitlements_init(&task->entitlements);
    rt_task_slot_store(ex, id, task);
    return task;
}

static int worker_shape(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return fail("missing executor");
    }
    rt_control_lock(ex);
    rt_runtime* runtime = rt_executor_runtime(ex);
    size_t shard_count = rt_runtime_shard_count(runtime);
    if (shard_count == 0 || shard_count > RT_RUNTIME_MAX_SHARDS) {
        rt_control_unlock(ex);
        return fail("runtime shard count mismatch");
    }
    if (rt_worker_count() != shard_count) {
        rt_control_unlock(ex);
        return fail("total worker count mismatch");
    }
    rt_heap_accounting* accounting = rt_executor_heap_accounting(ex);
    if (accounting == NULL || accounting->worker_cell_count != shard_count) {
        rt_control_unlock(ex);
        return fail("worker heap cell count mismatch");
    }
    for (size_t i = 0; i < shard_count; i++) {
        rt_shard* shard = rt_runtime_shard(runtime, i);
        rt_scheduler* scheduler = rt_shard_scheduler(shard);
        if (shard == NULL || scheduler == NULL) {
            rt_control_unlock(ex);
            return fail("missing shard worker context");
        }
        if (scheduler->carrier_ctx == NULL) {
            rt_control_unlock(ex);
            return fail("missing shard worker context");
        }
        if (!rt_debug_validate_carrier_ctx(ex, (uint32_t)i, (uint32_t)i)) {
            rt_control_unlock(ex);
            return fail("carrier context metadata mismatch");
        }
    }
    rt_control_unlock(ex);
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

	static int invalid_owner(void) {
	    rt_executor* ex = ensure_exec();
	    if (ex == NULL) {
	        return fail("missing executor");
	    }
	    rt_control_lock(ex);
	    rt_task* task = alloc_ready_task(ex, POLL_KIND_GENERIC);
	    if (task == NULL) {
	        rt_control_unlock(ex);
	        return fail("task allocation failed");
	    }
	    rt_task_set_placement(task, 99, TASK_PLACEMENT_CONNECTION);
	    ready_push(ex, task->id);
	    rt_control_unlock(ex);
	    return fail("invalid owner placement did not fail closed");
	}

	static int missing_owner(void) {
	    rt_executor* ex = ensure_exec();
	    if (ex == NULL) {
	        return fail("missing executor");
	    }
	    rt_control_lock(ex);
	    rt_task* task = alloc_ready_task(ex, POLL_KIND_GENERIC);
	    if (task == NULL) {
	        rt_control_unlock(ex);
	        return fail("task allocation failed");
	    }
	    ready_push(ex, task->id);
	    rt_control_unlock(ex);
	    return fail("missing owner placement did not fail closed");
	}

	static int missing_scope_owner(void) {
	    rt_executor* ex = ensure_exec();
	    if (ex == NULL) {
	        return fail("missing executor");
	    }
	    (void)rt_scope_owner_shard(ex, NULL);
	    return fail("missing scope owner did not fail closed");
	}

	static int invalid_scope_owner(void) {
	    rt_executor* ex = ensure_exec();
	    if (ex == NULL) {
	        return fail("missing executor");
	    }
	    rt_scope scope;
	    memset(&scope, 0, sizeof(scope));
	    scope.owner_shard_id = 99;
	    (void)rt_scope_owner_shard(ex, &scope);
	    return fail("invalid scope owner did not fail closed");
	}

	static int parked_clean(void) {
	    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return fail("missing executor");
    }
    rt_control_lock(ex);
    rt_debug_assert_no_parked_with_work(ex, 1);
    rt_control_unlock(ex);
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int parked_violation(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return fail("missing executor");
	    }
	    rt_control_lock(ex);
	    rt_task* task = alloc_ready_task(ex, POLL_KIND_GENERIC);
	    if (task == NULL) {
	        rt_control_unlock(ex);
	        return fail("task allocation failed");
    }
    rt_task_set_placement(task, 1, TASK_PLACEMENT_CONNECTION);
    ready_push(ex, task->id);
    rt_debug_assert_no_parked_with_work(ex, 1);
    rt_control_unlock(ex);
    return fail("parked-with-work assertion did not fire");
}

int main(int argc, char** argv) {
    if (argc != 2) {
        return fail("usage: scheduler_placement_harness <mode>");
    }
    if (strcmp(argv[1], "worker-shape") == 0) {
        return worker_shape();
    }
	    if (strcmp(argv[1], "invalid-owner") == 0) {
	        return invalid_owner();
	    }
	    if (strcmp(argv[1], "missing-owner") == 0) {
	        return missing_owner();
	    }
	    if (strcmp(argv[1], "missing-scope-owner") == 0) {
	        return missing_scope_owner();
	    }
	    if (strcmp(argv[1], "invalid-scope-owner") == 0) {
	        return invalid_scope_owner();
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
