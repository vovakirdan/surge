//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeV2TransportSeamStaticShape(t *testing.T) {
	source := `
#include "rt_async_internal.h"
#include "rt_transport.h"

rt_transport_status (*runtime_v2_check_transport_enqueue)(rt_shard*, const rt_transport_msg*) =
    rt_transport_enqueue;
rt_transport_status (*runtime_v2_check_transport_try_drain_one)(rt_shard*, rt_transport_msg*) =
    rt_transport_try_drain_one;
rt_transport_status (*runtime_v2_check_transport_prepare_shard_park)(rt_shard*) =
    rt_transport_prepare_shard_park;
void (*runtime_v2_check_transport_mark_shard_running)(rt_shard*) =
    rt_transport_mark_shard_running;
uint64_t (*runtime_v2_check_transport_shutdown_wake_all)(rt_executor*) =
    rt_transport_shutdown_wake_all;
struct rt_transport_debug_snapshot (*runtime_v2_check_transport_debug_snapshot)(
    rt_shard*) = rt_transport_debug_snapshot;

_Static_assert(RT_TRANSPORT_STATUS_OK == 0, "transport OK status must stay zero");
_Static_assert(RT_TRANSPORT_STATUS_QUEUE_FULL != RT_TRANSPORT_STATUS_OK,
               "data backpressure must not look successful");
_Static_assert(RT_TRANSPORT_DATA_SLOT_CREDITS > 0, "data traffic must be bounded in slots");
_Static_assert(RT_TRANSPORT_CONTROL_SLOT_RESERVE > 0, "the control reserve must be non-empty");
_Static_assert(RT_TRANSPORT_DRAIN_TURN_LIMIT > 0, "worker turn must have bounded transport drain");
_Static_assert(RT_TRANSPORT_CONTROL_SLOT_RESERVE < RT_TRANSPORT_DATA_SLOT_CREDITS,
               "control lane is reserved, not the bulk data lane");

_Static_assert(RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST != RT_TRANSPORT_MSG_NONE,
               "remote spawn request must be a real data category");
_Static_assert(RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST != RT_TRANSPORT_MSG_NONE,
               "immediate execute request must be a real data category");
_Static_assert(RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION != RT_TRANSPORT_MSG_NONE,
               "completion must be a real data category");
_Static_assert(RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_REQUEST != RT_TRANSPORT_MSG_NONE,
               "cancel must be a real control category");
_Static_assert(RT_TRANSPORT_MSG_REMOTE_TASK_AWAIT_REQUEST != RT_TRANSPORT_MSG_NONE,
               "remote task await must be a real data category");
_Static_assert(RT_TRANSPORT_MSG_REMOTE_TASK_RELEASE_REQUEST != RT_TRANSPORT_MSG_NONE,
               "remote task release must be a real control category");
_Static_assert(RT_TRANSPORT_MSG_CLASS_DATA != RT_TRANSPORT_MSG_CLASS_CONTROL,
               "the two budgets must be distinguishable classes");
_Static_assert(RT_TRANSPORT_MSG_CLASS_INVALID != RT_TRANSPORT_MSG_CLASS_DATA &&
                   RT_TRANSPORT_MSG_CLASS_INVALID != RT_TRANSPORT_MSG_CLASS_CONTROL,
               "an unclassified kind must not fall into a budget");
_Static_assert(RT_TRANSPORT_MSG_SHUTDOWN_WAKE != RT_TRANSPORT_MSG_NONE,
               "shutdown wake must be a real control category");

_Static_assert(sizeof(rt_transport_msg) > 0, "rt_transport_msg must be complete");
_Static_assert(sizeof(((rt_transport_msg*)0)->source_shard_id) == sizeof(uint32_t),
               "source shard id must stay uint32_t");
_Static_assert(sizeof(((rt_transport_msg*)0)->target_shard_id) == sizeof(uint32_t),
               "target shard id must stay uint32_t");
_Static_assert(sizeof(((rt_transport_msg*)0)->generation) == sizeof(uint64_t),
               "generation token must stay uint64_t");
_Static_assert(sizeof(((rt_transport_msg*)0)->payload) == sizeof(void*),
               "payload must stay a bare pointer the transport does not own");
_Static_assert(offsetof(rt_transport_msg, payload) + sizeof(void*) == sizeof(rt_transport_msg),
               "the payload pointer must be the last member: nothing may trail it claiming to "
               "measure bytes the transport does not own");

_Static_assert(sizeof(((rt_shard*)0)->transport) == sizeof(rt_transport_state),
               "rt_shard must embed transport state by value");
_Static_assert(sizeof(((rt_transport_state*)0)->data) / sizeof(rt_transport_msg) ==
                   RT_TRANSPORT_DATA_SLOT_CREDITS,
               "data lane capacity must be structural");
_Static_assert(sizeof(((rt_transport_state*)0)->control) / sizeof(rt_transport_msg) ==
                   RT_TRANSPORT_CONTROL_SLOT_RESERVE,
               "control lane capacity must be structural");
_Static_assert(sizeof(((rt_transport_state*)0)->park_state) == sizeof(_Atomic uint8_t),
               "park state must be atomic");

_Static_assert(sizeof(struct rt_transport_debug_snapshot) > 0,
               "transport debug snapshot must be complete");
_Static_assert(sizeof(((struct rt_transport_debug_snapshot*)0)->inbound_len) == sizeof(size_t),
               "snapshot inbound_len must stay size_t");
_Static_assert(sizeof(((struct rt_transport_debug_snapshot*)0)->control_len) == sizeof(size_t),
               "snapshot control_len must stay size_t");
_Static_assert(sizeof(((struct rt_transport_debug_snapshot*)0)->data_len) == sizeof(size_t),
               "snapshot data_len must stay size_t");
_Static_assert(sizeof(((struct rt_transport_debug_snapshot*)0)->transport_spawn_requests) ==
                   sizeof(uint64_t),
               "transport spawn requests must be counted separately");
_Static_assert(sizeof(((struct rt_transport_debug_snapshot*)0)->transport_spawn_acks) ==
                   sizeof(uint64_t),
               "transport spawn acks must be counted separately");
_Static_assert(sizeof(((struct rt_transport_debug_snapshot*)0)->transport_wake_writes) ==
                   sizeof(uint64_t),
               "transport wake writes must be counted separately");
_Static_assert(sizeof(((struct rt_transport_debug_snapshot*)0)->transport_wake_elisions) ==
                   sizeof(uint64_t),
               "transport wake elisions must be counted separately");
_Static_assert(sizeof(((struct rt_transport_debug_snapshot*)0)->shutdown_wakes) ==
                   sizeof(uint64_t),
               "shutdown transport wakes must be counted separately");
_Static_assert(sizeof(((struct rt_transport_debug_snapshot*)0)->data_credit_stalls) ==
                   sizeof(uint64_t),
               "a refused data envelope must be counted, not merely returned");
_Static_assert(sizeof(((struct rt_transport_debug_snapshot*)0)->control_reserve_stalls) ==
                   sizeof(uint64_t),
               "a refused control envelope must be counted separately from a data refusal");
`

	runFDRegistryStaticCheck(t, "Runtime V2 transport seam static shape check", source)
}

