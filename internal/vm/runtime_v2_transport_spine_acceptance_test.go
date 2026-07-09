//go:build runtime_v2_transport_spine

package vm_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeV2TransportSpineAcceptanceRows(t *testing.T) {
	rows := []string{
		"lost-wake seq-cst proof",
		"lost-wake negative skip recheck",
		"lost-wake negative relaxed park ordering",
		"wake elision running target",
		"PARKED wake exactly once",
		"wake elision negative parked wake skipped",
		"wake elision negative running wake written",
		"PARKED-with-inbound-work invariant",
		"PARKED-with-inbound-work negative",
		"shutdown wakes parked shards and reply waiters",
		"shutdown negative no wake",
		"reply wait suspends task instead of parking shard",
		"reply wait negative shard park",
	}
	for _, row := range rows {
		row := row
		t.Run(row, func(t *testing.T) {
			stderr := runTransportSpineAcceptanceProgram(t)
			t.Fatalf("pending-spine: %s requires Task 4 inbound transport spine (rt_transport_enqueue returned RT_TRANSPORT_STATUS_PENDING_SPINE)\nstderr:\n%s",
				row, stderr)
		})
	}
}

func runTransportSpineAcceptanceProgram(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 transport acceptance rows: %v", err)
	}
	exe := filepath.Join(t.TempDir(), "transport-spine-acceptance")
	source := `
#include <stdio.h>

#include "rt_transport.h"

int main(void) {
    rt_transport_msg msg = {
        .kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST,
        .source_shard_id = 0,
        .target_shard_id = 1,
        .route_id = 1,
        .generation = 1,
        .payload = 0,
        .payload_len = 0,
    };
    rt_transport_status status = rt_transport_enqueue(0, &msg);
    if (status == RT_TRANSPORT_STATUS_PENDING_SPINE) {
        fputs("pending-spine: rt_transport_enqueue has no inbound spine yet\n", stderr);
        return 77;
    }
    return status == RT_TRANSPORT_STATUS_OK ? 0 : 78;
}
`
	buildCmd := exec.Command(
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
	buildCmd.Dir = root
	buildCmd.Stdin = strings.NewReader(source)
	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("transport spine acceptance compile failed:\n%s", buildOutput)
	}

	runCmd := exec.Command(exe)
	runOutput, err := runCmd.CombinedOutput()
	if err == nil {
		t.Fatalf("transport spine acceptance row unexpectedly passed; Task 4 must replace this pending row with the real park/wake proof")
	}
	return string(runOutput)
}
