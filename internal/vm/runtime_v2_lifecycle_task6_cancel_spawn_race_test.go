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

// Epic 8 Task 6 introduced a hazard: task_add_child (the parent's children[]
// append in __task_create) moved off the control lane onto the parent's own
// owner shard lock, but cancel_task's tree walk (rt_async_state.c) still
// runs entirely under control and, before this task's fix, read
// task->children_len/children[] directly. A concurrent rt_task_cancel on a
// RUNNING parent (legal - cancellation of a running task via handle is a
// supported case) could then race a realloc'ing children[] array. The fix
// (cancel_task now snapshots children ids under the task's own owner shard
// lock before recursing) is proven here by a dedicated build-and-run
// harness that spawns many parents which each spawn children in a tight
// loop while a separate, non-worker pthread concurrently calls
// rt_task_cancel on them. This file is intentionally self-contained (does
// not reuse or edit the Task 4 lifecycleHarness* files).

func buildRuntimeV2CancelSpawnRaceHarness(t *testing.T, name string, extraFlags []string) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping cancel/spawn race harness")
	}

	root := repoRoot(t)
	tmpDir := t.TempDir()
	harnessPath := filepath.Join(tmpDir, name+".c")
	binPath := filepath.Join(tmpDir, name)
	if writeErr := os.WriteFile(harnessPath, []byte(cancelSpawnRaceHarnessSource), 0o600); writeErr != nil {
		t.Fatalf("write harness: %v", writeErr)
	}

	sources, globErr := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if globErr != nil {
		t.Fatalf("glob runtime sources: %v", globErr)
	}
	sort.Strings(sources)

	args := []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-pthread"}
	args = append(args, extraFlags...)
	args = append(args, "-I"+filepath.Join(root, "runtime", "native"), "-o", binPath, harnessPath)
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
		if strings.Contains(buildErr, "unsupported option") || strings.Contains(buildErr, "-fsanitize=thread") {
			t.Skipf("clang build does not support the requested flags; skipping\nstderr:\n%s", buildErr)
		}
		t.Fatalf("build cancel/spawn race harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			buildCode, buildOut, buildErr)
	}
	return binPath
}

