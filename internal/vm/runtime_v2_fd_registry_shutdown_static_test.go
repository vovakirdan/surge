//go:build runtime_v2_pending

package vm_test

import "testing"

func TestRuntimeV2FDRegistryCloseWakePollNotificationProof(t *testing.T) {
	source := `
#include <stdint.h>
#include <stdlib.h>

#include "rt_async_internal.h"

static int wake_poll_calls;
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
    return (waker_key){WAKER_NET_ACCEPT, (uint64_t)fd};
}

waker_key net_read_key(int fd) {
    return (waker_key){WAKER_NET_READ, (uint64_t)fd};
}

waker_key net_write_key(int fd) {
    return (waker_key){WAKER_NET_WRITE, (uint64_t)fd};
}

const rt_fd_registry* rt_executor_fd_registry_const(const rt_executor* ex) {
    (void)ex;
    return NULL;
}

rt_waiter_completion rt_executor_wake_net_waiters_for_key(rt_executor* ex, waker_key key) {
    (void)ex;
    return (rt_waiter_completion){waker_valid(key) && waker_is_net(key) ? 1U : 0U, 1U};
}

void rt_lock(rt_executor* ex) {
    (void)ex;
}

void rt_unlock(rt_executor* ex) {
    (void)ex;
}

void rt_net_wake_poll(void) {
    wake_poll_calls++;
}

int pthread_cond_broadcast(pthread_cond_t* cond) {
    (void)cond;
    cond_broadcast_calls++;
    return 0;
}

#include "rt_fd_registry.c"

static int require_int(int condition, int code) {
    return condition ? 0 : code;
}

int main(void) {
    rt_executor ex;
    rt_fd_lifecycle_snapshot read_closed = {42, 7, 0, 1, 0};
    rt_fd_lifecycle_snapshot empty_closed = {43, 8, 0, 0, 0};

    rt_fd_completion_summary summary =
        rt_fd_registry_wake_closed_net_waiters(&ex, &read_closed);
    int err = require_int(summary.calls == 1 && summary.woken == 1, 1);
    if (err != 0) return err;
    err = require_int(wake_poll_calls == 1, 2);
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

// Task 10 expected-red contract for Task 11: shutdown must have an explicit,
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
