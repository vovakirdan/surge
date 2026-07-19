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

rt_remote_spawn_status (*check_publish)(uint32_t, uint64_t, int64_t, void*,
    rt_remote_spawn_pending**, rt_far_task_handle*) = rt_remote_spawn_publish;
rt_remote_spawn_status (*check_publish_placement)(rt_placement, uint64_t, int64_t, void*,
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
			name: "shutdown-queued-kinds",
			mode: "shutdown-queued-kinds",
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

// Spawn-on abandon and refusal edges over a DROPPABLE shipped state: every
// row asserts the exactly-once census (the harness drop stub) on top of the
// edge it forces. Refusals drop through the pending exactly once; abandons
// land while the dispatch lane is held at an armed window, and because the
// request is already in flight the body still runs and owns the state — the
// abandoned pending must not drop it, and the ack resolves as an
// owner-routed release.
func TestRuntimeV2RemoteSpawnAbandonEdges(t *testing.T) {
	bin := buildRemotePublicationHarness(t)
	rows := []struct {
		name string
		mode string
		env  []string
	}{
		{
			name: "refusal-queue-full-drops-once",
			mode: "refusal-drop-queue-full",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "refusal-shutdown-drops-once",
			mode: "refusal-drop-shutdown",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "abandon-before-dispatch",
			mode: "abandon-before-dispatch",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_DISPATCH:block"),
		},
		{
			name: "abandon-before-body-publish",
			mode: "abandon-before-body-publish",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_BODY_PUBLISH:block"),
		},
		{
			name: "abandon-before-ack",
			mode: "abandon-before-ack",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_ACK:block"),
		},
		{
			// Saturated source control lane at ack time: the dispatch
			// rescue-drains (control-first) and the ack still lands — a
			// full lane can never orphan the handle or the shipped state.
			name: "ack-rescue-drain-after-handoff",
			mode: "ack-rescue-drain",
			env: remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_ACK:block"),
		},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, row.mode, row.env)
			if code != 0 {
				t.Fatalf("abandon edge %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					row.mode, code, stdout, stderr)
			}
		})
	}
}

