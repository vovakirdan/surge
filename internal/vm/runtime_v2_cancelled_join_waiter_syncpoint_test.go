//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const cancelledJoinWaiterSyncPoint = "SP_TASK_POLL_AFTER_JOIN_REGISTER"

func buildRuntimeV2CancelledJoinWaiterHarness(t *testing.T, negativeControl bool) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping cancelled join waiter proof")
	}

	root := repoRoot(t)
	tmpDir := t.TempDir()
	name := "cancelled_join_waiter"
	if negativeControl {
		name += "_negative"
	}
	harnessPath := filepath.Join(tmpDir, name+".c")
	binPath := filepath.Join(tmpDir, name)
	if err := os.WriteFile(harnessPath, []byte(cancelledJoinWaiterHarnessSource), 0o600); err != nil {
		t.Fatalf("write cancelled join waiter harness: %v", err)
	}

	sources, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob runtime sources: %v", err)
	}
	sort.Strings(sources)
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-pthread", "-DRT_TEST_SYNC_POINTS",
		"-I" + filepath.Join(root, "runtime", "native"), "-o", binPath, harnessPath,
	}
	for _, source := range sources {
		if filepath.Base(source) != "rt_entry.c" {
			args = append(args, source)
		}
	}

	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	stdout, stderr, exitCode := runCommand(t, cmd, "")
	if exitCode != 0 {
		t.Fatalf("build cancelled join waiter harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	return binPath
}

func runRuntimeV2CancelledJoinWaiterHarness(t *testing.T, binPath string) (string, string, int) {
	t.Helper()
	return runRuntimeV2CancelledJoinWaiterMode(t, binPath, "positive")
}

func runRuntimeV2CancelledJoinWaiterMode(
	t *testing.T, binPath string, mode string,
) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binPath, mode)
	cmd.Env = lifecycleEnv(
		"SURGE_SHARDS=2",
		"SURGE_THREADS=2",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT="+cancelledJoinWaiterSyncPoint+":block",
	)
	return runCommand(t, cmd, "")
}