func TestRuntimeV2TransportSpineBehavior(t *testing.T) {
	source := `
#include <pthread.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "rt_async_internal.h"
#include "rt_transport.h"

void panic_msg(const char* msg) {
    fprintf(stderr, "panic: %s\n", msg);
}

void rt_trace_control_lock_acquired(void) {
}

static int require_int(int condition, const char* message) {
    if (!condition) {
        fprintf(stderr, "%s\n", message);
        return 1;
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

int main(void) {
    rt_executor ex = {0};
    rt_runtime runtime = {0};
    runtime.shard_count = 2;
    ex.runtime = &runtime;
    if (init_shard(&runtime.shards[0], &runtime, &ex, 0) != 0) return 2;
    if (init_shard(&runtime.shards[1], &runtime, &ex, 1) != 0) return 3;
    rt_shard* shard = &runtime.shards[0];
    rt_shard* other = &runtime.shards[1];

    rt_transport_msg data = {
        .kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST,
        .source_shard_id = 0,
        .target_shard_id = 0,
        .route_id = 11,
        .generation = 22,
    };
    rt_transport_msg control = data;
    control.kind = RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_ACK;
    control.route_id = 99;

    if (require_int(rt_transport_enqueue(shard, &data) == RT_TRANSPORT_STATUS_OK,
                    "data enqueue failed")) return 3;
    if (require_int(rt_transport_enqueue(shard, &control) == RT_TRANSPORT_STATUS_OK,
                    "control enqueue failed")) return 4;

    rt_transport_msg out = {0};
    if (require_int(rt_transport_try_drain_one(shard, &out) == RT_TRANSPORT_STATUS_OK,
                    "first drain failed")) return 5;
    if (require_int(out.kind == RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_ACK,
                    "control lane did not drain before data")) return 6;
    if (require_int(rt_transport_try_drain_one(shard, &out) == RT_TRANSPORT_STATUS_OK,
                    "second drain failed")) return 7;
    if (require_int(out.kind == RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST,
                    "data lane did not drain after control")) return 8;

    for (size_t i = 0; i < RT_TRANSPORT_DATA_SLOT_CREDITS; i++) {
        if (rt_transport_enqueue(shard, &data) != RT_TRANSPORT_STATUS_OK) return 9;
    }
    if (require_int(rt_transport_enqueue(shard, &data) == RT_TRANSPORT_STATUS_QUEUE_FULL,
                    "bounded data lane did not report full")) return 10;
    if (require_int(rt_transport_enqueue(shard, &control) == RT_TRANSPORT_STATUS_OK,
                    "reserved control lane was blocked by full data lane")) return 11;
    rt_shard_lock(shard);
    (void)rt_transport_drain_inbound_locked(shard, 0);
    rt_shard_unlock(shard);

    if (require_int(rt_transport_prepare_shard_park(shard) == RT_TRANSPORT_STATUS_OK,
                    "empty shard did not park")) return 12;
    if (require_int(rt_transport_enqueue(shard, &data) == RT_TRANSPORT_STATUS_OK,
                    "parked enqueue failed")) return 13;
    struct rt_transport_debug_snapshot snapshot = rt_transport_debug_snapshot(shard);
    if (require_int(snapshot.transport_wake_writes == 1,
                    "parked shard wake was not recorded exactly once")) return 14;
    if (require_int(snapshot.inbound_len == 1,
                    "parked enqueue did not publish inbound message")) return 15;
    rt_transport_mark_shard_running(shard);
    (void)rt_transport_try_drain_one(shard, &out);

    if (require_int(rt_transport_enqueue(shard, &data) == RT_TRANSPORT_STATUS_OK,
                    "running enqueue failed")) return 16;
    snapshot = rt_transport_debug_snapshot(shard);
    if (require_int(snapshot.transport_wake_elisions >= 1,
                    "running shard wake was not elided")) return 17;
    (void)rt_transport_try_drain_one(shard, &out);

    rt_shard_lock(shard);
    if (require_int(rt_transport_enqueue(shard, &data) == RT_TRANSPORT_STATUS_OK,
                    "same-shard locked enqueue failed")) return 18;
    snapshot = rt_transport_debug_snapshot(shard);
    if (require_int(snapshot.inbound_len == 1,
                    "same-shard locked snapshot did not see inbound")) return 19;
    if (require_int(rt_transport_try_drain_one(shard, &out) == RT_TRANSPORT_STATUS_OK,
                    "same-shard locked drain failed")) return 20;
    if (require_int(rt_transport_enqueue(other, &data) == RT_TRANSPORT_STATUS_INVALID_ARGUMENT,
                    "cross-shard locked enqueue must fail before taking another shard lock")) return 21;
    rt_shard_unlock(shard);

    if (require_int(rt_transport_shutdown_wake_all(&ex) == 2,
                    "shutdown did not wake every shard")) return 22;
    snapshot = rt_transport_debug_snapshot(shard);
    if (require_int(snapshot.shutdown_wakes == 1,
                    "shutdown wake counter not recorded")) return 23;

    destroy_shard(shard);
    destroy_shard(other);
    return 0;
}
`

	runTransportCProgram(t, "Runtime V2 transport spine behavior", source, nil)
}

