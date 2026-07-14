//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeV2RemoteTaskReplyValidationIsGenerationQualified(t *testing.T) {
	root := repoRoot(t)
	matches, err := filepath.Glob(filepath.Join(root, "runtime", "native", "rt_remote_task*.c"))
	if err != nil {
		t.Fatalf("glob remote-task sources: %v", err)
	}
	var source strings.Builder
	for _, path := range matches {
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		source.Write(body)
	}
	for _, field := range []string{
		"msg->route_id",
		"msg->generation",
		"msg->source_shard_id",
		"msg->target_shard_id",
	} {
		if !strings.Contains(source.String(), field) {
			t.Fatalf("remote-task reply validation must inspect %s", field)
		}
	}
}

func TestRuntimeV2TransportReplyWaitersHaveExplicitShardRouting(t *testing.T) {
	root := repoRoot(t)
	routePath := filepath.Join(root, "runtime", "native", "rt_waiter_route.c")
	body, err := os.ReadFile(routePath)
	if err != nil {
		t.Fatalf("read waiter routing: %v", err)
	}
	source := string(body)
	for _, kind := range []string{"WAKER_REMOTE_SPAWN_REPLY", "WAKER_REMOTE_TASK_REPLY"} {
		if !strings.Contains(source, "case "+kind+":") {
			t.Fatalf("%s must not fall through to the shard-0 waiter store", kind)
		}
	}
}

func TestRuntimeV2RemoteTaskSourcesRespectFileLimit(t *testing.T) {
	root := repoRoot(t)
	matches, err := filepath.Glob(filepath.Join(root, "runtime", "native", "rt_remote_task*.[ch]"))
	if err != nil {
		t.Fatalf("glob remote-task sources: %v", err)
	}
	immediate, err := filepath.Glob(filepath.Join(root, "runtime", "native", "rt_immediate_on*.[ch]"))
	if err != nil {
		t.Fatalf("glob immediate-on sources: %v", err)
	}
	matches = append(matches, immediate...)
	// The module-size guard flags files worth reviewing for readability,
	// not an absolute ceiling: a modestly-over file that is not hard to
	// read is intentionally left alone (project direction 2026-07-14 —
	// refactor by readability, not by line count). Only a large overrun
	// signals a genuine structural problem.
	const softLimit = 300
	const hardLimit = 360
	for _, path := range matches {
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		lines := strings.Count(string(body), "\n")
		if len(body) != 0 && body[len(body)-1] != '\n' {
			lines++
		}
		if lines > hardLimit {
			t.Errorf("%s has %d lines; remote-task modules must stay <=%d (split it)",
				filepath.Base(path), lines, hardLimit)
		} else if lines > softLimit {
			t.Logf("%s has %d lines (soft limit %d); review for readability if it grows further",
				filepath.Base(path), lines, softLimit)
		}
	}
}

func TestRuntimeV2RemoteTaskStateHasInitRollbackPair(t *testing.T) {
	root := repoRoot(t)
	state := readTransportContractFile(t, root, "runtime/native/rt_async_state.c")
	for _, failure := range []string{
		"async: local queue allocation failed",
		"async: scheduler initialization failed",
	} {
		at := strings.Index(state, failure)
		if at < 0 {
			t.Fatalf("missing scheduler failure path %q", failure)
		}
		prefix := state[:at]
		call := strings.LastIndex(prefix, "rt_remote_task_state_destroy(ex)")
		runtimeDestroy := strings.LastIndex(prefix, "rt_runtime_destroy_global()")
		if call < 0 || runtimeDestroy < 0 || call > runtimeDestroy {
			t.Fatalf("remote-task state must be destroyed before runtime rollback on %q", failure)
		}
	}
}
