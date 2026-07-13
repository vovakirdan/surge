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

func TestRuntimeV2RemotePublicationAPIShape(t *testing.T) {
	source := `
#include <stdint.h>
#include "rt_async_internal.h"
#include "rt_remote_spawn.h"
#include "rt_remote_task.h"

rt_remote_spawn_status (*check_publish)(uint32_t, int64_t, void*,
    rt_remote_spawn_pending**, rt_far_task_handle*) = rt_remote_spawn_publish;
rt_remote_spawn_status (*check_publish_placement)(rt_placement, int64_t, void*,
    rt_remote_spawn_pending**, rt_far_task_handle*) = rt_remote_spawn_publish_placement;
rt_remote_spawn_status (*check_validate)(rt_executor*, const rt_far_task_handle*) =
    rt_remote_spawn_handle_validate;
size_t (*check_drain)(rt_executor*, rt_shard*, size_t) =
    rt_remote_spawn_drain_inbound_locked;
rt_remote_task_status (*check_far_await)(const rt_far_task_handle*,
    rt_remote_task_pending**, uint8_t*, uint64_t*) = rt_far_task_await;
rt_remote_task_status (*check_far_cancel)(const rt_far_task_handle*,
    rt_remote_task_pending**, uint8_t*, uint64_t*) = rt_far_task_cancel;
rt_remote_task_status (*check_far_release)(const rt_far_task_handle*) =
    rt_far_task_release;
rt_remote_spawn_status (*check_handle_alloc)(rt_far_task_handle**) =
    rt_far_task_handle_alloc;
rt_runtime_status (*check_remote_task_state_destroy)(rt_executor*) =
    rt_remote_task_state_destroy;

_Static_assert(RT_REMOTE_SPAWN_STATUS_OK == 0, "OK status must stay zero");
_Static_assert(RT_REMOTE_SPAWN_STATUS_PENDING != RT_REMOTE_SPAWN_STATUS_OK,
               "pending must not look successful");
_Static_assert(RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN != RT_REMOTE_SPAWN_STATUS_OK,
               "shutdown must not look successful");
_Static_assert(RT_REMOTE_SPAWN_STATUS_QUEUE_FULL != RT_REMOTE_SPAWN_STATUS_OK,
               "queue-full must not look successful");
_Static_assert(RT_REMOTE_SPAWN_STATUS_STALE_TOKEN != RT_REMOTE_SPAWN_STATUS_OK,
               "stale token must not look successful");
_Static_assert(RT_REMOTE_SPAWN_STATUS_UNSUPPORTED_PLACEMENT != RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT,
               "unsupported placement must not look like missing async context");
_Static_assert(RT_REMOTE_SPAWN_STATUS_INVALID_PLACEMENT != RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT,
               "invalid placement must not look like missing async context");
_Static_assert(RT_REMOTE_TASK_STATUS_OK == 0, "remote task OK status must stay zero");
_Static_assert(RT_REMOTE_TASK_STATUS_PENDING != RT_REMOTE_TASK_STATUS_OK,
               "remote task pending must not look successful");
_Static_assert(RT_REMOTE_TASK_STATUS_CONSUMED != RT_REMOTE_TASK_STATUS_STALE_TOKEN,
               "consumed handle must not look stale");
_Static_assert(sizeof(rt_far_task_handle) == 24,
               "LLVM far Task handle allocation assumes exact native handle size");
_Static_assert(_Alignof(rt_far_task_handle) == 8,
               "LLVM far Task handle allocation assumes exact native handle alignment");
_Static_assert(sizeof(rt_far_task_handle) >= sizeof(uint64_t) * 2,
               "far task handle must carry id and generation");
_Static_assert(sizeof(((rt_far_task_handle*)0)->task_id) == sizeof(uint64_t),
               "task id must stay uint64_t");
_Static_assert(sizeof(((rt_far_task_handle*)0)->generation) == sizeof(uint64_t),
               "generation token must stay uint64_t");
_Static_assert(sizeof(((rt_far_task_handle*)0)->owner_shard_id) == sizeof(uint32_t),
               "owner shard id must stay uint32_t");
_Static_assert(sizeof(((rt_task*)0)->generation) == sizeof(uint64_t),
               "task birth generation must be stored on the task");
_Static_assert(sizeof(((rt_task*)0)->remote_handle_state) == sizeof(_Atomic uint8_t),
               "remote task handle consume state must be atomic");
_Static_assert(WAKER_REMOTE_SPAWN_REPLY != WAKER_NONE,
               "remote spawn reply wait must have its own waiter key");
_Static_assert(WAKER_REMOTE_TASK_REPLY != WAKER_NONE,
               "remote task reply wait must have its own waiter key");
_Static_assert(RT_TRANSPORT_MSG_REMOTE_TASK_AWAIT_REQUEST != RT_TRANSPORT_MSG_NONE,
               "remote task await request must be a real control category");
_Static_assert(RT_TRANSPORT_MSG_REMOTE_TASK_RELEASE_REQUEST != RT_TRANSPORT_MSG_NONE,
               "remote task release request must be a real control category");
`

	runFDRegistryStaticCheck(t, "Runtime V2 remote publication API shape", source)
}