// Stale-generation rows for the shipped-state contract: a pending that
// resolves before its request dispatches is the sole owner and drops the
// state exactly once (no body); a redelivered request or ack arriving
// after resolution releases only its own message reference — the
// body-owned state never drops through it and no second body is created.
func TestRuntimeV2RemoteSpawnStaleGenerationRows(t *testing.T) {
	bin := buildRemotePublicationHarness(t)
	rows := []struct {
		name string
		mode string
		env  []string
	}{
		{
			name: "stale-request-before-body",
			mode: "stale-request-before-body",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_DISPATCH:block"),
		},
		{
			name: "duplicate-request-after-handoff",
			mode: "duplicate-request-after-handoff",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_ACK:block"),
		},
		{
			name: "stale-ack-after-resolution",
			mode: "stale-ack-after-resolution",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_ACK:block"),
		},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, row.mode, row.env)
			if code != 0 {
				t.Fatalf("stale row %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
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
	// The shutdown drain releases every kind production can enqueue and
	// fails closed only for truly unknown kinds (RV2-DEBT-047): a message
	// parked between the last steady-state drain and shutdown is valid
	// traffic, and its payload releases through the kind-complete switch.
	if !strings.Contains(
		source, `panic_msg("remote spawn: unknown transport message kind during shutdown")`) {
		t.Fatal("shutdown drain must fail closed for unknown transport kinds")
	}
	for _, kind := range []string{
		"RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST",
		"RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY",
		"RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REQUEST",
		"RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REQUEST",
		"RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REQUEST",
		"RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REPLY",
		"RT_TRANSPORT_MSG_CREDIT_CONTROL",
	} {
		if !strings.Contains(source, "case "+kind+":") {
			t.Fatalf("shutdown drain release switch must cover %s (RV2-DEBT-047)", kind)
		}
	}
}

// The shipped-state ownership contract (rt_remote_spawn_internal.h):
// pending owns -> body owns, linearized at the publication-accepted
// handoff. Every family records the obligation when it publishes, each
// dispatch clears it exactly once immediately after the accepted
// publication, and the final pending release stays the single
// pre-handoff drop site.
func TestRuntimeV2RemoteStateHandoffStaticContract(t *testing.T) {
	root := repoRoot(t)
	for path, record := range map[string]string{
		"runtime/native/rt_remote_spawn.c":          "req->state_owned = state_drop_fn_id != 0;",
		"runtime/native/rt_immediate_on.c":          "request->state_owned = state_drop_fn_id != 0;",
		"runtime/native/rt_immediate_on_anchored.c": "request->state_owned = state_drop_fn_id != 0;",
		"runtime/native/rt_far_channel_select.c":    "request->state_owned = state_drop_fn_id != 0;",
	} {
		if !strings.Contains(readTransportContractFile(t, root, path), record) {
			t.Fatalf("%s must record the shipped-state drop obligation at publish", path)
		}
	}
	for path, handoff := range map[string]string{
		"runtime/native/rt_remote_spawn.c":       "req->state_owned = 0;",
		"runtime/native/rt_immediate_on.c":       "pending->state_owned = 0;",
		"runtime/native/rt_far_channel_select.c": "pending->state_owned = 0;",
	} {
		dispatch := readTransportContractFile(t, root, path)
		if strings.Count(dispatch, handoff) != 1 {
			t.Fatalf("%s must clear the obligation at exactly one publication-accepted handoff", path)
		}
		publish := strings.Index(dispatch, "rt_remote_spawn_publish_body_task")
		if publish < 0 || strings.Index(dispatch, handoff) < publish {
			t.Fatalf("%s handoff must follow the accepted publication", path)
		}
	}
	// Anchored bodies dispatch through the shared immediate-on path; a
	// second handoff site would split the linearization point.
	if strings.Contains(
		readTransportContractFile(t, root, "runtime/native/rt_immediate_on_anchored.c"),
		"state_owned = 0") {
		t.Fatal("anchored dispatch must reuse the immediate-on handoff, not add a second one")
	}
	// Pre-handoff drops stay gated on the owned flag at the final release.
	for path, guard := range map[string]string{
		"runtime/native/rt_remote_spawn_pending.c": "pending->state_owned != 0",
		"runtime/native/rt_remote_task_pending.c":  "pending->state_owned != 0",
	} {
		if !strings.Contains(readTransportContractFile(t, root, path), guard) {
			t.Fatalf("%s final release must drop only while the pending still owns the state", path)
		}
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
#include "rt_remote_spawn_internal.h"
#include "rt_remote_task.h"
#include "rt_sync_point.h"
#include "rt_transport.h"

#include <pthread.h>
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
    POLL_REMOTE_CHILD = 7002,
    DROP_REMOTE_STATE = 7003
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
    uint32_t droppable;
    uint32_t abandon_mode;
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

// Drop-dispatch stub: the abandon/refusal rows publish a droppable state
// (DROP_REMOTE_STATE) and count releases here — the exactly-once census
// for the shipped-state ownership contract. Any other id is a test bug.
static _Atomic uint32_t drop_calls;
static void* drop_expected_state;
void __surge_drop_call(uint64_t id, void* state) {
    if (id == DROP_REMOTE_STATE && state == drop_expected_state) {
        atomic_fetch_add_explicit(&drop_calls, 1, memory_order_acq_rel);
        return;
    }
    fputs("unexpected __surge_drop_call\n", stderr);
    exit(97);
}

static int wait_reached(rt_sync_point_id id, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (rt_sync_point_reached_count(id) > 0) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static int wait_drops(uint32_t want, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(&drop_calls, memory_order_acquire) == want) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

// Saturate the shard's control lane so the NEXT control-kind enqueue
// (an ack) fails deterministically. NULL-payload acks are harmless to
// drain later (the ack dispatcher ignores them).
static int fill_control_lane(rt_shard* shard, uint32_t shard_id) {
    if (shard == NULL) {
        return 0;
    }
    rt_transport_msg ack = {0};
    ack.kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK;
    ack.target_shard_id = shard_id;
    for (size_t i = 0; i < RT_TRANSPORT_CONTROL_QUEUE_CAP * 2; i++) {
        if (rt_transport_enqueue(shard, &ack) == RT_TRANSPORT_STATUS_QUEUE_FULL) {
            return 1;
        }
    }
    return 0;
}

static int wait_lanes_empty(rt_shard* shard, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        struct rt_transport_debug_snapshot snap = rt_transport_debug_snapshot(shard);
        if (snap.data_len == 0 && snap.control_len == 0) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

void __surge_poll_call(uint64_t id) {
    if (id == POLL_REMOTE_CHILD) {
        remote_child_state* child = (remote_child_state*)__task_state();
        const rt_task* task = rt_current_task();
        atomic_store_explicit(&child->owner,
                              task != NULL ? task->owner_shard_id : UINT32_MAX,
                              memory_order_release);
        atomic_store_explicit(&child->worker, rt_debug_current_worker_shard_id(), memory_order_release);
        // A counter, not a flag: the duplicate/stale rows assert that a
        // redelivered request never creates a SECOND body.
        atomic_fetch_add_explicit(&child->ran, 1, memory_order_acq_rel);
        rt_async_return(child, 77);
        return;
    }
    if (id == POLL_REMOTE_PUBLISHER) {
        remote_publish_state* st = (remote_publish_state*)__task_state();
        rt_executor* ex = ensure_exec();
        if (st->abandon_mode && st->saw_pending) {
            // The abandon rows model a departed caller: after the driver
            // abandons the handle the pending may be freed at any moment,
            // so this task must never touch it again.
            rt_async_yield(st);
            return;
        }
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
            st->dst, st->droppable ? DROP_REMOTE_STATE : 0, POLL_REMOTE_CHILD,
            st->child, &st->pending, &st->handle);
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

// RV2-DEBT-047: messages parked between the last steady-state drain and
// shutdown are valid traffic for every production kind — the shutdown
// drain must release them, never panic.
static int run_shutdown_queued_kinds(void) {
    rt_executor* ex = ensure_exec();
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), 0);
    const rt_transport_msg_kind kinds[] = {
        RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST,
        RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY,
        RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REQUEST,
        RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REQUEST,
        RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REQUEST,
        RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REPLY,
        RT_TRANSPORT_MSG_CREDIT_CONTROL,
    };
    for (size_t i = 0; i < sizeof(kinds) / sizeof(kinds[0]); i++) {
        rt_transport_msg msg = {0};
        msg.kind = kinds[i];
        msg.target_shard_id = 0;
        if (rt_transport_enqueue(shard, &msg) != RT_TRANSPORT_STATUS_OK) {
            return fail("queued-kind enqueue failed");
        }
    }
    rt_remote_spawn_fail_all_pending(ex, RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN);
    struct rt_transport_debug_snapshot snap = rt_transport_debug_snapshot(shard);
    if (snap.data_len != 0 || snap.control_len != 0) {
        return fail("shutdown drain left queued messages behind");
    }
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

// Refusal edges with a droppable shipped state: the publish is refused
// (queue full or destination shutdown), so the pending — or the pre-link
// path — is the sole owner and must drop the state exactly once, before
// the caller observes the refusal.
static int run_refusal_drop(int shutdown_first) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = 0;
    st.fill_queue = shutdown_first ? 0 : 1;
    st.shutdown_first = shutdown_first ? 1 : 0;
    st.droppable = 1;
    drop_expected_state = &child;
    if (!await_parent(&st)) return fail("refusal publisher await failed");
    rt_remote_spawn_status want = shutdown_first ? RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN
                                                 : RT_REMOTE_SPAWN_STATUS_QUEUE_FULL;
    if (st.status != want) return fail("refusal status mismatch");
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 1) {
        return fail("refused publish must drop the shipped state exactly once");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
        return fail("refused publish must not run a body");
    }
    if (!shutdown_first) {
        (void)rt_executor_request_shutdown(ex);
    }
    return 0;
}

// Abandon edges: the caller-owned handle is abandoned while the dispatch
// lane is held at an armed window (dispatch entry / created-but-unpublished
// / published-but-unacked). In every window the request is already in
// flight, so the body still runs and owns the shipped state — the pending
// must NOT drop it (drop count stays zero), and the resolved-while-abandoned
// ack turns into an owner-routed release instead of waking a caller.
static int run_abandon_window(rt_sync_point_id window) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = pin_shard(ex, 1);
    st.droppable = 1;
    st.abandon_mode = 1;
    drop_expected_state = &child;
    void* publisher = __task_create(POLL_REMOTE_PUBLISHER, &st);
    if (publisher == NULL) return fail("publisher task create failed");
    if (!wait_reached(window, 5000)) return fail("armed window was never reached");
    if (!rt_remote_spawn_abandon_handle(&st.handle)) {
        return fail("abandon did not find the listed pending");
    }
    rt_sync_point_open();
    if (!wait_child(&child, 5000)) return fail("abandoned spawn body did not run");
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("handed-off state must not drop through the abandoned pending");
    }
    if (rt_sync_point_reached_count(window) == 0) {
        return fail("window count vanished");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row-4 remainder, pinned as what the runtime actually guarantees: an
// ack facing a SATURATED control lane does not fail the publication —
// enqueue_with_drain rescue-drains (control-first) and the ack lands, so
// a full lane can never orphan the handle or the handed-off state. The
// failure branch below the rescue is reachable only through transport
// shutdown; its release ordering stays pinned by the static guards.
// Single-shard execution is driven by the main thread inside
// rt_task_await, so the lane fill runs on a helper thread while main is
// held at the armed window.
typedef struct ack_failure_driver {
    rt_executor* ex;
    _Atomic uint32_t failed;
} ack_failure_driver;

static void* ack_failure_driver_main(void* arg) {
    ack_failure_driver* drv = (ack_failure_driver*)arg;
    if (!wait_reached(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_ACK, 5000)) {
        atomic_store_explicit(&drv->failed, 1, memory_order_release);
        rt_sync_point_open();
        return NULL;
    }
    rt_shard* source = rt_runtime_shard(rt_executor_runtime(drv->ex), 0);
    if (!fill_control_lane(source, 0)) {
        atomic_store_explicit(&drv->failed, 2, memory_order_release);
    }
    rt_sync_point_open();
    return NULL;
}

static int run_ack_rescue_drain(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = 0;
    st.droppable = 1;
    drop_expected_state = &child;
    ack_failure_driver drv;
    memset(&drv, 0, sizeof(drv));
    drv.ex = ex;
    pthread_t driver;
    if (pthread_create(&driver, NULL, ack_failure_driver_main, &drv) != 0) {
        return fail("driver thread create failed");
    }
    remote_publish_state* stp = &st;
    if (!await_parent(stp)) {
        (void)pthread_join(driver, NULL);
        return fail("ack-failure publisher await failed");
    }
    (void)pthread_join(driver, NULL);
    if (atomic_load_explicit(&drv.failed, memory_order_acquire) != 0) {
        return fail(atomic_load_explicit(&drv.failed, memory_order_acquire) == 1
                        ? "ack window was never reached"
                        : "control lane did not saturate");
    }
    if (st.status != RT_REMOTE_SPAWN_STATUS_OK) {
        return fail("saturated control lane must not fail the publication (rescue drain)");
    }
    if (!wait_child(&child, 5000)) return fail("published body did not run");
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("handed-off state must not drop across the ack rescue");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Stale request before body creation: the pending resolves while its
// request message is still waiting at dispatch entry. The dispatch must
// step aside (no body), leaving the pending the sole owner — the state
// drops exactly once through the final pending release.
static int run_stale_request_before_body(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = pin_shard(ex, 1);
    st.droppable = 1;
    drop_expected_state = &child;
    void* publisher = __task_create(POLL_REMOTE_PUBLISHER, &st);
    if (publisher == NULL) return fail("publisher task create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_DISPATCH, 5000)) {
        return fail("dispatch window was never reached");
    }
    rt_remote_spawn_fail_all_pending(ex, RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN);
    rt_sync_point_open();
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(publisher, &kind, &bits);
    if (kind != 1 || st.status != RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN) {
        return fail("resolved-before-dispatch must surface the failure status");
    }
    if (!wait_drops(1, 5000)) {
        return fail("sole-owner pending must drop the state exactly once");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
        return fail("stale request must not create a body");
    }
    return 0;
}

// Duplicate/stale delivery after resolution: a second copy of an already
// resolved request (or its ack) must release only its own message
// reference — never drop the body-owned state, never create a second
// body. The extra pending reference taken in the ack window models the
// redelivered copy's payload reference.
static int run_stale_redelivery(int redeliver_ack) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = pin_shard(ex, 1);
    st.droppable = 1;
    drop_expected_state = &child;
    void* publisher = __task_create(POLL_REMOTE_PUBLISHER, &st);
    if (publisher == NULL) return fail("publisher task create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_ACK, 5000)) {
        return fail("ack window was never reached");
    }
    rt_remote_spawn_pending* req = st.pending;
    if (req == NULL) return fail("pending missing in ack window");
    remote_spawn_pending_add_ref(req);
    uint32_t source_shard = req->source_shard_id;
    rt_sync_point_open();
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(publisher, &kind, &bits);
    if (kind != 1 || st.status != RT_REMOTE_SPAWN_STATUS_OK) {
        return fail("publication must resolve OK before the redelivery");
    }
    uint32_t target = redeliver_ack ? source_shard : st.dst;
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), target);
    rt_transport_msg dup = {0};
    dup.kind = redeliver_ack ? RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK
                             : RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST;
    dup.source_shard_id = source_shard;
    dup.target_shard_id = target;
    dup.route_id = rt_remote_spawn_pending_request_id(req);
    dup.payload = req;
    if (rt_transport_enqueue(shard, &dup) != RT_TRANSPORT_STATUS_OK) {
        return fail("redelivery enqueue failed");
    }
    if (!wait_lanes_empty(shard, 5000)) {
        return fail("redelivered message was never drained");
    }
    sleep_us(20000);
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("redelivery must not drop the body-owned state");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 1) {
        return fail("redelivery must not create a second body");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

int main(int argc, char** argv) {
    if (argc != 2) return fail("usage: remote_publication_harness <mode>");
    if (strcmp(argv[1], "publish-other") == 0) return run_publish(1, 0);
    if (strcmp(argv[1], "self-crossing") == 0) return run_publish(0, 0);
    if (strcmp(argv[1], "stale-token") == 0) return run_publish(1, 1);
    if (strcmp(argv[1], "queue-full") == 0) return run_queue_full();
    if (strcmp(argv[1], "shutdown") == 0) return run_shutdown();
    if (strcmp(argv[1], "shutdown-queued-kinds") == 0) return run_shutdown_queued_kinds();
    if (strcmp(argv[1], "refusal-drop-queue-full") == 0) return run_refusal_drop(0);
    if (strcmp(argv[1], "refusal-drop-shutdown") == 0) return run_refusal_drop(1);
    if (strcmp(argv[1], "abandon-before-dispatch") == 0) {
        return run_abandon_window(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_DISPATCH);
    }
    if (strcmp(argv[1], "abandon-before-body-publish") == 0) {
        return run_abandon_window(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_BODY_PUBLISH);
    }
    if (strcmp(argv[1], "abandon-before-ack") == 0) {
        return run_abandon_window(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_ACK);
    }
    if (strcmp(argv[1], "ack-rescue-drain") == 0) return run_ack_rescue_drain();
    if (strcmp(argv[1], "stale-request-before-body") == 0) return run_stale_request_before_body();
    if (strcmp(argv[1], "duplicate-request-after-handoff") == 0) return run_stale_redelivery(0);
    if (strcmp(argv[1], "stale-ack-after-resolution") == 0) return run_stale_redelivery(1);
    return fail("unknown mode");
}
`
