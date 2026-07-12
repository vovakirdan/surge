//go:build runtime_v2_pending

package vm_test

import "testing"

func TestRuntimeV2FDRegistryCloseWakePollNotificationProof(t *testing.T) {
	source := `
#include <stdint.h>
#include <stdlib.h>

#include "rt_async_internal.h"

static int wake_poll_calls;
static int wake_poll_shard_7_calls;
static int cond_broadcast_calls;

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

int waker_valid(waker_key key) {
    return key.kind != WAKER_NONE && key.id != 0;
}

int waker_is_net(waker_key key) {
    return key.kind == WAKER_NET_ACCEPT || key.kind == WAKER_NET_READ ||
           key.kind == WAKER_NET_WRITE;
}

waker_key net_accept_key(int fd) {
    return (waker_key){WAKER_NET_ACCEPT, (uint64_t)fd, 0};
}

waker_key net_read_key(int fd) {
    return (waker_key){WAKER_NET_READ, (uint64_t)fd, 0};
}

waker_key net_write_key(int fd) {
    return (waker_key){WAKER_NET_WRITE, (uint64_t)fd, 0};
}

const rt_fd_registry* rt_executor_fd_registry_const(const rt_executor* ex) {
    (void)ex;
    return NULL;
}

const rt_fd_registry* rt_executor_fd_registry_const_for_shard(const rt_executor* ex,
                                                              size_t shard_index) {
    (void)shard_index;
    return rt_executor_fd_registry_const(ex);
}

rt_waiter_completion rt_executor_wake_net_waiters_for_key(rt_executor* ex, waker_key key) {
    (void)ex;
    return (rt_waiter_completion){waker_valid(key) && waker_is_net(key) ? 1U : 0U, 1U};
}

rt_waiter_completion rt_executor_wake_net_waiters_for_key_on_owner(rt_executor* ex,
                                                                   waker_key key,
                                                                   uint32_t owner_shard_id) {
    (void)owner_shard_id;
    return rt_executor_wake_net_waiters_for_key(ex, key);
}

void rt_control_lock(rt_executor* ex) {
    (void)ex;
}

void rt_control_unlock(rt_executor* ex) {
    (void)ex;
}

uint64_t rt_net_wake_poll_on_shard(rt_executor* ex, uint32_t owner_shard_id) {
    (void)ex;
    wake_poll_calls++;
    if (owner_shard_id == 7) {
        wake_poll_shard_7_calls++;
    }
    return 1;
}

uint64_t rt_net_wake_poll_all_shards(rt_executor* ex) {
    (void)ex;
    return 0;
}

int pthread_cond_broadcast(pthread_cond_t* cond) {
    (void)cond;
    cond_broadcast_calls++;
    return 0;
}

void rt_io_poll_nudge(rt_executor* ex) {
    (void)ex;
    cond_broadcast_calls++;
}

rt_runtime* rt_executor_runtime(rt_executor* ex) {
    (void)ex;
    return NULL;
}

rt_shard* rt_runtime_shard(rt_runtime* runtime, size_t index) {
    (void)runtime;
    (void)index;
    return NULL;
}

void rt_shard_lock(rt_shard* shard) {
    (void)shard;
}

void rt_shard_unlock(rt_shard* shard) {
    (void)shard;
}
#include "rt_fd_registry.c"

static int require_int(int condition, int code) {
    return condition ? 0 : code;
}

int main(void) {
    rt_executor ex;
    rt_fd_lifecycle_snapshot read_closed = {42, 7, 7, 0, 1, 0};
    rt_fd_lifecycle_snapshot empty_closed = {43, 8, 8, 0, 0, 0};

    rt_fd_completion_summary summary =
        rt_fd_registry_wake_closed_net_waiters(&ex, &read_closed);
    int err = require_int(summary.calls == 1 && summary.woken == 1, 1);
    if (err != 0) return err;
    err = require_int(wake_poll_calls == 1, 2);
    if (err != 0) return err;
    err = require_int(wake_poll_shard_7_calls == 1, 6);
    if (err != 0) return err;
    err = require_int(cond_broadcast_calls == 1, 3);
    if (err != 0) return err;

    summary = rt_fd_registry_wake_closed_net_waiters(&ex, &empty_closed);
    err = require_int(summary.calls == 0 && summary.woken == 0, 4);
    if (err != 0) return err;
    err = require_int(wake_poll_calls == 1 && cond_broadcast_calls == 1, 5);
    if (err != 0) return err;
    return 0;
}
`

	runFDRegistryBehaviorCheck(t, "Runtime V2 fd registry close wake-poll notification check", source)
}