func TestRuntimeV2RemotePublicationBehavior(t *testing.T) {
	bin := buildRemotePublicationHarness(t)
	rows := []struct {
		name string
		mode string
		env  []string
	}{
		{
			name: "publish-other-shard-2",
			mode: "publish-other",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "publish-other-shard-8",
			mode: "publish-other",
			env:  remotePublicationEnv("SURGE_SHARDS=8", "SURGE_THREADS=8", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "self-crossing",
			mode: "self-crossing",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "queue-full",
			mode: "queue-full",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "shutdown",
			mode: "shutdown",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "stale-token",
			mode: "stale-token",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1"),
		},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, row.mode, row.env)
			if code != 0 {
				t.Fatalf("remote publication mode %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					row.mode, code, stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2RemotePublicationFailurePathStaticGuards(t *testing.T) {
	root := repoRoot(t)
	source := readTransportContractFile(t, root, "runtime/native/rt_remote_spawn.c")
	if strings.Contains(source,
		"remote_spawn_pending_finish(ex, req, RT_REMOTE_SPAWN_STATUS_OK, &handle);") {
		t.Fatal("ack enqueue failure must not complete the pending publication as OK")
	}
	publishStatus := strings.Index(source, "status = rt_remote_spawn_publish_body_task")
	ackStatus := strings.Index(source, "status = remote_spawn_enqueue_ack")
	ackFailure := strings.Index(source, "(void)rt_far_task_release(&handle);")
	if publishStatus < 0 || ackStatus < publishStatus {
		t.Fatal("destination task must be published before its OK ack becomes observable")
	}
	if ackFailure < ackStatus {
		t.Fatal("failed ack enqueue must owner-route release of the already-published task")
	}
	if !strings.Contains(source, "remote_spawn_release_msg_payload(&msg);") {
		t.Fatal("shutdown must drop queued remote-spawn transport payload refs")
	}
	if !strings.Contains(source, `panic_msg("remote spawn: unsupported transport message kind")`) {
		t.Fatal("drain must fail closed for transport kinds it does not handle")
	}
	if !strings.Contains(
		source, `panic_msg("remote spawn: unsupported transport message kind during shutdown")`) {
		t.Fatal("shutdown drain must fail closed for transport kinds it does not handle")
	}
}

func buildRemotePublicationHarness(t *testing.T) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 remote publication check: %v", err)
	}
	root := repoRoot(t)
	tmp := t.TempDir()
	harness := filepath.Join(tmp, "remote_publication_harness.c")
	bin := filepath.Join(tmp, "remote_publication_harness")
	if err := os.WriteFile(harness, []byte(remotePublicationHarness), 0o600); err != nil {
		t.Fatalf("write harness: %v", err)
	}
	sources, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob runtime sources: %v", err)
	}
	sort.Strings(sources)
	args := []string{
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Werror",
		"-DRT_TEST_SYNC_POINTS",
		"-pthread",
		"-I" + filepath.Join(root, "runtime", "native"),
		"-o",
		bin,
		harness,
	}
	for _, src := range sources {
		if filepath.Base(src) != "rt_entry.c" {
			args = append(args, src)
		}
	}
	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("build remote publication harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	return bin
}

func runRemotePublicationHarness(t *testing.T, bin, mode string, env []string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, mode)
	cmd.Env = env
	return runCommand(t, cmd, "")
}

func remotePublicationEnv(values ...string) []string {
	env := os.Environ()
	for _, value := range values {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) == 2 {
			env = overrideEnvVar(env, parts[0], parts[1])
		}
	}
	return env
}

