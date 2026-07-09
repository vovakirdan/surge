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
    const rt_shard*) = rt_transport_debug_snapshot;

_Static_assert(RT_TRANSPORT_STATUS_OK == 0, "transport OK status must stay zero");
_Static_assert(RT_TRANSPORT_STATUS_PENDING_SPINE != RT_TRANSPORT_STATUS_OK,
               "pending-spine status must not look successful");
_Static_assert(RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST != RT_TRANSPORT_MSG_NONE,
               "remote spawn request must be a real transport category");
_Static_assert(RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY != RT_TRANSPORT_MSG_NONE,
               "immediate on reply must be a real transport category");
_Static_assert(RT_TRANSPORT_MSG_SHUTDOWN_WAKE != RT_TRANSPORT_MSG_NONE,
               "shutdown wake must be a real transport category");

_Static_assert(sizeof(rt_transport_msg) > 0, "rt_transport_msg must be complete");
_Static_assert(sizeof(((rt_transport_msg*)0)->source_shard_id) == sizeof(uint32_t),
               "source shard id must stay uint32_t");
_Static_assert(sizeof(((rt_transport_msg*)0)->target_shard_id) == sizeof(uint32_t),
               "target shard id must stay uint32_t");
_Static_assert(sizeof(((rt_transport_msg*)0)->generation) == sizeof(uint64_t),
               "generation token must stay uint64_t");
_Static_assert(sizeof(((rt_transport_msg*)0)->payload_len) == sizeof(size_t),
               "payload length must stay size_t");

_Static_assert(sizeof(struct rt_transport_debug_snapshot) > 0,
               "transport debug snapshot must be complete");
_Static_assert(sizeof(((struct rt_transport_debug_snapshot*)0)->inbound_len) == sizeof(size_t),
               "snapshot inbound_len must stay size_t");
_Static_assert(sizeof(((struct rt_transport_debug_snapshot*)0)->transport_wake_writes) ==
                   sizeof(uint64_t),
               "transport wake writes must be counted separately");
_Static_assert(sizeof(((struct rt_transport_debug_snapshot*)0)->transport_wake_elisions) ==
                   sizeof(uint64_t),
               "transport wake elisions must be counted separately");
_Static_assert(sizeof(((struct rt_transport_debug_snapshot*)0)->shutdown_wakes) ==
                   sizeof(uint64_t),
               "shutdown transport wakes must be counted separately");
`

	runFDRegistryStaticCheck(t, "Runtime V2 transport seam static shape check", source)
}

func TestRuntimeV2TransportStubPendingBehavior(t *testing.T) {
	source := `
#include "rt_transport.h"

static int require_int(int condition, int code) {
    return condition ? 0 : code;
}

int main(void) {
    rt_transport_msg msg = {
        .kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST,
        .source_shard_id = 0,
        .target_shard_id = 1,
        .route_id = 11,
        .generation = 22,
        .payload = 0,
        .payload_len = 0,
    };
    rt_transport_msg out = {0};
    struct rt_transport_debug_snapshot snapshot = rt_transport_debug_snapshot(0);

    int err = require_int(snapshot.pending_spine == 1, 1);
    if (err != 0) return err;
    err = require_int(snapshot.inbound_len == 0, 2);
    if (err != 0) return err;
    err = require_int(snapshot.transport_wake_writes == 0, 3);
    if (err != 0) return err;
    err = require_int(snapshot.transport_wake_elisions == 0, 4);
    if (err != 0) return err;
    err = require_int(rt_transport_enqueue(0, &msg) == RT_TRANSPORT_STATUS_PENDING_SPINE, 5);
    if (err != 0) return err;
    err = require_int(rt_transport_try_drain_one(0, &out) == RT_TRANSPORT_STATUS_UNAVAILABLE, 6);
    if (err != 0) return err;
    err = require_int(rt_transport_prepare_shard_park(0) == RT_TRANSPORT_STATUS_PENDING_SPINE, 7);
    if (err != 0) return err;
    rt_transport_mark_shard_running(0);
    err = require_int(rt_transport_shutdown_wake_all(0) == 0, 8);
    if (err != 0) return err;
    return 0;
}
`

	runTransportCProgram(t, "Runtime V2 transport stub pending behavior", source)
}

func TestRuntimeV2TransportSyncPointAllowlistShape(t *testing.T) {
	root := repoRoot(t)
	header := readTransportContractFile(t, root, "runtime/native/rt_sync_point.h")
	syncPointSource := readTransportContractFile(t, root, "runtime/native/rt_sync_point.c")
	checkScript := readTransportContractFile(t, root, "check_sync_points.sh")
	transportSource := readTransportContractFile(t, root, "runtime/native/rt_transport.c")

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
		if !strings.Contains(checkScript, "["+name+`]="rt_transport.c"`) {
			t.Fatalf("check_sync_points.sh must allow %s only in rt_transport.c", name)
		}
		if !strings.Contains(transportSource, "RT_SYNC_POINT("+name+")") {
			t.Fatalf("rt_transport.c must expose stub window %s", name)
		}
	}
	if strings.Contains(checkScript, "rt_net.c\"") &&
		strings.Contains(checkScript, "SP_TRANSPORT_") {
		t.Fatal("transport sync-point windows must not be allowlisted on net wake sources")
	}
}

func TestRuntimeV2TransportPendingProbeRowsDocumented(t *testing.T) {
	root := repoRoot(t)
	taskDoc := readTransportContractFile(t, root, "docs/runtime-v2-epics/13-tasks/03-park-wake-proof.md")
	liveness := readTransportContractFile(t, root, "docs/runtime-v2-epics/LIVENESS_PROBES.md")

	for _, needle := range []string{
		"make runtime-v2-transport-contract-check",
		"go test -tags runtime_v2_transport_spine ./internal/vm -run '^TestRuntimeV2TransportSpineAcceptanceRows$'",
		"pending-spine",
	} {
		if !strings.Contains(taskDoc, needle) {
			t.Fatalf("Task 3 doc missing %q", needle)
		}
	}
	for _, needle := range []string{
		"Task 3 static gate: `make runtime-v2-transport-contract-check`",
		"pending acceptance command: `go test -tags runtime_v2_transport_spine ./internal/vm -run '^TestRuntimeV2TransportSpineAcceptanceRows$'`",
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

func runTransportCProgram(t *testing.T, label, source string) {
	t.Helper()
	root := repoRoot(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 transport contract check: %v", err)
	}
	exe := filepath.Join(t.TempDir(), "transport-contract")
	cmd := exec.Command(
		clang,
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Werror",
		"-I"+filepath.Join(root, "runtime", "native"),
		"-x",
		"c",
		"-",
		filepath.Join(root, "runtime", "native", "rt_transport.c"),
		"-o",
		exe,
	)
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