func TestRuntimeV2TransportSyncPointAllowlistShape(t *testing.T) {
	root := repoRoot(t)
	header := readTransportContractFile(t, root, "runtime/native/rt_sync_point.h")
	syncPointSource := readTransportContractFile(t, root, "runtime/native/rt_sync_point.c")
	checkScript := readTransportContractFile(t, root, "check_sync_points.sh")
	// The six windows sit in the transport's shard-facing half: admission,
	// park, shutdown and the reply wait live in rt_transport_park.c since the
	// E1 split; rt_transport.c keeps the queues and the wake pipe.
	transportSource := readTransportContractFile(t, root, "runtime/native/rt_transport_park.c")
	workerTurn := readTransportContractFile(t, root, "runtime/native/rt_worker_turn.c")
	asyncPoll := readTransportContractFile(t, root, "runtime/native/rt_async_poll.c")

	names := []string{
		"SP_TRANSPORT_AFTER_DRAIN_BEFORE_PARK",
		"SP_TRANSPORT_AFTER_PARK_BEFORE_RECHECK",
		"SP_TRANSPORT_AFTER_PUBLISH_BEFORE_STATE_LOAD",
		"SP_TRANSPORT_AFTER_STATE_LOAD_BEFORE_WAKE",
		"SP_TRANSPORT_REPLY_WAIT_BEFORE_TASK_SUSPEND",
		"SP_TRANSPORT_SHUTDOWN_BEFORE_WAKE",
	}
	for _, name := range names {
		if !strings.Contains(header, "RT_SYNC_POINT_"+name) {
			t.Fatalf("rt_sync_point.h missing transport sync-point enum %s", name)
		}
		if !strings.Contains(syncPointSource, `return "`+name+`"`) {
			t.Fatalf("rt_sync_point.c missing transport sync-point name %s", name)
		}
		if !strings.Contains(checkScript, "["+name+`]="rt_transport_park.c"`) {
			t.Fatalf("check_sync_points.sh must allow %s only in rt_transport_park.c", name)
		}
		if !strings.Contains(transportSource, "RT_SYNC_POINT("+name+")") {
			t.Fatalf("rt_transport_park.c must expose transport sync-point window %s", name)
		}
	}
	if strings.Contains(checkScript, "rt_net.c\"") &&
		strings.Contains(checkScript, "SP_TRANSPORT_") {
		t.Fatal("transport sync-point windows must not be allowlisted on net wake sources")
	}
	workerDrain := strings.Index(workerTurn,
		"(void)rt_remote_spawn_drain_inbound_locked(ex, shard, RT_TRANSPORT_DRAIN_TURN_LIMIT);")
	workerReady := strings.Index(workerTurn, "while (!ex->shutdown && !worker_next_ready")
	if workerDrain < 0 || workerReady < 0 || workerDrain > workerReady {
		t.Fatal("worker loop must drain a bounded transport slice before ready-work selection")
	}
	if !strings.Contains(asyncPoll,
		"(void)rt_remote_spawn_drain_inbound_locked(ex, shard0, RT_TRANSPORT_DRAIN_TURN_LIMIT);") {
		t.Fatal("single-runner run_ready_one path must drain transport before ready-pop")
	}
}