// TestRuntimeV2LifecycleCancelSpawnChildrenRace is the required, enumerated
// gate (runtime-v2-lifecycle-check): a deterministic, non-TSan run that
// must complete with no panic/abort and no hang, proving the collect-then-
// recurse fix in cancel_task does not deadlock, drop children, or corrupt
// the array under concurrent cancellation.
func TestRuntimeV2LifecycleCancelSpawnChildrenRace(t *testing.T) {
	binPath := buildRuntimeV2CancelSpawnRaceHarness(t, "cancel_spawn_race", nil)
	env := overrideEnvVar(os.Environ(), "SURGE_SHARDS", "4")
	env = overrideEnvVar(env, "SURGE_THREADS", "4")
	env = overrideEnvVar(env, "SURGE_BLOCKING_THREADS", "1")
	cmd := exec.Command(binPath)
	cmd.Env = env
	stdout, stderr, exitCode := runCommand(t, cmd, "")
	if exitCode != 0 {
		t.Fatalf("cancel/spawn children race failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

// TestRuntimeV2LifecycleCancelSpawnChildrenRaceTSan is the TSan oracle for
// the same scenario (mirrors the Task 4 completion-pin-stress methodology).
// Not part of the enumerated runtime-v2-lifecycle-check regex: TSan builds
// are slower and best-effort here (skips, not fails, if unsupported), but
// this is the strongest available proof that the fix closes a genuine data
// race rather than just avoiding an observed crash.
func TestRuntimeV2LifecycleCancelSpawnChildrenRaceTSan(t *testing.T) {
	binPath := buildRuntimeV2CancelSpawnRaceHarness(
		t, "cancel_spawn_race_tsan", []string{"-fsanitize=thread", "-g", "-O1"})
	env := overrideEnvVar(os.Environ(), "SURGE_SHARDS", "4")
	env = overrideEnvVar(env, "SURGE_THREADS", "4")
	env = overrideEnvVar(env, "SURGE_BLOCKING_THREADS", "1")
	cmd := exec.Command(binPath)
	cmd.Env = env
	stdout, stderr, exitCode := runCommand(t, cmd, "")
	if exitCode != 0 {
		t.Fatalf("cancel/spawn children race (TSan) failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

const cancelSpawnRaceHarnessSource = `
#define _POSIX_C_SOURCE 199309L
#include "rt_async_internal.h"

#include <pthread.h>
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

enum { POLL_RACE_PARENT = 9101, POLL_RACE_CHILD = 9102 };
enum { RACE_PARENT_COUNT = 64, RACE_CHILDREN_PER_PARENT = 24 };

// Required by other linked runtime/native sources (rt_io.c, rt_async_blocking.c)
// that this harness does not otherwise exercise.
int rt_argc = 0;
char** rt_argv_raw = NULL;

uint64_t __surge_blocking_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
    return 0;
}

static _Atomic(void*) g_race_parents[RACE_PARENT_COUNT];
static _Atomic uint32_t g_race_children_total;

typedef struct {
    uint32_t spawned;
} race_parent_state;

static int fail(const char* msg) {
    if (msg != NULL) {
        fputs(msg, stderr);
        fputc('\n', stderr);
    }
    return 1;
}

static void sleep_us(unsigned long micros) {
    struct timespec ts;
    ts.tv_sec = (time_t)(micros / 1000000UL);
    ts.tv_nsec = (long)((micros % 1000000UL) * 1000UL);
    while (nanosleep(&ts, &ts) != 0) {
    }
}

static int wait_task_status(const rt_task* task, uint8_t want, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (task_status_load(task) == want) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

// race_alloc_ready_task / race_spawn_top_level are a small, deliberately
// duplicated copy of the shared harness's alloc_ready_task/spawn_pinned
// (runtime_v2_lifecycle_behavior_harness_test.go) - this file must not edit
// that file, so top-level (no-parent) task spawning is reimplemented here.
static rt_task* race_alloc_ready_task(rt_executor* ex, int64_t poll_fn_id) {
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
    rt_task_slot_store(ex, id, task);
    return task;
}

static rt_task* race_spawn_top_level(rt_executor* ex, int64_t poll_fn_id, uint32_t wanted_shard) {
    rt_control_lock(ex);
    rt_task* task = race_alloc_ready_task(ex, poll_fn_id);
    if (task != NULL) {
        size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
        uint32_t shard = shard_count > 0 ? wanted_shard % (uint32_t)shard_count : 0;
        rt_task_set_placement(task, shard, TASK_PLACEMENT_GENERIC);
        ready_push(ex, task->id);
    }
    rt_control_unlock(ex);
    return task;
}

static void poll_race_child(void) {
    rt_async_return(NULL, 0);
}

// Spawns children of itself via __task_create (rt_current_task() is the
// parent) in small batches with a yield in between, so the canceller thread
// below has real wall-clock windows to race task_add_child's append
// (rt_async_task.c, now under the parent's owner shard lock) against
// cancel_task's children[] snapshot (rt_async_state.c, under the same
// lock). Keeps spawning regardless of its own cancelled flag, so children
// keep being appended even after a cancel lands.
static void poll_race_parent(void) {
    race_parent_state* st = (race_parent_state*)__task_state();
    if (st == NULL) {
        st = (race_parent_state*)malloc(sizeof(race_parent_state));
        if (st == NULL) {
            rt_async_return(NULL, 0);
            return;
        }
        st->spawned = 0;
    }
    if (st->spawned < RACE_CHILDREN_PER_PARENT) {
        for (int i = 0; i < 3 && st->spawned < RACE_CHILDREN_PER_PARENT; i++) {
            (void)__task_create(POLL_RACE_CHILD, NULL);
            st->spawned++;
            atomic_fetch_add_explicit(&g_race_children_total, 1, memory_order_relaxed);
        }
        rt_async_yield(st);
        return;
    }
    free(st);
    rt_async_return(NULL, 1);
}

static void* race_canceller_main(void* arg) {
    (void)arg;
    for (int pass = 0; pass < 30; pass++) {
        for (int i = 0; i < RACE_PARENT_COUNT; i++) {
            void* handle = atomic_load_explicit(&g_race_parents[i], memory_order_acquire);
            if (handle != NULL) {
                rt_task_cancel(handle);
            }
        }
        sleep_us(200);
    }
    return NULL;
}

void __surge_poll_call(uint64_t id) {
    switch (id) {
        case POLL_RACE_PARENT:
            poll_race_parent();
            break;
        case POLL_RACE_CHILD:
            poll_race_child();
            break;
        default:
            break;
    }
    rt_async_return(NULL, 0);
}

static int mode_cancel_spawn_race(rt_executor* ex) {
    atomic_store_explicit(&g_race_children_total, 0, memory_order_relaxed);
    for (int i = 0; i < RACE_PARENT_COUNT; i++) {
        atomic_store_explicit(&g_race_parents[i], NULL, memory_order_relaxed);
    }
    for (int i = 0; i < RACE_PARENT_COUNT; i++) {
        rt_task* parent = race_spawn_top_level(ex, POLL_RACE_PARENT, (uint32_t)i);
        if (parent == NULL) {
            return fail("race parent allocation failed");
        }
        atomic_store_explicit(&g_race_parents[i], parent, memory_order_release);
    }

    pthread_t canceller;
    if (pthread_create(&canceller, NULL, race_canceller_main, NULL) != 0) {
        return fail("failed to start canceller thread");
    }

    int ok = 1;
    for (int i = 0; i < RACE_PARENT_COUNT; i++) {
        rt_task* parent =
            (rt_task*)atomic_load_explicit(&g_race_parents[i], memory_order_acquire);
        if (!wait_task_status(parent, TASK_DONE, 20000)) {
            ok = 0;
            break;
        }
    }
    pthread_join(canceller, NULL);
    if (!ok) {
        (void)rt_executor_request_shutdown(ex);
        return fail("race parent did not reach DONE (hang or lost wakeup)");
    }
    if (atomic_load_explicit(&g_race_children_total, memory_order_acquire) == 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("no children were ever spawned; test did not exercise the race");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

int main(int argc, char** argv) {
    (void)argc;
    (void)argv;
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return fail("missing executor");
    }
    return mode_cancel_spawn_race(ex);
}
`
