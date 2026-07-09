//go:build runtime_v2_transport_spine

package vm_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeV2TransportSpineAcceptanceRows(t *testing.T) {
	rows := []struct {
		name       string
		mode       string
		flags      []string
		expectFail bool
	}{
		{name: "lost-wake/recheck", mode: "threaded_recheck"},
		{
			name:       "lost-wake negative skip recheck",
			mode:       "threaded_recheck",
			flags:      []string{"-DRT_TRANSPORT_NEG_SKIP_RECHECK"},
			expectFail: true,
		},
		{
			name:       "lost-wake negative relaxed park ordering",
			mode:       "worker_wait_wake",
			flags:      []string{"-DRT_TRANSPORT_NEG_RELAXED_PARK_ORDER"},
			expectFail: true,
		},
		{name: "wake elision RUNNING", mode: "running_elision"},
		{name: "PARKED wake exactly once", mode: "worker_wait_wake"},
		{
			name:       "wake elision negative parked wake skipped",
			mode:       "worker_wait_wake",
			flags:      []string{"-DRT_TRANSPORT_NEG_SKIP_PARKED_WAKE"},
			expectFail: true,
		},
		{
			name:       "wake elision negative running wake written",
			mode:       "running_elision",
			flags:      []string{"-DRT_TRANSPORT_NEG_WRITE_RUNNING_WAKE"},
			expectFail: true,
		},
		{name: "PARKED-with-inbound invariant", mode: "recheck"},
		{
			name:       "PARKED-with-inbound negative",
			mode:       "parked_inbound_negative",
			flags:      []string{"-DRT_TRANSPORT_NEG_SKIP_RECHECK"},
			expectFail: true,
		},
		{name: "shutdown wakes parked shards and reply waiters", mode: "shutdown_wake"},
		{
			name:       "shutdown negative no wake",
			mode:       "shutdown_wake",
			flags:      []string{"-DRT_TRANSPORT_NEG_SHUTDOWN_NO_WAKE"},
			expectFail: true,
		},
		{name: "reply wait suspends task instead of parking shard", mode: "reply_wait"},
		{
			name:       "reply wait negative shard park",
			mode:       "reply_wait",
			flags:      []string{"-DRT_TRANSPORT_NEG_REPLY_WAIT_PARKS_SHARD"},
			expectFail: true,
		},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			output, err := runTransportSpineAcceptanceProgram(t, row.mode, row.flags)
			if row.expectFail {
				if err == nil {
					t.Fatalf("negative control unexpectedly passed\n%s", output)
				}
				if !strings.Contains(output, "transport-spine-check:") {
					t.Fatalf("negative control failed without deterministic harness message: %v\n%s", err, output)
				}
				return
			}
			if err != nil {
				t.Fatalf("acceptance row failed: %v\n%s", err, output)
			}
		})
	}
}

func runTransportSpineAcceptanceProgram(t *testing.T, mode string, flags []string) (string, error) {
	t.Helper()
	root := repoRoot(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 transport acceptance rows: %v", err)
	}
	exe := filepath.Join(t.TempDir(), "transport-spine-acceptance")
	args := []string{
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Werror",
		"-DRT_TEST_SYNC_POINTS",
		"-I" + filepath.Join(root, "runtime", "native"),
	}
	args = append(args, flags...)
	args = append(args,
		"-x",
		"c",
		"-",
		filepath.Join(root, "runtime", "native", "rt_transport.c"),
		filepath.Join(root, "runtime", "native", "rt_lane.c"),
		filepath.Join(root, "runtime", "native", "rt_sync_point.c"),
		"-pthread",
		"-o",
		exe,
	)
	buildCmd := exec.Command(clang, args...)
	buildCmd.Dir = root
	buildCmd.Stdin = strings.NewReader(transportSpineAcceptanceSource)
	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("transport spine acceptance compile failed:\n%s", buildOutput)
	}

	runCmd := exec.Command(exe, mode)
	runOutput, err := runCmd.CombinedOutput()
	return string(runOutput), err
}