const remotePublicationHarness = `
#define _POSIX_C_SOURCE 199309L
#include "rt_async_internal.h"
#include "rt_remote_spawn.h"
#include "rt_remote_task.h"
#include "rt_sync_point.h"
#include "rt_transport.h"

#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

enum {
    POLL_REMOTE_PUBLISHER = 7001,
    POLL_REMOTE_CHILD = 7002
};

typedef struct remote_child_state {
    _Atomic uint32_t ran;
    _Atomic uint32_t owner;
    _Atomic uint32_t worker;
} remote_child_state;

typedef struct remote_publish_state {
    rt_remote_spawn_pending* pending;
    remote_child_state* child;
    rt_far_task_handle handle;
    uint32_t dst;
    uint32_t fill_queue;
    uint32_t shutdown_first;
    uint32_t filled;
    uint32_t shutdown_done;
    uint32_t saw_pending;
    uint64_t request_id;
    rt_remote_spawn_status status;
    rt_remote_spawn_status validate_status;
    size_t children_after;
} remote_publish_state;

uint64_t __surge_blocking_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
    return 0;
}

static void sleep_us(unsigned long micros) {
    struct timespec ts;
    ts.tv_sec = (time_t)(micros / 1000000UL);
    ts.tv_nsec = (long)((micros % 1000000UL) * 1000UL);
    while (nanosleep(&ts, &ts) != 0) {
    }
}

static int fail(const char* msg) {
    if (msg != NULL) {
        fputs(msg, stderr);
        fputc('\n', stderr);
    }
    return 1;
}

static int wait_child(remote_child_state* child, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(&child->ran, memory_order_acquire) != 0) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static uint32_t pin_shard(rt_executor* ex, uint32_t wanted) {
    size_t count = rt_runtime_shard_count(rt_executor_runtime(ex));
    return count > 0 ? (uint32_t)(wanted % (uint32_t)count) : 0;
}

static int fill_data_lane(rt_executor* ex, uint32_t dst) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), dst);
    if (shard == NULL) {
        return 0;
    }
    rt_transport_msg data = {
        .kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST,
        .source_shard_id = 0,
        .target_shard_id = dst,
        .route_id = 9000,
        .generation = 1,
        .payload = NULL,
        .payload_len = 0,
    };
    for (size_t i = 0; i < RT_TRANSPORT_DATA_QUEUE_CAP; i++) {
        if (rt_transport_enqueue(shard, &data) != RT_TRANSPORT_STATUS_OK) {
            return 0;
        }
    }
    data.kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK;
    return rt_transport_enqueue(shard, &data) == RT_TRANSPORT_STATUS_OK;
}

// Drop-dispatch stub: no harness state struct carries a drop obligation
// (drop-fn id 0 never dispatches), so reaching this is a test bug.
void __surge_drop_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

void __surge_poll_call(uint64_t id) {
    if (id == POLL_REMOTE_CHILD) {
        remote_child_state* child = (remote_child_state*)__task_state();
        const rt_task* task = rt_current_task();
        atomic_store_explicit(&child->owner,
                              task != NULL ? task->owner_shard_id : UINT32_MAX,
                              memory_order_release);
        atomic_store_explicit(&child->worker, rt_debug_current_worker_shard_id(), memory_order_release);
        atomic_store_explicit(&child->ran, 1, memory_order_release);
        rt_async_return(child, 77);
        return;
    }
    if (id == POLL_REMOTE_PUBLISHER) {
        remote_publish_state* st = (remote_publish_state*)__task_state();
        rt_executor* ex = ensure_exec();
        if (st->shutdown_first && !st->shutdown_done) {
            st->shutdown_done = 1;
            (void)rt_executor_request_shutdown(ex);
        }
        if (st->fill_queue && !st->filled) {
            st->filled = 1;
            if (!fill_data_lane(ex, st->dst)) {
                rt_async_return(st, RT_REMOTE_SPAWN_STATUS_REFUSED);
                return;
            }
        }
        rt_remote_spawn_status status = rt_remote_spawn_publish(
            st->dst, POLL_REMOTE_CHILD, st->child, &st->pending, &st->handle);
        if (status == RT_REMOTE_SPAWN_STATUS_PENDING) {
            st->saw_pending = 1;
            st->request_id = rt_remote_spawn_pending_request_id(st->pending);
            rt_async_yield(st);
            return;
        }
        st->status = status;
        st->children_after = rt_current_task() != NULL ? rt_current_task()->children_len : 999;
        st->validate_status = status == RT_REMOTE_SPAWN_STATUS_OK
                                  ? rt_remote_spawn_handle_validate(ex, &st->handle)
                                  : status;
        rt_async_return(st, (uint64_t)status);
        return;
    }
    rt_async_return(NULL, 0);
}

static int await_parent(remote_publish_state* st) {
    void* task = __task_create(POLL_REMOTE_PUBLISHER, st);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(task, &kind, &bits);
    if (kind != 1) {
        return 0;
    }
    return bits == (uint64_t)st->status;
}

static int run_publish(uint32_t wanted_dst, int stale) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = pin_shard(ex, wanted_dst);
    if (!await_parent(&st)) return fail("publisher await failed");
    if (st.status != RT_REMOTE_SPAWN_STATUS_OK) return fail("publish did not return OK");
    if (st.validate_status != RT_REMOTE_SPAWN_STATUS_OK) return fail("handle did not validate");
    if (st.handle.owner_shard_id != st.dst) return fail("ack returned wrong owner shard");
    if (st.handle.generation == 0) return fail("ack returned empty generation");
    if (st.children_after != 0) return fail("remote child was enrolled under caller children");
    if (st.saw_pending == 0 || st.request_id == 0) return fail("publisher did not suspend for ack");
    if (!wait_child(&child, 5000)) return fail("remote child did not run");
    if (atomic_load_explicit(&child.owner, memory_order_acquire) != st.dst) {
        return fail("remote child owner mismatch");
    }
    uint32_t worker = atomic_load_explicit(&child.worker, memory_order_acquire);
    if (worker != st.dst && !(st.dst == 0 && worker == UINT32_MAX)) {
        return fail("remote child ran on non-owner shard");
    }
    rt_shard* source = rt_runtime_shard(rt_executor_runtime(ex), 0);
    rt_shard* dest = rt_runtime_shard(rt_executor_runtime(ex), st.dst);
    struct rt_transport_debug_snapshot src = rt_transport_debug_snapshot(source);
    struct rt_transport_debug_snapshot dst = rt_transport_debug_snapshot(dest);
    if (dst.transport_spawn_requests == 0) return fail("destination request counter missing");
    if (src.transport_spawn_acks == 0) return fail("source ack counter missing");
    if (rt_sync_point_reached_count(RT_SYNC_POINT_SP_TRANSPORT_REPLY_WAIT_BEFORE_TASK_SUSPEND) == 0) {
        return fail("reply wait did not use task-suspend seam");
    }
    if (stale) {
        rt_far_task_handle bad = st.handle;
        bad.generation++;
        if (rt_remote_spawn_handle_validate(ex, &bad) != RT_REMOTE_SPAWN_STATUS_STALE_TOKEN) {
            return fail("fabricated stale token validated");
        }
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int run_queue_full(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = 0;
    st.fill_queue = 1;
    if (!await_parent(&st)) return fail("queue-full publisher await failed");
    if (st.status != RT_REMOTE_SPAWN_STATUS_QUEUE_FULL) return fail("queue full status mismatch");
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), 0);
    struct rt_transport_debug_snapshot snap = rt_transport_debug_snapshot(shard);
    if (snap.control_len == 0 || snap.data_len != RT_TRANSPORT_DATA_QUEUE_CAP) {
        return fail("control lane was blocked by full data lane");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int run_shutdown(void) {
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = 0;
    st.shutdown_first = 1;
    if (!await_parent(&st)) return fail("shutdown publisher await failed");
    if (st.status != RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN) {
        return fail("shutdown status mismatch");
    }
    return 0;
}

int main(int argc, char** argv) {
    if (argc != 2) return fail("usage: remote_publication_harness <mode>");
    if (strcmp(argv[1], "publish-other") == 0) return run_publish(1, 0);
    if (strcmp(argv[1], "self-crossing") == 0) return run_publish(0, 0);
    if (strcmp(argv[1], "stale-token") == 0) return run_publish(1, 1);
    if (strcmp(argv[1], "queue-full") == 0) return run_queue_full();
    if (strcmp(argv[1], "shutdown") == 0) return run_shutdown();
    return fail("unknown mode");
}
`
