//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeV2OwnerLocalWaiterSkeletonStaticShape(t *testing.T) {
	root := repoRoot(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skipf("clang not installed; skipping Runtime V2 owner-local waiter static shape check: %v", err)
	}

	source := `
	#include "rt_async_internal.h"

	rt_waiter_store* (*runtime_v2_check_shard_waiter_store)(rt_shard*) = rt_shard_waiter_store;
	const rt_waiter_store* (*runtime_v2_check_shard_waiter_store_const)(const rt_shard*) = rt_shard_waiter_store_const;
	rt_waiter_store* (*runtime_v2_check_executor_waiter_store_for_shard)(rt_executor*, size_t) = rt_executor_waiter_store_for_shard;
	const rt_waiter_store* (*runtime_v2_check_executor_waiter_store_const_for_shard)(const rt_executor*, size_t) = rt_executor_waiter_store_const_for_shard;
	rt_waiter_store* (*runtime_v2_check_executor_waiter_store)(rt_executor*) = rt_executor_waiter_store;
	const rt_waiter_store* (*runtime_v2_check_executor_waiter_store_const)(const rt_executor*) = rt_executor_waiter_store_const;

	_Static_assert(sizeof(rt_waiter_store) > 0, "rt_waiter_store must be complete");
	_Static_assert(sizeof(((rt_waiter_store*)0)->entries) == sizeof(waiter*), "rt_waiter_store.entries must store waiter entries");
	_Static_assert(sizeof(((rt_waiter_store*)0)->len) == sizeof(size_t), "rt_waiter_store.len must stay size_t");
	_Static_assert(sizeof(((rt_waiter_store*)0)->cap) == sizeof(size_t), "rt_waiter_store.cap must stay size_t");
	_Static_assert(sizeof(((rt_waiter_store*)0)->net_len) == sizeof(size_t), "rt_waiter_store.net_len must stay size_t");
	_Static_assert(sizeof(((rt_shard*)0)->waiter_store) == sizeof(rt_waiter_store), "rt_shard.waiter_store must own waiter storage");
	`

	cmd := exec.Command(
		clang,
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Werror",
		"-fsyntax-only",
		"-I"+filepath.Join(root, "runtime", "native"),
		"-x",
		"c",
		"-",
	)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(source)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Runtime V2 owner-local waiter static shape check failed:\n%s", output)
	}
}

func TestRuntimeV2OwnerLocalTraceAggregatesShardWaiters(t *testing.T) {
	root := repoRoot(t)
	traceBytes, err := os.ReadFile(filepath.Join(root, "runtime", "native", "rt_async_trace.c"))
	if err != nil {
		t.Fatalf("read rt_async_trace.c: %v", err)
	}
	trace := string(traceBytes)
	if strings.Contains(trace, "rt_executor_waiter_store_const(ex)") {
		t.Fatalf("TRACE_EXEC_SNAPSHOT must not read only shard-0 waiter store after owner-local net waiter migration")
	}
	if !strings.Contains(trace, "rt_trace_collect_waiter_counts(ex, &waiters)") {
		t.Fatalf("TRACE_EXEC_SNAPSHOT must use aggregated waiter counters")
	}

	helperBytes, err := os.ReadFile(filepath.Join(root, "runtime", "native", "rt_async_trace_waiters.c"))
	if err != nil {
		t.Fatalf("read rt_async_trace_waiters.c: %v", err)
	}
	helper := string(helperBytes)
	for _, needle := range []string{
		"rt_runtime_shard_count(runtime)",
		"rt_executor_waiter_store_const_for_shard(ex, shard_index)",
		"out->net++",
		"out->total++",
	} {
		if !strings.Contains(helper, needle) {
			t.Fatalf("waiter trace aggregation helper missing %q", needle)
		}
	}
}