const transportSpineAcceptanceSource = `
#define _POSIX_C_SOURCE 200809L

#include <errno.h>
#include <pthread.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "rt_async_internal.h"
#include "rt_sync_point.h"
#include "rt_transport.h"

void panic_msg(const char* msg) {
    fprintf(stderr, "panic: %s\n", msg);
}

void rt_trace_control_lock_acquired(void) {
}

static int fail(const char* message) {
    fprintf(stderr, "transport-spine-check: %s\n", message);
    return 1;
}

static void sleep_ms(long ms) {
    struct timespec ts = {
        .tv_sec = ms / 1000,
        .tv_nsec = (ms % 1000) * 1000L * 1000L,
    };
    (void)nanosleep(&ts, 0);
}

static void deadline_ms(struct timespec* out, long ms) {
    clock_gettime(CLOCK_REALTIME, out);
    out->tv_sec += ms / 1000;
    out->tv_nsec += (ms % 1000) * 1000L * 1000L;
    if (out->tv_nsec >= 1000000000L) {
        out->tv_sec++;
        out->tv_nsec -= 1000000000L;
    }
}

static int wait_sync_point_count(rt_sync_point_id id, unsigned before) {
    for (int i = 0; i < 500; i++) {
        if (rt_sync_point_reached_count(id) > before) {
            return 1;
        }
        sleep_ms(1);
    }
    return 0;
}

static int wait_parked(rt_shard* shard) {
    for (int i = 0; i < 500; i++) {
        struct rt_transport_debug_snapshot snapshot = rt_transport_debug_snapshot(shard);
        if (snapshot.park_state == RT_TRANSPORT_SHARD_PARKED) {
            return 1;
        }
        sleep_ms(1);
    }
    return 0;
}

static int init_shard(rt_shard* shard, rt_runtime* runtime, rt_executor* ex, uint32_t id) {
    memset(shard, 0, sizeof(*shard));
    shard->runtime = runtime;
    shard->executor = ex;
    shard->shard_id = id;
    if (rt_shard_sync_init(shard) != RT_RUNTIME_STATUS_OK) return 1;
    if (rt_transport_state_init(&shard->transport) != RT_RUNTIME_STATUS_OK) return 2;
    shard->scheduler.worker_count = 1;
    return 0;
}

static void destroy_shard(rt_shard* shard) {
    rt_transport_state_destroy(&shard->transport);
    rt_shard_sync_destroy(shard);
}

static int init_runtime(rt_executor* ex, rt_runtime* runtime, size_t shards) {
    memset(ex, 0, sizeof(*ex));
    memset(runtime, 0, sizeof(*runtime));
    runtime->shard_count = shards;
    ex->runtime = runtime;
    for (size_t i = 0; i < shards; i++) {
        if (init_shard(&runtime->shards[i], runtime, ex, (uint32_t)i) != 0) return 1;
    }
    return 0;
}

static void destroy_runtime(rt_runtime* runtime) {
    for (size_t i = 0; i < runtime->shard_count; i++) {
        destroy_shard(&runtime->shards[i]);
    }
}

static rt_transport_msg msg(rt_transport_msg_kind kind) {
    rt_transport_msg out = {
        .kind = kind,
        .source_shard_id = 0,
        .target_shard_id = 0,
        .route_id = 1,
        .generation = 1,
        .payload = 0,
        .payload_len = 0,
    };
    return out;
}

static int mode_recheck(void) {
    rt_executor ex;
    rt_runtime runtime;
    if (init_runtime(&ex, &runtime, 1) != 0) return fail("init failed");
    rt_shard* shard = &runtime.shards[0];
    rt_transport_msg data = msg(RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST);
    if (rt_transport_enqueue(shard, &data) != RT_TRANSPORT_STATUS_OK) {
        destroy_runtime(&runtime);
        return fail("pre-park enqueue failed");
    }
    rt_transport_status status = rt_transport_prepare_shard_park(shard);
    struct rt_transport_debug_snapshot snapshot = rt_transport_debug_snapshot(shard);
    if (status != RT_TRANSPORT_STATUS_UNAVAILABLE) {
        destroy_runtime(&runtime);
        return fail("park recheck did not report inbound work");
    }
    if (snapshot.park_state != RT_TRANSPORT_SHARD_RUNNING || snapshot.inbound_len != 1) {
        destroy_runtime(&runtime);
        return fail("park recheck did not restore RUNNING with inbound visible");
    }
    destroy_runtime(&runtime);
    return 0;
}

typedef struct park_thread_args {
    rt_shard* shard;
    rt_transport_status status;
} park_thread_args;

static void* park_thread_main(void* raw) {
    park_thread_args* args = (park_thread_args*)raw;
    args->status = rt_transport_prepare_shard_park(args->shard);
    return 0;
}

static int mode_threaded_recheck(void) {
    setenv("SURGE_SYNC_POINT", "SP_TRANSPORT_AFTER_PARK_BEFORE_RECHECK:block", 1);
    rt_executor ex;
    rt_runtime runtime;
    if (init_runtime(&ex, &runtime, 1) != 0) return fail("init failed");
    rt_shard* shard = &runtime.shards[0];
    rt_transport_msg data = msg(RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST);
    if (rt_transport_enqueue(shard, &data) != RT_TRANSPORT_STATUS_OK) {
        destroy_runtime(&runtime);
        return fail("pre-park enqueue failed");
    }
    unsigned before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_TRANSPORT_AFTER_PARK_BEFORE_RECHECK);
    park_thread_args args = {.shard = shard, .status = RT_TRANSPORT_STATUS_INVALID_ARGUMENT};
    pthread_t thread;
    if (pthread_create(&thread, 0, park_thread_main, &args) != 0) {
        destroy_runtime(&runtime);
        return fail("park thread create failed");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_TRANSPORT_AFTER_PARK_BEFORE_RECHECK, before)) {
        rt_sync_point_open();
        (void)pthread_join(thread, 0);
        destroy_runtime(&runtime);
        return fail("park thread did not reach recheck window");
    }
    rt_sync_point_open();
    if (pthread_join(thread, 0) != 0) {
        destroy_runtime(&runtime);
        return fail("park thread join failed");
    }
    struct rt_transport_debug_snapshot snapshot = rt_transport_debug_snapshot(shard);
    if (args.status != RT_TRANSPORT_STATUS_UNAVAILABLE) {
        destroy_runtime(&runtime);
        return fail("park recheck did not observe pre-existing inbound work");
    }
    if (snapshot.park_state != RT_TRANSPORT_SHARD_RUNNING || snapshot.inbound_len != 1 ||
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_TRANSPORT_AFTER_PARK_BEFORE_RECHECK) !=
            before + 1) {
        destroy_runtime(&runtime);
        return fail("threaded recheck did not restore RUNNING after sync-point window");
    }
    destroy_runtime(&runtime);
    return 0;
}

static int mode_running_elision(void) {
    rt_executor ex;
    rt_runtime runtime;
    if (init_runtime(&ex, &runtime, 1) != 0) return fail("init failed");
    rt_shard* shard = &runtime.shards[0];
    rt_transport_msg data = msg(RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST);
    if (rt_transport_enqueue(shard, &data) != RT_TRANSPORT_STATUS_OK) {
        destroy_runtime(&runtime);
        return fail("running enqueue failed");
    }
    struct rt_transport_debug_snapshot snapshot = rt_transport_debug_snapshot(shard);
    if (snapshot.transport_wake_writes != 0 || snapshot.transport_wake_elisions != 1) {
        destroy_runtime(&runtime);
        return fail("RUNNING enqueue did not elide wake exactly once");
    }
    destroy_runtime(&runtime);
    return 0;
}

static int mode_parked_wake(void) {
    rt_executor ex;
    rt_runtime runtime;
    if (init_runtime(&ex, &runtime, 1) != 0) return fail("init failed");
    rt_shard* shard = &runtime.shards[0];
    if (rt_transport_prepare_shard_park(shard) != RT_TRANSPORT_STATUS_OK) {
        destroy_runtime(&runtime);
        return fail("empty shard did not park");
    }
    rt_transport_msg data = msg(RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST);
    if (rt_transport_enqueue(shard, &data) != RT_TRANSPORT_STATUS_OK) {
        destroy_runtime(&runtime);
        return fail("parked enqueue failed");
    }
    struct rt_transport_debug_snapshot snapshot = rt_transport_debug_snapshot(shard);
    if (snapshot.transport_wake_writes != 1 || snapshot.transport_wake_elisions != 0) {
        destroy_runtime(&runtime);
        return fail("PARKED enqueue did not write exactly one wake");
    }
    if (shard->scheduler.wake_pending != 1) {
        destroy_runtime(&runtime);
        return fail("PARKED enqueue did not publish worker wake token");
    }
    destroy_runtime(&runtime);
    return 0;
}

typedef struct worker_wait_args {
    rt_shard* shard;
    int result;
} worker_wait_args;

static void* worker_wait_thread_main(void* raw) {
    worker_wait_args* args = (worker_wait_args*)raw;
    rt_shard* shard = args->shard;
    args->result = 1;
    rt_shard_lock(shard);
    rt_transport_status status = rt_transport_prepare_shard_park(shard);
    if (status != RT_TRANSPORT_STATUS_OK) {
        rt_shard_unlock(shard);
        args->result = 2;
        return 0;
    }
    struct timespec deadline;
    deadline_ms(&deadline, 300);
    while (shard->scheduler.wake_pending == 0) {
        int wait_status = pthread_cond_timedwait(&shard->worker_cv, &shard->lock, &deadline);
        if (wait_status == ETIMEDOUT) {
            rt_shard_unlock(shard);
            args->result = 3;
            return 0;
        }
    }
    shard->scheduler.wake_pending--;
    rt_transport_mark_shard_running(shard);
    size_t drained = rt_transport_drain_inbound_locked(shard, 1);
    rt_shard_unlock(shard);
    args->result = drained == 1 ? 0 : 4;
    return 0;
}

static int mode_worker_wait_wake(void) {
    rt_executor ex;
    rt_runtime runtime;
    if (init_runtime(&ex, &runtime, 1) != 0) return fail("init failed");
    rt_shard* shard = &runtime.shards[0];
    worker_wait_args args = {.shard = shard, .result = 99};
    pthread_t thread;
    if (pthread_create(&thread, 0, worker_wait_thread_main, &args) != 0) {
        destroy_runtime(&runtime);
        return fail("worker wait thread create failed");
    }
    if (!wait_parked(shard)) {
        (void)pthread_join(thread, 0);
        destroy_runtime(&runtime);
        return fail("worker wait thread did not park");
    }
    rt_transport_msg data = msg(RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST);
    if (rt_transport_enqueue(shard, &data) != RT_TRANSPORT_STATUS_OK) {
        (void)pthread_join(thread, 0);
        destroy_runtime(&runtime);
        return fail("worker wait enqueue failed");
    }
    if (pthread_join(thread, 0) != 0) {
        destroy_runtime(&runtime);
        return fail("worker wait thread join failed");
    }
    if (args.result != 0) {
        destroy_runtime(&runtime);
        return fail("worker wait did not wake and drain exactly one message");
    }
    struct rt_transport_debug_snapshot snapshot = rt_transport_debug_snapshot(shard);
    if (snapshot.transport_wake_writes != 1 || snapshot.drain_count != 1 ||
        snapshot.inbound_len != 0) {
        destroy_runtime(&runtime);
        return fail("worker wait counters did not prove wake and drain");
    }
    destroy_runtime(&runtime);
    return 0;
}

static int mode_parked_inbound_negative(void) {
    rt_executor ex;
    rt_runtime runtime;
    if (init_runtime(&ex, &runtime, 1) != 0) return fail("init failed");
    rt_shard* shard = &runtime.shards[0];
    rt_transport_msg data = msg(RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST);
    if (rt_transport_enqueue(shard, &data) != RT_TRANSPORT_STATUS_OK) {
        destroy_runtime(&runtime);
        return fail("pre-park enqueue failed");
    }
    (void)rt_transport_prepare_shard_park(shard);
    struct rt_transport_debug_snapshot snapshot = rt_transport_debug_snapshot(shard);
    if (snapshot.park_state == RT_TRANSPORT_SHARD_PARKED && snapshot.inbound_len > 0) {
        destroy_runtime(&runtime);
        return fail("negative left shard PARKED with inbound work");
    }
    destroy_runtime(&runtime);
    return 0;
}

static int mode_shutdown_wake(void) {
    rt_executor ex;
    rt_runtime runtime;
    if (init_runtime(&ex, &runtime, 2) != 0) return fail("init failed");
    if (rt_transport_prepare_shard_park(&runtime.shards[0]) != RT_TRANSPORT_STATUS_OK ||
        rt_transport_prepare_shard_park(&runtime.shards[1]) != RT_TRANSPORT_STATUS_OK) {
        destroy_runtime(&runtime);
        return fail("shutdown setup park failed");
    }
    if (rt_transport_shutdown_wake_all(&ex) != 2) {
        destroy_runtime(&runtime);
        return fail("shutdown did not wake all shards");
    }
    struct rt_transport_debug_snapshot first = rt_transport_debug_snapshot(&runtime.shards[0]);
    struct rt_transport_debug_snapshot second = rt_transport_debug_snapshot(&runtime.shards[1]);
    if (first.shutdown_wakes != 1 || second.shutdown_wakes != 1 ||
        first.park_state != RT_TRANSPORT_SHARD_SHUTDOWN ||
        second.park_state != RT_TRANSPORT_SHARD_SHUTDOWN) {
        destroy_runtime(&runtime);
        return fail("shutdown counters or states are wrong");
    }
    destroy_runtime(&runtime);
    return 0;
}

static int mode_reply_wait(void) {
    if (!rt_transport_reply_wait_before_task_suspend()) {
        return fail("reply wait parked shard instead of suspending task");
    }
    if (rt_sync_point_reached_count(RT_SYNC_POINT_SP_TRANSPORT_REPLY_WAIT_BEFORE_TASK_SUSPEND) !=
        1) {
        return fail("reply wait sync point was not reached exactly once");
    }
    return 0;
}

int main(int argc, char** argv) {
    if (argc != 2) return fail("missing mode");
    if (strcmp(argv[1], "recheck") == 0) return mode_recheck();
    if (strcmp(argv[1], "threaded_recheck") == 0) return mode_threaded_recheck();
    if (strcmp(argv[1], "running_elision") == 0) return mode_running_elision();
    if (strcmp(argv[1], "parked_wake") == 0) return mode_parked_wake();
    if (strcmp(argv[1], "worker_wait_wake") == 0) return mode_worker_wait_wake();
    if (strcmp(argv[1], "parked_inbound_negative") == 0) return mode_parked_inbound_negative();
    if (strcmp(argv[1], "shutdown_wake") == 0) return mode_shutdown_wake();
    if (strcmp(argv[1], "reply_wait") == 0) return mode_reply_wait();
    return fail("unknown mode");
}
`
