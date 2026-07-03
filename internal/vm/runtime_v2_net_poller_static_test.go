//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeV2NetPollerPerShardWakeShape(t *testing.T) {
	root := repoRoot(t)
	netBytes, err := os.ReadFile(filepath.Join(root, "runtime", "native", "rt_net.c"))
	if err != nil {
		t.Fatalf("read rt_net.c: %v", err)
	}
	pollerBytes, err := os.ReadFile(filepath.Join(root, "runtime", "native", "rt_net_poller.c"))
	if err != nil {
		t.Fatalf("read rt_net_poller.c: %v", err)
	}
	source := string(netBytes) + "\n" + string(pollerBytes)
	for _, forbidden := range []string{
		"net_poll_wake_read_fd",
		"net_poll_wake_write_fd",
		"void rt_net_wake_poll(void)",
		"int poll_net_waiters(rt_executor* ex",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("rt_net.c must not keep process-global poller wake surface %q", forbidden)
		}
	}

	wakeBody, ok := cFunctionBody(source, "rt_net_wake_poll_on_shard")
	if !ok {
		t.Fatal("rt_net_wake_poll_on_shard not found")
	}
	for _, required := range []string{
		"rt_runtime_shard(rt_executor_runtime(ex), owner_shard_id)",
		"rt_net_poll_wake_init(shard)",
		"shard->net_poll_wake.write_fd",
		"return 1",
	} {
		if !strings.Contains(wakeBody, required) {
			t.Fatalf("owner-shard wake body missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"rt_runtime_shard_count",
		"rt_net_wake_poll_all_shards",
		"for (",
	} {
		if strings.Contains(wakeBody, forbidden) {
			t.Fatalf("owner-shard wake must not broadcast or loop across shards; found %q", forbidden)
		}
	}
}

func TestRuntimeV2NetPollerShardLocalPollInput(t *testing.T) {
	root := repoRoot(t)
	sourceBytes, err := os.ReadFile(filepath.Join(root, "runtime", "native", "rt_net.c"))
	if err != nil {
		t.Fatalf("read rt_net.c: %v", err)
	}
	body, ok := cFunctionBody(string(sourceBytes), "poll_net_waiters_on_shard")
	if !ok {
		t.Fatal("poll_net_waiters_on_shard not found")
	}
	for _, required := range []string{
		"rt_runtime_shard(rt_executor_runtime(ex), owner_shard_id)",
		"rt_executor_fd_registry_const_for_shard(ex, owner_shard_id)",
		"rt_shard_net_poll_scratch(shard)",
		"fds[i].owner_shard_id = owner_shard_id",
		"shard->net_poll_wake.read_fd",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("shard-local poll body missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"cap +=",
		"for (size_t i = 0; i < shard_count",
		"rt_executor_net_poll_scratch_for_shard(ex, 0)",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("poll_net_waiters_on_shard must not aggregate shard registries or use shard 0 scratch; found %q", forbidden)
		}
	}
}

func TestRuntimeV2NetPollerPerShardWakeBehavior(t *testing.T) {
	source := `
#include <errno.h>
#include <stdint.h>
#include <string.h>
#include <unistd.h>

#include "rt_async_internal.h"

static rt_runtime runtime;
static rt_fd_entry shard0_entries[1];
static rt_fd_entry shard1_entries[1];
static int cond_signal_calls;

rt_runtime* rt_executor_runtime(rt_executor* ex) {
    return ex != NULL ? ex->runtime : NULL;
}

rt_shard* rt_runtime_shard(rt_runtime* rt, size_t index) {
    if (rt == NULL || index >= rt->shard_count || index >= RT_RUNTIME_MAX_SHARDS) {
        return NULL;
    }
    return &rt->shards[index];
}

const rt_shard* rt_runtime_shard_const(const rt_runtime* rt, size_t index) {
    if (rt == NULL || index >= rt->shard_count || index >= RT_RUNTIME_MAX_SHARDS) {
        return NULL;
    }
    return &rt->shards[index];
}

size_t rt_runtime_shard_count(const rt_runtime* rt) {
    return rt != NULL ? rt->shard_count : 0;
}

const rt_fd_registry* rt_executor_fd_registry_const_for_shard(const rt_executor* ex,
                                                              size_t shard_index) {
    const rt_runtime* rt = ex != NULL ? ex->runtime : NULL;
    const rt_shard* shard = rt_runtime_shard_const(rt, shard_index);
    return shard != NULL ? &shard->fd_registry : NULL;
}

int poll_net_waiters_on_shard(rt_executor* ex, uint32_t owner_shard_id, int timeout_ms) {
    (void)ex;
    (void)owner_shard_id;
    (void)timeout_ms;
    return 7;
}

int pthread_cond_signal(pthread_cond_t* cond) {
    (void)cond;
    cond_signal_calls++;
    return 0;
}

int waker_is_net(waker_key key) {
    return key.kind == WAKER_NET_ACCEPT || key.kind == WAKER_NET_READ ||
           key.kind == WAKER_NET_WRITE;
}

uint32_t rt_net_owner_shard_for_key(rt_executor* ex, waker_key key, uint32_t fallback_shard_id) {
    (void)ex;
    if (key.id == 10) {
        return 0;
    }
    if (key.id == 11) {
        return 1;
    }
    return fallback_shard_id;
}

#include "rt_net_poller.c"

static int require_int(int condition, int code) {
    return condition ? 0 : code;
}

static int read_one(int fd) {
    uint8_t byte = 0;
    ssize_t n = read(fd, &byte, 1);
    if (n == 1) {
        return 1;
    }
    if (n < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
        return 0;
    }
    return -1;
}

int main(void) {
    rt_executor ex;
    memset(&ex, 0, sizeof(ex));
    memset(&runtime, 0, sizeof(runtime));
    runtime.shard_count = 2;
    ex.runtime = &runtime;
    for (size_t i = 0; i < runtime.shard_count; i++) {
        runtime.shards[i].runtime = &runtime;
        runtime.shards[i].executor = &ex;
        runtime.shards[i].shard_id = (uint32_t)i;
        runtime.shards[i].net_poll_wake.read_fd = -1;
        runtime.shards[i].net_poll_wake.write_fd = -1;
    }
    shard0_entries[0] = (rt_fd_entry){10, 1, RT_FD_CLOSE_STATE_OPEN, 1, 0, 0, 0};
    shard1_entries[0] = (rt_fd_entry){11, 1, RT_FD_CLOSE_STATE_OPEN, 1, 0, 1, 0};
    runtime.shards[0].fd_registry.entries = shard0_entries;
    runtime.shards[0].fd_registry.len = 1;
    runtime.shards[0].fd_registry.cap = 1;
    runtime.shards[1].fd_registry.entries = shard1_entries;
    runtime.shards[1].fd_registry.len = 1;
    runtime.shards[1].fd_registry.cap = 1;

    int err = require_int(rt_net_poll_wake_init(&runtime.shards[0]) == 1, 1);
    if (err != 0) return err;
    err = require_int(rt_net_poll_wake_init(&runtime.shards[1]) == 1, 2);
    if (err != 0) return err;
    err = require_int(rt_net_has_waiters_on_shard(&ex, 0) == 0, 3);
    if (err != 0) return err;
    err = require_int(rt_net_has_waiters_on_shard(&ex, 1) == 1, 4);
    if (err != 0) return err;
    err = require_int(rt_net_begin_poll_on_shard(&ex, 0) == 0, 5);
    if (err != 0) return err;
    err = require_int(rt_net_begin_poll_on_shard(&ex, 1) == 1, 6);
    if (err != 0) return err;
    err = require_int(runtime.shards[0].net_polling == 0 && runtime.shards[1].net_polling == 1, 7);
    if (err != 0) return err;
    err = require_int(rt_net_poll_waiters_owned_on_shard(&ex, 1, 0) == 7, 8);
    if (err != 0) return err;
    err = require_int(runtime.shards[1].net_polling == 0 && cond_signal_calls == 1, 9);
    if (err != 0) return err;

    err = require_int(rt_net_wake_poll_on_shard(&ex, 1) == 1, 10);
    if (err != 0) return err;
    err = require_int(read_one(runtime.shards[0].net_poll_wake.read_fd) == 0, 11);
    if (err != 0) return err;
    err = require_int(read_one(runtime.shards[1].net_poll_wake.read_fd) == 1, 12);
    if (err != 0) return err;

    err = require_int(rt_net_wake_poll_all_shards(&ex) == 2, 13);
    if (err != 0) return err;
    err = require_int(read_one(runtime.shards[0].net_poll_wake.read_fd) == 1, 14);
    if (err != 0) return err;
    err = require_int(read_one(runtime.shards[1].net_poll_wake.read_fd) == 1, 15);
    if (err != 0) return err;

    waker_key wait_keys[2] = {
        {WAKER_NET_ACCEPT, 10},
        {WAKER_NET_ACCEPT, 11},
    };
    rt_task task;
    memset(&task, 0, sizeof(task));
    task.wait_keys = wait_keys;
    task.wait_keys_len = 2;
    err = require_int(rt_net_wake_poll_for_task_wait_keys(&ex, &task, wait_keys[0]) == 2, 16);
    if (err != 0) return err;
    err = require_int(read_one(runtime.shards[0].net_poll_wake.read_fd) == 1, 17);
    if (err != 0) return err;
    err = require_int(read_one(runtime.shards[1].net_poll_wake.read_fd) == 1, 18);
    if (err != 0) return err;

    rt_net_poll_wake_close(&runtime.shards[0].net_poll_wake);
    rt_net_poll_wake_close(&runtime.shards[1].net_poll_wake);
    return 0;
}
`

	runFDRegistryBehaviorCheck(t, "Runtime V2 net poller per-shard wake behavior", source)
}

func TestRuntimeV2NetPollerGlobalIOThreadDoesNotOwnMultiShardNetPolling(t *testing.T) {
	root := repoRoot(t)
	sourceBytes, err := os.ReadFile(filepath.Join(root, "runtime", "native", "rt_async_state.c"))
	if err != nil {
		t.Fatalf("read rt_async_state.c: %v", err)
	}
	source := string(sourceBytes)
	if !strings.Contains(source, "static void* rt_io_main(void* arg) {") {
		t.Fatal("rt_io_main definition not found")
	}
	if !strings.Contains(source, "shard_count <= 1 && rt_net_has_waiters_on_shard(ex, 0)") {
		t.Fatalf("rt_io_main must gate net polling to single-shard compatibility")
	}
}

func TestRuntimeV2NetPollerShutdownWakesEveryShard(t *testing.T) {
	root := repoRoot(t)
	shutdownBytes, err := os.ReadFile(filepath.Join(root, "runtime", "native", "rt_shutdown.c"))
	if err != nil {
		t.Fatalf("read rt_shutdown.c: %v", err)
	}
	requestBody, ok := cFunctionBody(string(shutdownBytes), "rt_executor_request_shutdown")
	if !ok {
		t.Fatal("rt_executor_request_shutdown not found")
	}
	for _, required := range []string{
		"rt_net_wake_poll_all_shards(ex)",
		"pthread_cond_broadcast(&ex->ready_cv)",
		"pthread_cond_broadcast(&ex->io_cv)",
	} {
		if !strings.Contains(requestBody, required) {
			t.Fatalf("shutdown request body missing %q", required)
		}
	}

	netBytes, err := os.ReadFile(filepath.Join(root, "runtime", "native", "rt_net_poller.c"))
	if err != nil {
		t.Fatalf("read rt_net_poller.c: %v", err)
	}
	allBody, ok := cFunctionBody(string(netBytes), "rt_net_wake_poll_all_shards")
	if !ok {
		t.Fatal("rt_net_wake_poll_all_shards not found")
	}
	for _, required := range []string{
		"rt_runtime_shard_count(rt_executor_runtime(ex))",
		"for (size_t i = 0; i < shard_count; i++)",
		"woken += rt_net_wake_poll_on_shard(ex, (uint32_t)i)",
		"return woken",
	} {
		if !strings.Contains(allBody, required) {
			t.Fatalf("all-shards wake body missing %q", required)
		}
	}
}