func TestRuntimeV2OwnerLocalNetWaiterBehavior(t *testing.T) {
	source := `
#include <stdarg.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "rt_async_internal.h"

static int wake_poll_by_shard[2];
static int wake_task_calls;

void* rt_alloc(uint64_t size, uint64_t align) {
    (void)align;
    return malloc((size_t)size);
}

void rt_free(uint8_t* ptr, uint64_t size, uint64_t align) {
    (void)size;
    (void)align;
    free(ptr);
}

void* rt_realloc(uint8_t* ptr, uint64_t old_size, uint64_t new_size, uint64_t align) {
    (void)old_size;
    (void)align;
    return realloc(ptr, (size_t)new_size);
}

void panic_msg(const char* msg) {
    (void)msg;
    abort();
}

int rt_async_debug_enabled(void) {
    return 0;
}

void rt_async_debug_printf(const char* fmt, ...) {
    (void)fmt;
}

rt_runtime* rt_executor_runtime(rt_executor* ex) {
    return ex != NULL ? ex->runtime : NULL;
}

size_t rt_runtime_shard_count(const rt_runtime* runtime) {
    return runtime != NULL ? runtime->shard_count : 0;
}

rt_shard* rt_runtime_shard(rt_runtime* runtime, size_t index) {
    if (runtime == NULL || index >= runtime->shard_count || index >= RT_RUNTIME_MAX_SHARDS) {
        return NULL;
    }
    return &runtime->shards[index];
}

const rt_shard* rt_runtime_shard_const(const rt_runtime* runtime, size_t index) {
    if (runtime == NULL || index >= runtime->shard_count || index >= RT_RUNTIME_MAX_SHARDS) {
        return NULL;
    }
    return &runtime->shards[index];
}

rt_waiter_store* rt_shard_waiter_store(rt_shard* shard) {
    return shard != NULL ? &shard->waiter_store : NULL;
}

const rt_waiter_store* rt_shard_waiter_store_const(const rt_shard* shard) {
    return shard != NULL ? &shard->waiter_store : NULL;
}

rt_waiter_store* rt_executor_waiter_store_for_shard(rt_executor* ex, size_t shard_index) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), shard_index);
    return rt_shard_waiter_store(shard);
}

const rt_waiter_store* rt_executor_waiter_store_const_for_shard(const rt_executor* ex,
                                                                size_t shard_index) {
    return rt_shard_waiter_store_const(
        rt_runtime_shard_const(ex != NULL ? ex->runtime : NULL, shard_index));
}

rt_waiter_store* rt_executor_waiter_store(rt_executor* ex) {
    return rt_executor_waiter_store_for_shard(ex, 0);
}

const rt_waiter_store* rt_executor_waiter_store_const(const rt_executor* ex) {
    return rt_executor_waiter_store_const_for_shard(ex, 0);
}

rt_fd_registry* rt_executor_fd_registry_for_shard(rt_executor* ex, size_t shard_index) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), shard_index);
    return shard != NULL ? &shard->fd_registry : NULL;
}

const rt_fd_registry* rt_executor_fd_registry_const_for_shard(const rt_executor* ex,
                                                              size_t shard_index) {
    const rt_shard* shard =
        rt_runtime_shard_const(ex != NULL ? ex->runtime : NULL, shard_index);
    return shard != NULL ? &shard->fd_registry : NULL;
}

rt_fd_registry* rt_executor_fd_registry(rt_executor* ex) {
    return rt_executor_fd_registry_for_shard(ex, 0);
}

const rt_fd_registry* rt_executor_fd_registry_const(const rt_executor* ex) {
    return rt_executor_fd_registry_const_for_shard(ex, 0);
}

int rt_lane_holds_control(void) {
    return 1;
}

_Thread_local rt_worker_ctx* tls_worker_ctx;

// Epic 8 Task 6 changed rt_task_table_snapshot's signature from a struct
// pointer to a uint64_t next_id bound (rt_async_internal.h); this stub
// always returned NULL before, which made clear_accept_winner_wait_keys's
// scan a no-op in this harness (table != NULL guarded the loop) - returning
// 0 here preserves that exact no-op behavior (the loop below bound=0 never
// runs either).
uint64_t rt_task_table_snapshot(rt_executor* ex) {
    (void)ex;
    return 0;
}

static rt_task* stub_tasks[8];

rt_task* get_task(rt_executor* ex, uint64_t id) {
    (void)ex;
    if (id >= 8) {
        return NULL;
    }
    return stub_tasks[id];
}

void wake_net_task(rt_executor* ex, uint64_t id) {
    wake_task(ex, id, 0);
}

void rt_io_poll_nudge(rt_executor* ex) {
    (void)ex;
}

void rt_trace_collect_wake_batch(void) {
}

void wake_task(rt_executor* ex, uint64_t id, int remove_waiter_flag) {
    (void)remove_waiter_flag;
    rt_task* task = get_task(ex, id);
    if (task != NULL) {
        task_status_store(task, TASK_READY);
    }
    wake_task_calls++;
}

void rt_task_set_placement(rt_task* task, uint32_t shard_id, uint8_t placement_class) {
    if (task == NULL) {
        return;
    }
    task->owner_shard_id = shard_id;
    task->owner_shard_valid = 1;
    task->placement_class = placement_class;
}

uint32_t rt_channel_owner_shard_id(const rt_channel* ch) {
    (void)ch;
    return 0;
}

rt_shard* rt_task_owner_shard(rt_executor* ex, const rt_task* task) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    if (runtime != NULL && task != NULL && task->owner_shard_valid != 0) {
        return rt_runtime_shard(runtime, task->owner_shard_id);
    }
    return rt_runtime_shard(runtime, 0);
}

void rt_task_replace_owner(rt_executor* ex,
                           rt_task* task,
                           uint32_t shard_id,
                           uint8_t placement_class) {
    if (task == NULL) {
        return;
    }
    uint32_t old_shard_id = task->owner_shard_valid != 0 ? task->owner_shard_id : 0;
    if (old_shard_id != shard_id) {
        rt_waiter_migrate_join_waiters(ex, task->id, old_shard_id, shard_id);
    }
    rt_task_set_placement(task, shard_id, placement_class);
}

void rt_control_lock(rt_executor* ex) {
    (void)ex;
}

void rt_control_unlock(rt_executor* ex) {
    (void)ex;
}

void rt_shard_lock(rt_shard* shard) {
    (void)shard;
}

void rt_shard_unlock(rt_shard* shard) {
    (void)shard;
}

rt_shard* rt_runtime_shard0(rt_runtime* runtime) {
    return rt_runtime_shard(runtime, 0);
}

uint64_t rt_net_wake_poll_on_shard(rt_executor* ex, uint32_t owner_shard_id) {
    (void)ex;
    if (owner_shard_id < 2) {
        wake_poll_by_shard[owner_shard_id]++;
        return 1;
    }
    return 0;
}

int pthread_cond_signal(pthread_cond_t* cond) {
    (void)cond;
    return 0;
}

int pthread_cond_broadcast(pthread_cond_t* cond) {
    (void)cond;
    return 0;
}

#include "rt_fd_registry.c"
#include "rt_async_waiter.c"
#include "rt_waiter_route.c"

static int require_int(int condition, int code) {
    return condition ? 0 : code;
}

static void reset_task(rt_task* task, uint64_t id) {
    memset(task, 0, sizeof(*task));
    task->id = id;
    task_status_store(task, TASK_WAITING);
}

int main(void) {
    rt_runtime runtime;
    rt_executor ex;
    rt_task task;
    rt_task peer_task;
    rt_task** tasks = stub_tasks;
    memset(&runtime, 0, sizeof(runtime));
    memset(&ex, 0, sizeof(ex));
    memset(stub_tasks, 0, sizeof(stub_tasks));
    runtime.shard_count = 2;
    ex.runtime = &runtime;
    reset_task(&task, 7);
    reset_task(&peer_task, 6);
    tasks[6] = &peer_task;
    tasks[7] = &task;

    int err = require_int(rt_fd_registry_init(&runtime.shards[0].fd_registry) ==
                              RT_RUNTIME_STATUS_OK,
                          1);
    if (err != 0) return err;
    err = require_int(rt_fd_registry_init(&runtime.shards[1].fd_registry) ==
                          RT_RUNTIME_STATUS_OK,
                      2);
    if (err != 0) return err;
    err = require_int(rt_fd_registry_register_open_fd(&runtime.shards[1].fd_registry, 42) ==
                          RT_RUNTIME_STATUS_OK,
                      3);
    if (err != 0) return err;

    waker_key read_key = net_read_key(42);
    add_waiter(&ex, read_key, 7);
    err = require_int(runtime.shards[0].waiter_store.len == 0 &&
                          runtime.shards[0].waiter_store.net_len == 0,
                      4);
    if (err != 0) return err;
    err = require_int(runtime.shards[1].waiter_store.len == 1 &&
                          runtime.shards[1].waiter_store.net_len == 1,
                      5);
    if (err != 0) return err;
    err = require_int(rt_fd_registry_net_interest_present(&runtime.shards[1].fd_registry,
                                                          read_key),
                      6);
    if (err != 0) return err;
    err = require_int(wake_poll_by_shard[0] == 0 && wake_poll_by_shard[1] == 1, 7);
    if (err != 0) return err;

    add_waiter(&ex, join_key(55), 7);
    err = require_int(runtime.shards[0].waiter_store.len == 1 &&
                          runtime.shards[1].waiter_store.len == 1,
                      8);
    if (err != 0) return err;

    remove_waiter(&ex, read_key, 7);
    err = require_int(runtime.shards[1].waiter_store.len == 0 &&
                          runtime.shards[1].waiter_store.net_len == 0,
                      9);
    if (err != 0) return err;
    err = require_int(!rt_fd_registry_net_interest_present(&runtime.shards[1].fd_registry,
                                                           read_key),
                      10);
    if (err != 0) return err;
    err = require_int(runtime.shards[0].waiter_store.len == 1, 11);
    if (err != 0) return err;

    reset_task(&task, 7);
    add_waiter(&ex, read_key, 7);
    rt_waiter_completion completion =
        rt_executor_wake_net_waiters_for_key_on_owner(&ex, read_key, 1);
    err = require_int(completion.removed == 1 && completion.woken == 1 &&
                          wake_task_calls == 1,
                      12);
    if (err != 0) return err;
    err = require_int(runtime.shards[1].waiter_store.len == 0 &&
                          runtime.shards[0].waiter_store.len == 1,
                      13);
    if (err != 0) return err;

    reset_task(&task, 7);
    err = require_int(rt_fd_registry_register_open_fd(&runtime.shards[1].fd_registry, 77) ==
                          RT_RUNTIME_STATUS_OK,
                      14);
    if (err != 0) return err;
    waker_key stale_key = net_read_key(77);
    add_waiter(&ex, stale_key, 7);
    rt_fd_lifecycle_snapshot stale_snapshot;
    err = require_int(rt_fd_registry_mark_closed(&runtime.shards[1].fd_registry,
                                                 77,
                                                 &stale_snapshot) == RT_RUNTIME_STATUS_OK,
                      15);
    if (err != 0) return err;
    err = require_int(rt_fd_registry_register_open_fd(&runtime.shards[0].fd_registry, 77) ==
                          RT_RUNTIME_STATUS_OK,
                      16);
    if (err != 0) return err;
    add_waiter(&ex, stale_key, 6);
    remove_waiter(&ex, stale_key, 7);
    err = require_int(runtime.shards[1].waiter_store.len == 0, 17);
    if (err != 0) return err;
    err = require_int(rt_fd_registry_net_interest_present(&runtime.shards[0].fd_registry,
                                                          stale_key),
                      18);
    if (err != 0) return err;
    err = require_int(runtime.shards[0].waiter_store.net_len == 1, 19);
    if (err != 0) return err;
    remove_waiter(&ex, stale_key, 6);

    reset_task(&task, 7);
    err = require_int(rt_fd_registry_register_open_fd(&runtime.shards[1].fd_registry, 88) ==
                          RT_RUNTIME_STATUS_OK,
                      20);
    if (err != 0) return err;
    waker_key close_key = net_read_key(88);
    add_waiter(&ex, close_key, 7);
    rt_fd_lifecycle_snapshot close_snapshot;
    err = require_int(rt_fd_registry_mark_closed(&runtime.shards[1].fd_registry,
                                                 88,
                                                 &close_snapshot) == RT_RUNTIME_STATUS_OK,
                      21);
    if (err != 0) return err;
    close_snapshot.owner_shard_id = 1;
    rt_fd_completion_summary fd_completion =
        rt_fd_registry_wake_closed_net_waiters(&ex, &close_snapshot);
    err = require_int(fd_completion.calls == 1 && fd_completion.woken == 1 &&
                          runtime.shards[1].waiter_store.len == 0,
                      22);
    if (err != 0) return err;

    reset_task(&task, 7);
    err = require_int(rt_fd_registry_register_open_fd(&runtime.shards[1].fd_registry, 99) ==
                          RT_RUNTIME_STATUS_OK,
                      23);
    if (err != 0) return err;
    waker_key shutdown_key = net_read_key(99);
    add_waiter(&ex, shutdown_key, 7);
    fd_completion = rt_fd_registry_drain_shutdown_net_waiters_locked_on_owner(
        &ex, &runtime.shards[1].fd_registry, 1);
    err = require_int(fd_completion.calls == 1 && fd_completion.woken == 1 &&
                          runtime.shards[1].waiter_store.len == 0 &&
                          rt_fd_registry_len(&runtime.shards[1].fd_registry) == 0,
                      24);
    if (err != 0) return err;

    rt_fd_registry_free(&runtime.shards[0].fd_registry);
    rt_fd_registry_free(&runtime.shards[1].fd_registry);
    return 0;
}
`

	runFDRegistryBehaviorCheck(t, "Runtime V2 owner-local net waiter behavior", source)
}
