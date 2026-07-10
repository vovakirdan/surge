//go:build runtime_v2_pending

package vm_test

import (
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

func TestRuntimeV2RemoteTaskBehavior(t *testing.T) {
	bin := buildRemoteTaskBehaviorHarness(t)
	rows := []struct {
		name string
		mode string
		env  []string
	}{
		{
			name: "already-done-immediate-reply",
			mode: "already-done",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "stale-request-and-reply",
			mode: "stale",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "completion-before-owner-register",
			mode: "race-before",
			env: remotePublicationEnv(
				"SURGE_SHARDS=1", "SURGE_THREADS=2",
				"SURGE_SYNC_POINT=SP_REMOTE_TASK_BEFORE_OWNER_REGISTER:block",
			),
		},
		{
			name: "completion-after-owner-register",
			mode: "race-after",
			env: remotePublicationEnv(
				"SURGE_SHARDS=1", "SURGE_THREADS=2",
				"SURGE_SYNC_POINT=SP_REMOTE_TASK_AFTER_OWNER_REGISTER:block",
			),
		},
		{
			name: "unconsumed-handle-teardown",
			mode: "teardown",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "cancel-before-publication-ack",
			mode: "pre-ack-cancel",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_ACK:block",
			),
		},
		{
			name: "queue-failure-restores-lease",
			mode: "queue-failure",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1"),
		},
		{
			name: "shutdown-wakes-reply-waiters-on-all-shards",
			mode: "shutdown-waiters",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, row.mode, row.env)
			if code != 0 {
				t.Fatalf("remote task mode %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					row.mode, code, stdout, stderr)
			}
		})
	}
}

func buildRemoteTaskBehaviorHarness(t *testing.T) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 remote task check: %v", err)
	}
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "remote_task_behavior")
	sources, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob runtime sources: %v", err)
	}
	fixtures, err := filepath.Glob(filepath.Join(root, "internal", "vm", "testdata", "remote_task_behavior_*.c"))
	if err != nil {
		t.Fatalf("glob behavior fixtures: %v", err)
	}
	sort.Strings(sources)
	sort.Strings(fixtures)
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-DRT_TEST_SYNC_POINTS", "-pthread",
		"-I" + filepath.Join(root, "runtime", "native"),
		"-I" + filepath.Join(root, "internal", "vm", "testdata"),
		"-o", bin,
	}
	args = append(args, fixtures...)
	for _, source := range sources {
		if filepath.Base(source) != "rt_entry.c" {
			args = append(args, source)
		}
	}
	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("build remote task harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	return bin
}