func TestRuntimeV2TransportProbeRowsDocumented(t *testing.T) {
	root := repoRoot(t)
	taskDoc := readTransportContractFile(t, root, "docs/runtime-v2-epics/13-tasks/04-inbound-transport-spine.md")
	liveness := readTransportContractFile(t, root, "docs/runtime-v2-epics/LIVENESS_PROBES.md")

	for _, needle := range []string{
		"make runtime-v2-transport-contract-check",
		"go test -tags runtime_v2_transport_spine ./internal/vm -run '^TestRuntimeV2TransportSpineAcceptanceRows$'",
		"worker_cv",
	} {
		if !strings.Contains(taskDoc, needle) {
			t.Fatalf("transport contract doc missing %q", needle)
		}
	}
	for _, needle := range []string{
		"transport gate: `make runtime-v2-transport-contract-check` (also called by `make runtime-v2-check`)",
	} {
		if !strings.Contains(liveness, needle) {
			t.Fatalf("LIVENESS_PROBES missing %q", needle)
		}
	}
}

func readTransportContractFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func runTransportCProgram(t *testing.T, label, source string, extraFlags []string) {
	t.Helper()
	root := repoRoot(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 transport contract check: %v", err)
	}
	exe := filepath.Join(t.TempDir(), "transport-contract")
	args := []string{
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Werror",
		"-I" + filepath.Join(root, "runtime", "native"),
	}
	args = append(args, extraFlags...)
	args = append(args,
		"-x",
		"c",
		"-",
		filepath.Join(root, "runtime", "native", "rt_transport.c"),
		filepath.Join(root, "runtime", "native", "rt_transport_park.c"),
		filepath.Join(root, "runtime", "native", "rt_transport_debug.c"),
		filepath.Join(root, "runtime", "native", "rt_resident_bytes.c"),
		filepath.Join(root, "runtime", "native", "rt_lane.c"),
		filepath.Join(root, "runtime", "native", "rt_sync_point.c"),
		"-pthread",
		"-o",
		exe,
	)
	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(source)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s compile failed:\n%s", label, output)
	}

	runCmd := exec.Command(exe)
	runOutput, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s run failed: %v\n%s", label, err, runOutput)
	}
}
