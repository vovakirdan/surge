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
		{
			name: "immediate-on-trace-equivalence-and-owner-proof",
			mode: "immediate-basic",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "immediate-on-distributed-non-caller",
			mode: "immediate-distributed",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "immediate-on-invalid-shard-cancelled-resume",
			mode: "immediate-invalid-shard",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "immediate-on-stale-request-rejected",
			mode: "immediate-stale",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "immediate-on-caller-cancel-exactly-one-resume",
			mode: "immediate-cancel-race",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				"SURGE_SYNC_POINT=SP_IMMEDIATE_ON_BEFORE_PUBLISH:block",
			),
		},
		{
			name: "immediate-on-shutdown-wakes-reply-waiter",
			mode: "immediate-shutdown",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "immediate-on-self-crossing-uses-transport-at-one-shard",
			mode: "immediate-self-crossing",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1"),
		},
		{
			name: "far-channel-mint-resolve-release-round-trip",
			mode: "channel-create",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "far-channel-kind-tag-blocks-registry-aliasing",
			mode: "channel-kind-aliasing",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "far-channel-shutdown-releases-live-entries",
			mode: "channel-shutdown-release",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "far-channel-self-crossing-create-uses-transport",
			mode: "channel-create-self",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1"),
		},
		{
			name: "anchored-send-round-trip-with-trace-proof",
			mode: "anchored-send-round-trip",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "anchored-stale-and-wrong-kind-answer-without-a-body",
			mode: "anchored-stale-anchor",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "anchored-full-channel-parks-body-not-dispatcher",
			mode: "anchored-full-channel",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "anchored-close-wakes-parked-recv-with-closed-outcome",
			mode: "anchored-close-vs-recv",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "anchored-caller-cancel-cannot-resurrect-parked-body",
			mode: "anchored-cancel-parked-body",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "anchored-owner-teardown-fails-caller-deterministically",
			mode: "anchored-owner-teardown",
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
