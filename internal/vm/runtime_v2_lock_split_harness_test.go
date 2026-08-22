//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

// buildRuntimeV2LockSplitHarness compiles the lock-split behavior harness
// against the full native runtime (rt_entry.c excluded), in the established
// scheduler-placement-harness pattern.
func buildRuntimeV2LockSplitHarness(t *testing.T) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping native lock-split behavior test")
	}

	root := repoRoot(t)
	tmpDir := t.TempDir()
	harnessPath := filepath.Join(tmpDir, "lock_split_harness.c")
	binPath := filepath.Join(tmpDir, "lock_split_harness")
	if writeErr := os.WriteFile(harnessPath, []byte(lockSplitHarness), 0o600); writeErr != nil {
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
		t.Fatalf("build lock-split harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			buildCode, buildOut, buildErr)
	}
	return binPath
}

// The harness pins tasks with modulo of the live shard count, so every mode
// is valid for SURGE_SHARDS=1 compatibility runs and for multi-shard runs.
// Every wait is attempt-bounded: a lost wakeup fails fast instead of hanging.
const lockSplitHarness = `
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
    POLL_YIELDER = 2001,
    POLL_JOINER = 2002,
    POLL_PARK_RECV = 2003,
    POLL_SENDER = 2004,
    POLL_RECEIVER_FIFO = 2005,
    POLL_MAKE_CHAN_A = 2006,
    POLL_BLOCKING_AWAITER = 2008,
    POLL_SLEEPER = 2009,
    POLL_SELECTOR_TASKS = 2010,
    POLL_TIMEOUT_AWAITER = 2012
};

enum { BLOCKING_FN_SLOW_42 = 3001 };

#define LOCK_SPLIT_FIFO_VALUES 200
#define LOCK_SPLIT_CHAN_CAP 4

static _Atomic(void*) g_chan_a;
static _Atomic(void*) g_sleep_handle;
static _Atomic(void*) g_blocking_handle;
static void* g_join_target;
static void* g_select_arm0;
static void* g_select_arm1;
static _Atomic uint32_t g_yielder_steps;
static _Atomic uint32_t g_send_count;
static _Atomic uint32_t g_recv_count;
static _Atomic uint32_t g_recv_bad_order;
static _Atomic uint32_t g_recv_closed;
static _Atomic uint32_t g_park_recv_entered;

static void sleep_us(unsigned long micros) {
    struct timespec ts;
    ts.tv_sec = (time_t)(micros / 1000000UL);
    ts.tv_nsec = (long)((micros % 1000000UL) * 1000UL);
    while (nanosleep(&ts, &ts) != 0) {
    }
}

static void poll_yielder(void) {
    uint32_t step = atomic_fetch_add_explicit(&g_yielder_steps, 1, memory_order_acq_rel);
    if (step < 3) {
        rt_async_yield(NULL, 0);
    }
    rt_async_return(NULL, 7);
}

static void poll_joiner(void) {
    uint64_t bits = 0;
    uint8_t st = rt_task_poll(g_join_target, &bits);
    if (st == 0) {
        rt_async_yield(NULL, 0);
    }
    rt_async_return(NULL, st == 1 ? bits : 0);
}

static void poll_park_recv(void) {
    atomic_store_explicit(&g_park_recv_entered, 1, memory_order_release);
    void* ch = atomic_load_explicit(&g_chan_a, memory_order_acquire);
    uint64_t bits = 0;
    uint8_t st = rt_channel_recv(ch, &bits);
    if (st == 0) {
        rt_async_yield(NULL, 0);
    }
    rt_async_return(NULL, st);
}

static void poll_sender(void) {
    void* ch = atomic_load_explicit(&g_chan_a, memory_order_acquire);
    for (;;) {
        uint32_t sent = atomic_load_explicit(&g_send_count, memory_order_acquire);
        if (sent >= LOCK_SPLIT_FIFO_VALUES) {
            rt_channel_close(ch);
            rt_async_return(NULL, 0);
        }
        if (!rt_channel_send(ch, &(uint64_t){(uint64_t)sent + 1})) {
            rt_async_yield(NULL, 0);
        }
        atomic_fetch_add_explicit(&g_send_count, 1, memory_order_acq_rel);
    }
}

static void poll_receiver_fifo(void) {
    void* ch = atomic_load_explicit(&g_chan_a, memory_order_acquire);
    for (;;) {
        uint64_t bits = 0;
        uint8_t st = rt_channel_recv(ch, &bits);
        if (st == 0) {
            rt_async_yield(NULL, 0);
        }
        if (st == 2) {
            atomic_store_explicit(&g_recv_closed, 1, memory_order_release);
            rt_async_return(NULL, 0);
        }
        uint32_t got = atomic_fetch_add_explicit(&g_recv_count, 1, memory_order_acq_rel) + 1;
        if (bits != (uint64_t)got) {
            atomic_store_explicit(&g_recv_bad_order, 1, memory_order_release);
        }
    }
}

static void poll_make_chan_a(void) {
    atomic_store_explicit(
        &g_chan_a, rt_channel_new(LOCK_SPLIT_CHAN_CAP, rt_channel_opaque_word_ops(), 0), memory_order_release);
    rt_async_return(NULL, 0);
}

static void poll_blocking_awaiter(void) {
    void* handle = atomic_load_explicit(&g_blocking_handle, memory_order_acquire);
    if (handle == NULL) {
        handle = rt_blocking_submit(BLOCKING_FN_SLOW_42, NULL, 0, 0);
        if (handle == NULL) {
            rt_async_return(NULL, 0);
        }
        atomic_store_explicit(&g_blocking_handle, handle, memory_order_release);
    }
    uint64_t bits = 0;
    uint8_t st = rt_task_poll(handle, &bits);
    if (st == 0) {
        rt_async_yield(NULL, 0);
    }
    rt_async_return(NULL, st == 1 ? bits : 0);
}

static void poll_sleeper(void) {
    void* handle = atomic_load_explicit(&g_sleep_handle, memory_order_acquire);
    if (handle == NULL) {
        handle = rt_sleep(5);
        if (handle == NULL) {
            rt_async_return(NULL, 0);
        }
        atomic_store_explicit(&g_sleep_handle, handle, memory_order_release);
    }
    uint64_t bits = 0;
    uint8_t st = rt_task_poll(handle, &bits);
    if (st == 0) {
        rt_async_yield(NULL, 0);
    }
    rt_async_return(NULL, 9);
}

static void poll_selector_tasks(void) {
    void* arms[2] = {g_select_arm0, g_select_arm1};
    int64_t idx = rt_select_poll_tasks(2, arms, -1);
    if (idx < 0) {
        rt_async_yield(NULL, 0);
    }
    rt_async_return(NULL, (uint64_t)idx);
}

static void poll_timeout_awaiter(void) {
    uint64_t bits = 0;
    uint8_t st = rt_timeout_poll(g_join_target, 3, &bits);
    if (st == 0) {
        rt_async_yield(NULL, 0);
    }
    rt_async_return(NULL, st);
}

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
    switch (id) {
        case POLL_YIELDER:
            poll_yielder();
            break;
        case POLL_JOINER:
            poll_joiner();
            break;
        case POLL_PARK_RECV:
            poll_park_recv();
            break;
        case POLL_SENDER:
            poll_sender();
            break;
        case POLL_RECEIVER_FIFO:
            poll_receiver_fifo();
            break;
        case POLL_MAKE_CHAN_A:
            poll_make_chan_a();
            break;
        case POLL_BLOCKING_AWAITER:
            poll_blocking_awaiter();
            break;
        case POLL_SLEEPER:
            poll_sleeper();
            break;
        case POLL_SELECTOR_TASKS:
            poll_selector_tasks();
            break;
        case POLL_TIMEOUT_AWAITER:
            poll_timeout_awaiter();
            break;
        default:
            break;
    }
    rt_async_return(NULL, 0);
}

uint64_t __surge_blocking_call(uint64_t id, void* state) {
    (void)state;
    if (id == BLOCKING_FN_SLOW_42) {
        sleep_us(2000);
        return 42;
    }
    return 0;
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

static uint32_t pin_shard(rt_executor* ex, uint32_t wanted) {
    size_t count = rt_runtime_shard_count(rt_executor_runtime(ex));
    if (count < 1) {
        return 0;
    }
    return (uint32_t)(wanted % (uint32_t)count);
}

static rt_task* spawn_pinned(rt_executor* ex, int64_t poll_fn_id, uint32_t wanted_shard) {
    rt_control_lock(ex);
    rt_task* task = alloc_ready_task(ex, poll_fn_id);
    if (task != NULL) {
        rt_task_set_placement(task, pin_shard(ex, wanted_shard), TASK_PLACEMENT_CONNECTION);
        ready_push(ex, task->id);
    }
    rt_control_unlock(ex);
    return task;
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

static int wait_ptr(_Atomic(void*)* slot, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(slot, memory_order_acquire) != NULL) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static int make_chan_on(rt_executor* ex, uint32_t wanted_shard) {
    rt_task* maker = spawn_pinned(ex, POLL_MAKE_CHAN_A, wanted_shard);
    if (maker == NULL) {
        return 0;
    }
    if (!wait_ptr(&g_chan_a, 4000)) {
        return 0;
    }
    return wait_task_status(maker, TASK_DONE, 4000);
}

static int await_expect(rt_executor* ex, void* handle, uint8_t want_kind, uint64_t want_bits,
                        const char* what) {
    rt_task* task = (rt_task*)handle;
    if (!wait_task_status(task, TASK_DONE, 8000)) {
        (void)rt_executor_request_shutdown(ex);
        fprintf(stderr, "timeout waiting for %s\n", what);
        return 0;
    }
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(handle, &kind, &bits);
    if (kind != want_kind || bits != want_bits) {
        fprintf(stderr, "%s: kind=%u bits=%llu want kind=%u bits=%llu\n", what, (unsigned)kind,
                (unsigned long long)bits, (unsigned)want_kind, (unsigned long long)want_bits);
        return 0;
    }
    return 1;
}

static int mode_cross_join(rt_executor* ex) {
    rt_task* target = spawn_pinned(ex, POLL_YIELDER, 1);
    if (target == NULL) {
        return fail("target allocation failed");
    }
    g_join_target = target;
    rt_task* joiner = spawn_pinned(ex, POLL_JOINER, 0);
    if (joiner == NULL) {
        return fail("joiner allocation failed");
    }
    if (!await_expect(ex, joiner, 1, 7, "cross-shard joiner")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_cross_cancel(rt_executor* ex) {
    if (!make_chan_on(ex, 2)) {
        return fail("channel maker failed");
    }
    rt_task* gate = spawn_pinned(ex, POLL_PARK_RECV, 1);
    if (gate == NULL) {
        return fail("gate allocation failed");
    }
    if (!wait_task_status(gate, TASK_WAITING, 4000)) {
        return fail("gate did not park on the channel");
    }
    rt_task_cancel(gate);
    if (!await_expect(ex, gate, 2, 0, "cancelled cross-shard receiver")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_cross_channel(rt_executor* ex) {
    if (!make_chan_on(ex, 2)) {
        return fail("channel maker failed");
    }
    rt_task* receiver = spawn_pinned(ex, POLL_RECEIVER_FIFO, 0);
    rt_task* sender = spawn_pinned(ex, POLL_SENDER, 1);
    if (receiver == NULL || sender == NULL) {
        return fail("actor allocation failed");
    }
    if (!await_expect(ex, sender, 1, 0, "cross-shard sender")) {
        return 1;
    }
    if (!await_expect(ex, receiver, 1, 0, "cross-shard receiver")) {
        return 1;
    }
    if (atomic_load_explicit(&g_recv_count, memory_order_acquire) != LOCK_SPLIT_FIFO_VALUES) {
        return fail("receiver did not observe every value");
    }
    if (atomic_load_explicit(&g_recv_bad_order, memory_order_acquire) != 0) {
        return fail("channel FIFO order violated");
    }
    if (atomic_load_explicit(&g_recv_closed, memory_order_acquire) != 1) {
        return fail("receiver missed channel close");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_close_wakes(rt_executor* ex) {
    if (!make_chan_on(ex, 1)) {
        return fail("channel maker failed");
    }
    rt_task* gate = spawn_pinned(ex, POLL_PARK_RECV, 2);
    if (gate == NULL) {
        return fail("gate allocation failed");
    }
    if (!wait_task_status(gate, TASK_WAITING, 4000)) {
        return fail("receiver did not park on the channel");
    }
    rt_channel_close(atomic_load_explicit(&g_chan_a, memory_order_acquire));
    if (!await_expect(ex, gate, 1, 2, "close-woken receiver")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_blocking_completion(rt_executor* ex) {
    rt_task* awaiter = spawn_pinned(ex, POLL_BLOCKING_AWAITER, 1);
    if (awaiter == NULL) {
        return fail("awaiter allocation failed");
    }
    if (!await_expect(ex, awaiter, 1, 42, "blocking awaiter")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_sleep_idle_advance(rt_executor* ex) {
    rt_task* sleeper = spawn_pinned(ex, POLL_SLEEPER, 1);
    if (sleeper == NULL) {
        return fail("sleeper allocation failed");
    }
    if (!await_expect(ex, sleeper, 1, 9, "idle-advanced sleeper")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_select_across_owners(rt_executor* ex) {
    if (!make_chan_on(ex, 1)) {
        return fail("channel maker failed");
    }
    rt_task* gate = spawn_pinned(ex, POLL_PARK_RECV, 1);
    rt_task* yielder = spawn_pinned(ex, POLL_YIELDER, 2);
    if (gate == NULL || yielder == NULL) {
        return fail("arm allocation failed");
    }
    g_select_arm0 = gate;
    g_select_arm1 = yielder;
    rt_task* selector = spawn_pinned(ex, POLL_SELECTOR_TASKS, 0);
    if (selector == NULL) {
        return fail("selector allocation failed");
    }
    if (!await_expect(ex, selector, 1, 1, "cross-owner selector")) {
        return 1;
    }
    rt_task_cancel(gate);
    if (!await_expect(ex, gate, 2, 0, "losing select arm")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_timeout_across_owners(rt_executor* ex) {
    if (!make_chan_on(ex, 1)) {
        return fail("channel maker failed");
    }
    rt_task* gate = spawn_pinned(ex, POLL_PARK_RECV, 2);
    if (gate == NULL) {
        return fail("gate allocation failed");
    }
    if (!wait_task_status(gate, TASK_WAITING, 4000)) {
        return fail("gate did not park on the channel");
    }
    // rt_timeout_poll releases the target handle on the timeout path; keep a
    // main-owned reference so the later status wait and await stay valid.
    g_join_target = rt_task_clone(gate, NULL, 0);
    if (g_join_target == NULL) {
        return fail("gate clone failed");
    }
    rt_task* awaiter = spawn_pinned(ex, POLL_TIMEOUT_AWAITER, 1);
    if (awaiter == NULL) {
        return fail("timeout awaiter allocation failed");
    }
    if (!await_expect(ex, awaiter, 1, 2, "timeout awaiter")) {
        return 1;
    }
    if (!await_expect(ex, gate, 2, 0, "timed-out target")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_shutdown_liveness(rt_executor* ex) {
    atomic_store_explicit(&g_chan_a, rt_channel_new(0, rt_channel_opaque_word_ops(), 0), memory_order_release);
    rt_task* gates[3];
    for (uint32_t i = 0; i < 3; i++) {
        gates[i] = spawn_pinned(ex, POLL_PARK_RECV, i);
        if (gates[i] == NULL) {
            return fail("gate allocation failed");
        }
    }
    for (uint32_t i = 0; i < 3; i++) {
        if (!wait_task_status(gates[i], TASK_WAITING, 4000)) {
            return fail("a receiver did not park before shutdown");
        }
    }
    if (rt_executor_request_shutdown(ex) != RT_RUNTIME_STATUS_OK) {
        return fail("shutdown request failed");
    }
    return 0;
}

int main(int argc, char** argv) {
    if (argc != 2) {
        return fail("usage: lock_split_harness <mode>");
    }
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return fail("missing executor");
    }
    if (strcmp(argv[1], "cross-join") == 0) {
        return mode_cross_join(ex);
    }
    if (strcmp(argv[1], "cross-cancel") == 0) {
        return mode_cross_cancel(ex);
    }
    if (strcmp(argv[1], "cross-channel") == 0) {
        return mode_cross_channel(ex);
    }
    if (strcmp(argv[1], "close-wakes") == 0) {
        return mode_close_wakes(ex);
    }
    if (strcmp(argv[1], "blocking-completion") == 0) {
        return mode_blocking_completion(ex);
    }
    if (strcmp(argv[1], "sleep-idle-advance") == 0) {
        return mode_sleep_idle_advance(ex);
    }
    if (strcmp(argv[1], "select-across-owners") == 0) {
        return mode_select_across_owners(ex);
    }
    if (strcmp(argv[1], "timeout-across-owners") == 0) {
        return mode_timeout_across_owners(ex);
    }
    if (strcmp(argv[1], "shutdown-liveness") == 0) {
        return mode_shutdown_liveness(ex);
    }
    return fail("unknown mode");
}
`