func proveRuntimeV2CancelledJoinWaiterRegistrationNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2CancelledJoinWaiterHarness(t, true)
	stdout, stderr, exitCode := runRuntimeV2CancelledJoinWaiterMode(t, binPath, "negative")
	if exitCode == 0 {
		t.Fatalf("cancel-before-registration negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	const want = "cancelled join waiter registration assertion failed"
	if !strings.Contains(stderr, want) {
		t.Fatalf("negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

func checkRuntimeV2CancelledJoinWaiterSyncPointStaticBoundary(t *testing.T) {
	path := filepath.Join(repoRoot(t), "runtime", "native", "rt_async_task.c")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rt_async_task.c: %v", err)
	}
	text := string(source)
	start := strings.Index(text, "uint8_t rt_task_poll(void* task, void* out_dst) {")
	endOffset := -1
	if start >= 0 {
		endOffset = strings.Index(text[start:], "\nstatic void rt_task_poll_adopt_placement(")
	}
	if start < 0 || endOffset <= 0 {
		t.Fatal("could not isolate rt_task_poll for the join-registration boundary guard")
	}
	end := start + endOffset
	boundary := regexp.MustCompile(
		`(?s)prepare_park\(ex, current, key, 0\);\s*` +
			`pending_key = key;\s*` +
			`RT_SYNC_POINT\(SP_TASK_POLL_AFTER_JOIN_REGISTER\);\s*` +
			`// Register-then-verify:.*?` +
			`if \(task_status_load\(target\) == TASK_DONE\)`,
	)
	if !boundary.MatchString(text[start:end]) {
		t.Fatal("join boundary must remain prepare_park -> pending_key -> sync point -> DONE re-check")
	}
	headerPath := filepath.Join(repoRoot(t), "runtime", "native", "rt_sync_point.h")
	header, err := os.ReadFile(headerPath)
	if err != nil {
		t.Fatalf("read rt_sync_point.h: %v", err)
	}
	if !strings.Contains(string(header), "#define RT_SYNC_POINT(name) ((void)RT_SYNC_POINT_##name)") {
		t.Fatal("release RT_SYNC_POINT must remain a branch-free enum cast")
	}
}

const cancelledJoinWaiterHarnessSource = `
#define _POSIX_C_SOURCE 199309L
#include "rt_async_internal.h"
#include "rt_sync_point.h"

#include <sched.h>
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

enum {
    POLL_CANCELLED_JOIN_TARGET = 9401,
    POLL_CANCELLED_JOIN_WAITER = 9402
};

static _Atomic uint32_t g_target_open;

static int fail(const char* message) {
    fputs(message, stderr);
    fputc('\n', stderr);
    return 1;
}

static uint64_t monotonic_millis(void) {
    struct timespec now;
    clock_gettime(CLOCK_MONOTONIC, &now);
    return (uint64_t)now.tv_sec * 1000U + (uint64_t)now.tv_nsec / 1000000U;
}

static int wait_task_status_until(const rt_task* task, uint8_t want) {
    uint64_t deadline = monotonic_millis() + 4000U;
    while (monotonic_millis() < deadline) {
        if (task_status_load(task) == want) {
            return 1;
        }
        sched_yield();
    }
    return 0;
}

static int wait_sync_point_after(unsigned before) {
    return rt_sync_point_wait_until_after(
        RT_SYNC_POINT_SP_TASK_POLL_AFTER_JOIN_REGISTER, before);
}

static rt_task* alloc_task_locked(rt_executor* ex, int64_t poll_fn_id, void* state) {
    uint64_t id = ex->next_id++;
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(*task), _Alignof(rt_task));
    if (task == NULL) {
        return NULL;
    }
    memset(task, 0, sizeof(*task));
    // A stand's task answers with a machine word, which is exactly what the
    // opaque-word descriptor describes: the result slot carries it the same way
    // it carries a compiled type's value.
    (void)rt_value_cell_bind(&task->result, rt_channel_opaque_word_ops());
    task->id = id;
    task->generation = id;
    task->poll_fn_id = poll_fn_id;
    task->state = state;
    task->kind = TASK_KIND_USER;
    task_status_store(task, TASK_READY);
    task_cancelled_store(task, 0);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    rt_task_slot_store(ex, id, task);
    return task;
}

static rt_task* make_unpublished_task(
    rt_executor* ex, int64_t poll_fn_id, void* state, uint32_t shard_id) {
    rt_control_lock(ex);
    rt_task* task = alloc_task_locked(ex, poll_fn_id, state);
    if (task != NULL) {
        rt_task_set_placement(task, shard_id, TASK_PLACEMENT_CONNECTION);
    }
    rt_control_unlock(ex);
    return task;
}

static int join_waiter_registered(
    rt_executor* ex, const rt_task* target, const rt_task* joiner) {
    waker_key key = join_key(target->id);
    rt_runtime* runtime = rt_executor_runtime(ex);
    for (;;) {
        uint32_t route = rt_task_join_owner_shard_id_load(target);
        rt_shard* shard = rt_runtime_shard(runtime, route);
        if (shard == NULL) {
            return 0;
        }
        rt_shard_lock(shard);
        if (route != rt_task_join_owner_shard_id_load(target)) {
            rt_shard_unlock(shard);
            continue;
        }
        const rt_waiter_store* store = rt_executor_waiter_store_const_for_shard(ex, route);
        int found = 0;
        for (size_t i = 0; store != NULL && i < store->len; i++) {
            const waiter* entry = &store->entries[i];
            if (entry->key.kind == key.kind && entry->key.id == key.id &&
                entry->task_id == joiner->id) {
                found = 1;
                break;
            }
        }
        rt_shard_unlock(shard);
        return found;
    }
}

static int await_expect(
    rt_task* task, uint8_t want_kind, uint64_t want_bits, const char* name) {
    if (!wait_task_status_until(task, TASK_DONE)) {
        fprintf(stderr, "%s did not complete\n", name);
        return 0;
    }
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(task, &kind, &bits);
    if (kind != want_kind || bits != want_bits) {
        fprintf(stderr, "%s: kind=%u bits=%llu, want kind=%u bits=%llu\n",
                name, (unsigned)kind, (unsigned long long)bits,
                (unsigned)want_kind, (unsigned long long)want_bits);
        return 0;
    }
    return 1;
}

static void poll_target(void) {
    if (atomic_load_explicit(&g_target_open, memory_order_acquire) == 0) {
        rt_executor* ex = ensure_exec();
        rt_task* current = rt_current_task();
        waker_key key = blocking_key(current->id);
        prepare_park(ex, current, key, 0);
        pending_key = key;
        rt_async_yield(NULL, 0);
    }
    rt_async_return(NULL, &(uint64_t){42});
}

static void poll_join_waiter(void) {
    void* target = __task_state();
    uint64_t bits = 0;
    uint8_t status = rt_task_poll(target, &bits);
    if (status == 0) {
        // The state is the target HANDLE, borrowed, not a box this task owns:
        // a cancellation that abandons this frame has nothing here to reclaim,
        // and saying otherwise would hand the runtime a task pointer to free.
        rt_async_yield(target, 0);
    }
    rt_async_return(NULL, &(uint64_t){status == 1 ? bits : 0});
}

void __surge_poll_call(uint64_t id) {
    if (id == POLL_CANCELLED_JOIN_TARGET) {
        poll_target();
    }
    if (id == POLL_CANCELLED_JOIN_WAITER) {
        poll_join_waiter();
    }
    rt_async_return(NULL, &(uint64_t){0});
}

void __surge_drop_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

void __surge_drop_result_call(uint64_t id, void* value) {
    (void)id;
    (void)value;
}

void __surge_drop_abandoned_state_call(uint64_t id, void* state) {
    if (id == POLL_CANCELLED_JOIN_WAITER && state != NULL) {
        task_release_lane_aware(ensure_exec(), (rt_task*)state);
    }
}

void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    if (out_dst != NULL) {
            *(uint64_t*)out_dst = 0;
        }
        return;
}

static int registration_failure(rt_executor* ex, rt_task* target) {
    void* target_clone = rt_task_clone(target, NULL);
    if (target_clone == NULL) {
        return fail("negative-control target clone failed");
    }
    rt_task* joiner = make_unpublished_task(ex, POLL_CANCELLED_JOIN_WAITER, target_clone, 0);
    if (joiner == NULL) {
        task_release_lane_aware(ex, (rt_task*)target_clone);
        return fail("negative-control joiner allocation failed");
    }
    rt_task_cancel(joiner);
    if (join_waiter_registered(ex, target, joiner)) {
        return fail("negative control unexpectedly registered a join waiter");
    }
    (void)rt_executor_request_shutdown(ex);
    return fail("cancelled join waiter registration assertion failed");
}

static int positive_proof(rt_executor* ex, rt_task* target) {
    unsigned first_before = rt_sync_point_reached_count(
        RT_SYNC_POINT_SP_TASK_POLL_AFTER_JOIN_REGISTER);
    void* first_target = rt_task_clone(target, NULL);
    if (first_target == NULL) {
        return fail("first target clone failed");
    }
    rt_task* first = (rt_task*)__task_create(POLL_CANCELLED_JOIN_WAITER, first_target, rt_channel_opaque_word_ops());
    if (first == NULL) {
        task_release_lane_aware(ex, (rt_task*)first_target);
        return fail("first joiner allocation failed");
    }
    if (!wait_sync_point_after(first_before)) {
        return fail("first joiner did not reach the registration sync point");
    }
    if (!join_waiter_registered(ex, target, first)) {
        rt_sync_point_open();
        return fail("first join waiter was not registered at the sync point");
    }
    rt_task_cancel(first);
    rt_sync_point_open();
    if (!await_expect(first, 2, 0, "first cancelled joiner")) {
        return fail("first joiner did not complete as cancelled");
    }

    unsigned second_before = rt_sync_point_reached_count(
        RT_SYNC_POINT_SP_TASK_POLL_AFTER_JOIN_REGISTER);
    unsigned second_park_before = rt_sync_point_reached_count(
        RT_SYNC_POINT_SP_PARK_BEFORE_WAITING);
    void* second_target = rt_task_clone(target, NULL);
    if (second_target == NULL) {
        return fail("second target clone failed");
    }
    rt_task* second = (rt_task*)__task_create(POLL_CANCELLED_JOIN_WAITER, second_target, rt_channel_opaque_word_ops());
    if (second == NULL) {
        task_release_lane_aware(ex, (rt_task*)second_target);
        return fail("second joiner allocation failed");
    }
    if (!wait_sync_point_after(second_before)) {
        return fail("second joiner did not reach the registration sync point");
    }
    if (!join_waiter_registered(ex, target, second)) {
        rt_sync_point_open();
        return fail("second join waiter was not registered at the sync point");
    }
    rt_sync_point_open();
    if (!rt_sync_point_wait_until_after(
            RT_SYNC_POINT_SP_PARK_BEFORE_WAITING, second_park_before)) {
        return fail("second joiner did not reach the park commit boundary");
    }
    if (!wait_task_status_until(second, TASK_WAITING)) {
        return fail("second joiner did not park after registration");
    }

    atomic_store_explicit(&g_target_open, 1, memory_order_release);
    wake_task(ex, target->id, 1);
    if (!await_expect(second, 1, 42, "second joiner")) {
        return fail("second joiner missed the target completion");
    }
    if (!await_expect(target, 1, 42, "target")) {
        return fail("target result mismatch");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

int main(int argc, char** argv) {
    if (argc != 2) {
        return fail("usage: cancelled_join_waiter positive|negative");
    }
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return fail("missing executor");
    }
    atomic_store_explicit(&g_target_open, 0, memory_order_release);
    if (strcmp(argv[1], "negative") == 0) {
        rt_task* target = make_unpublished_task(ex, POLL_CANCELLED_JOIN_TARGET, NULL, 1);
        if (target == NULL) {
            return fail("negative-control target allocation failed");
        }
        return registration_failure(ex, target);
    }
    if (strcmp(argv[1], "positive") != 0) {
        return fail("unknown cancelled join waiter mode");
    }
    unsigned target_park_before = rt_sync_point_reached_count(
        RT_SYNC_POINT_SP_PARK_BEFORE_WAITING);
    rt_task* target = (rt_task*)__task_create(POLL_CANCELLED_JOIN_TARGET, NULL, rt_channel_opaque_word_ops());
    if (target == NULL) {
        return fail("target allocation failed");
    }
    if (!rt_sync_point_wait_until_after(
            RT_SYNC_POINT_SP_PARK_BEFORE_WAITING, target_park_before)) {
        fprintf(stderr,
                "target park boundary wait: status=%u enqueued=%u owner=%u\n",
                (unsigned)task_status_load(target), (unsigned)task_enqueued_load(target),
                target->owner_shard_id);
        return fail("target did not reach its park commit boundary");
    }
    if (!wait_task_status_until(target, TASK_WAITING)) {
        fprintf(stderr,
                "target gate wait: status=%u enqueued=%u owner=%u shards=%zu workers=%llu\n",
                (unsigned)task_status_load(target), (unsigned)task_enqueued_load(target),
                target->owner_shard_id, rt_runtime_shard_count(rt_executor_runtime(ex)),
                (unsigned long long)rt_worker_count());
        return fail("target did not park behind its explicit gate");
    }
    return positive_proof(ex, target);
}
`