func TestRuntimeV2FDRegistryShutdownDrainStaticContract(t *testing.T) {
	source := `
#include "rt_async_internal.h"

// expected-red contract: shutdown must have an explicit,
// status-returning entry point and a separate fd-registry net-waiter drain
// hook visible from rt_async_internal.h. The names follow the existing
// rt_executor_* owner-first helper style and keep shutdown ownership out of
// rt_net.c callers.
rt_runtime_status (*runtime_v2_check_executor_request_shutdown)(rt_executor*) =
    rt_executor_request_shutdown;
rt_runtime_status (*runtime_v2_check_executor_drain_shutdown_net_waiters)(rt_executor*) =
    rt_executor_drain_shutdown_net_waiters;

_Static_assert(RT_RUNTIME_STATUS_OK == 0, "shutdown contract must use rt_runtime_status");
`

	runFDRegistryStaticCheck(t, "Runtime V2 fd registry shutdown drain static contract", source)
}

func TestRuntimeV2FDRegistryShutdownDrainBehavior(t *testing.T) {
	source := `
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "rt_async_internal.h"

static rt_fd_registry registries[3];
static int wake_poll_calls;
static int wake_poll_by_shard[3];
static int wake_all_calls;
static int cond_broadcast_calls;
static int lock_calls;
static int unlock_calls;
static int read_calls;
static int accept_calls;
static int write_calls;
static uint64_t trace_shutdown_wakeups;

size_t rt_runtime_shard_count(const rt_runtime* runtime) {
    (void)runtime;
    return 3;
}

rt_runtime* rt_executor_runtime(rt_executor* ex) {
    (void)ex;
    return NULL;
}

int rt_exec_trace_enabled(void) {
    return 1;
}

void rt_net_trace_shutdown_poller_wakeups_enabled(uint64_t wakeups) {
    trace_shutdown_wakeups = wakeups;
}

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

int waker_valid(waker_key key) {
    return key.kind != WAKER_NONE && key.id != 0;
}

int waker_is_net(waker_key key) {
    return key.kind == WAKER_NET_ACCEPT || key.kind == WAKER_NET_READ ||
           key.kind == WAKER_NET_WRITE;
}

waker_key net_accept_key(int fd) {
    return (waker_key){WAKER_NET_ACCEPT, (uint64_t)fd, 0};
}

waker_key net_read_key(int fd) {
    return (waker_key){WAKER_NET_READ, (uint64_t)fd, 0};
}

waker_key net_write_key(int fd) {
    return (waker_key){WAKER_NET_WRITE, (uint64_t)fd, 0};
}

rt_fd_registry* rt_executor_fd_registry(rt_executor* ex) {
    (void)ex;
    return &registries[0];
}

rt_fd_registry* rt_executor_fd_registry_for_shard(rt_executor* ex, size_t shard_index) {
    (void)ex;
    return shard_index < 3 ? &registries[shard_index] : NULL;
}

const rt_fd_registry* rt_executor_fd_registry_const(const rt_executor* ex) {
    (void)ex;
    return &registries[0];
}

const rt_fd_registry* rt_executor_fd_registry_const_for_shard(const rt_executor* ex,
                                                              size_t shard_index) {
    (void)ex;
    return shard_index < 3 ? &registries[shard_index] : NULL;
}

rt_waiter_completion rt_executor_wake_net_waiters_for_key(rt_executor* ex, waker_key key) {
    (void)ex;
    if (!waker_valid(key) || !waker_is_net(key)) {
        return (rt_waiter_completion){0, 0};
    }
    if (key.kind == WAKER_NET_READ && key.id == 10) read_calls++;
    if (key.kind == WAKER_NET_ACCEPT && key.id == 10) accept_calls++;
    if (key.kind == WAKER_NET_WRITE && key.id == 11) write_calls++;
    return (rt_waiter_completion){1, 1};
}

rt_waiter_completion rt_executor_wake_net_waiters_for_key_on_owner(rt_executor* ex,
                                                                   waker_key key,
                                                                   uint32_t owner_shard_id) {
    (void)owner_shard_id;
    return rt_executor_wake_net_waiters_for_key(ex, key);
}

void rt_control_lock(rt_executor* ex) {
    (void)ex;
    lock_calls++;
}

void rt_control_unlock(rt_executor* ex) {
    (void)ex;
    unlock_calls++;
}

uint64_t rt_net_wake_poll_on_shard(rt_executor* ex, uint32_t owner_shard_id) {
    (void)ex;
    wake_poll_calls++;
    if (owner_shard_id < 3) {
        wake_poll_by_shard[owner_shard_id]++;
    }
    return owner_shard_id < 3 ? 1U : 0U;
}

uint64_t rt_net_wake_poll_all_shards(rt_executor* ex) {
    (void)ex;
    wake_all_calls++;
    uint64_t woken = 0;
    for (uint32_t i = 0; i < 3; i++) {
        woken += rt_net_wake_poll_on_shard(ex, i);
    }
    return woken;
}

int pthread_cond_broadcast(pthread_cond_t* cond) {
    (void)cond;
    cond_broadcast_calls++;
    return 0;
}

int pthread_mutex_lock(pthread_mutex_t* mutex) {
    (void)mutex;
    return 0;
}

int pthread_mutex_unlock(pthread_mutex_t* mutex) {
    (void)mutex;
    return 0;
}

static uint32_t sched_broadcast_calls;
static uint32_t transport_shutdown_wake_calls;

uint64_t rt_transport_shutdown_wake_all(rt_executor* ex) {
    (void)ex;
    transport_shutdown_wake_calls++;
    return 0;
}

void rt_sched_wake_broadcast_all(rt_executor* ex) {
    (void)ex;
    sched_broadcast_calls++;
}

void rt_io_poll_nudge(rt_executor* ex) {
    (void)ex;
    cond_broadcast_calls++;
}

rt_shard* rt_runtime_shard(rt_runtime* runtime, size_t index) {
    (void)runtime;
    (void)index;
    return NULL;
}

void rt_shard_lock(rt_shard* shard) {
    (void)shard;
}

void rt_shard_unlock(rt_shard* shard) {
    (void)shard;
}

#include "rt_remote_spawn.h"

// Transport teardown stubs: the shutdown path releases outstanding far-task
// leases and drains inbound transport queues; this harness proves only the
// fd-registry drain behavior, so both are no-ops here.
void rt_far_task_release_all(rt_executor* ex) {
    (void)ex;
}

void rt_far_channel_release_all(rt_executor* ex) {
    (void)ex;
}

size_t rt_remote_spawn_drain_inbound_locked(rt_executor* ex, rt_shard* shard, size_t limit) {
    (void)ex;
    (void)shard;
    (void)limit;
    return 0;
}

void rt_remote_spawn_fail_all_pending(rt_executor* ex, rt_remote_spawn_status status) {
    (void)ex;
    (void)status;
}
#include "rt_fd_registry.c"
#include "rt_shutdown.c"

static int require_int(int condition, int code) {
    return condition ? 0 : code;
}

int main(void) {
    rt_executor ex;
    memset(&ex, 0, sizeof(ex));
    waker_key read_key = {WAKER_NET_READ, 10, 0};
    waker_key accept_key = {WAKER_NET_ACCEPT, 10, 0};
    waker_key write_key = {WAKER_NET_WRITE, 11, 0};

    int err = require_int(rt_fd_registry_init(&registries[0]) == RT_RUNTIME_STATUS_OK, 1);
    if (err != 0) return err;
    err = require_int(rt_fd_registry_init(&registries[1]) == RT_RUNTIME_STATUS_OK, 16);
    if (err != 0) return err;
    err = require_int(rt_fd_registry_init(&registries[2]) == RT_RUNTIME_STATUS_OK, 17);
    if (err != 0) return err;
    err = require_int(rt_fd_registry_attach_net_interest(&registries[1], read_key) ==
                          RT_RUNTIME_STATUS_OK,
                      2);
    if (err != 0) return err;
    err = require_int(rt_fd_registry_attach_net_interest(&registries[1], accept_key) ==
                          RT_RUNTIME_STATUS_OK,
                      3);
    if (err != 0) return err;
    err = require_int(rt_fd_registry_attach_net_interest(&registries[2], write_key) ==
                          RT_RUNTIME_STATUS_OK,
                      4);
    if (err != 0) return err;
    err = require_int(rt_fd_registry_len(&registries[0]) == 0 &&
                          rt_fd_registry_len(&registries[1]) == 1 &&
                          rt_fd_registry_len(&registries[2]) == 1,
                      5);
    if (err != 0) return err;

    err = require_int(rt_executor_drain_shutdown_net_waiters(&ex) == RT_RUNTIME_STATUS_OK, 6);
    if (err != 0) return err;
    err = require_int(read_calls == 1 && accept_calls == 1 && write_calls == 1, 7);
    if (err != 0) return err;
    err = require_int(rt_fd_registry_len(&registries[0]) == 0 &&
                          rt_fd_registry_len(&registries[1]) == 0 &&
                          rt_fd_registry_len(&registries[2]) == 0,
                      8);
    if (err != 0) return err;
    err = require_int(!rt_fd_registry_net_interest_present(&registries[1], read_key) &&
                          !rt_fd_registry_net_interest_present(&registries[1], accept_key) &&
                          !rt_fd_registry_net_interest_present(&registries[2], write_key),
                      9);
    if (err != 0) return err;
    err = require_int(wake_poll_calls == 2 && wake_poll_by_shard[0] == 0 &&
                          wake_poll_by_shard[1] == 1 && wake_poll_by_shard[2] == 1 &&
                          cond_broadcast_calls == 2,
                      10);
    if (err != 0) return err;
    err = require_int(lock_calls == 1 && unlock_calls == 1, 11);
    if (err != 0) return err;

    err = require_int(rt_executor_drain_shutdown_net_waiters(&ex) == RT_RUNTIME_STATUS_OK, 12);
    if (err != 0) return err;
    err = require_int(read_calls == 1 && accept_calls == 1 && write_calls == 1, 13);
    if (err != 0) return err;
    err = require_int(wake_poll_calls == 2 && cond_broadcast_calls == 2, 14);
    if (err != 0) return err;
    err = require_int(lock_calls == 2 && unlock_calls == 2, 15);
    if (err != 0) return err;

    err = require_int(rt_executor_request_shutdown(&ex) == RT_RUNTIME_STATUS_OK, 18);
    if (err != 0) return err;
    err = require_int(wake_all_calls == 1 && wake_poll_by_shard[0] == 1 &&
                          wake_poll_by_shard[1] == 2 && wake_poll_by_shard[2] == 2,
                      19);
    if (err != 0) return err;
    err = require_int(trace_shutdown_wakeups == 3, 20);
    if (err != 0) return err;
    err = require_int(transport_shutdown_wake_calls == 1, 21);
    if (err != 0) return err;
    err = require_int(sched_broadcast_calls == 1, 22);
    if (err != 0) return err;

    rt_fd_registry_free(&registries[0]);
    rt_fd_registry_free(&registries[1]);
    rt_fd_registry_free(&registries[2]);
    return 0;
}
`

	runFDRegistryBehaviorCheck(t, "Runtime V2 fd registry shutdown drain behavior check", source)
}
