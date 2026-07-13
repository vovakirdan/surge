//go:build runtime_v2_pending

package vm_test

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The channel counterparty here is the harness main thread — an
				// external actor the quiescence scan cannot see.
				"SURGE_REMOTE_DEADLOCK_DETECT=0",
			),
		},
		{
			name: "anchored-close-wakes-parked-recv-with-closed-outcome",
			mode: "anchored-close-vs-recv",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The channel counterparty here is the harness main thread — an
				// external actor the quiescence scan cannot see.
				"SURGE_REMOTE_DEADLOCK_DETECT=0",
			),
		},
		{
			name: "anchored-caller-cancel-cannot-resurrect-parked-body",
			mode: "anchored-cancel-parked-body",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The channel counterparty here is the harness main thread — an
				// external actor the quiescence scan cannot see.
				"SURGE_REMOTE_DEADLOCK_DETECT=0",
			),
		},
		{
			name: "anchored-owner-teardown-fails-caller-deterministically",
			mode: "anchored-owner-teardown",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The channel counterparty here is the harness main thread — an
				// external actor the quiescence scan cannot see.
				"SURGE_REMOTE_DEADLOCK_DETECT=0",
			),
		},
		{
			name: "anchored-release-during-block-keeps-pinned-channel-alive",
			mode: "anchored-pin-vs-release",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "anchored-helper-protocol-park-retry-close",
			mode: "anchored-helper-protocol",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The parked send's counterparty is the harness main thread.
				"SURGE_REMOTE_DEADLOCK_DETECT=0",
			),
		},
		{
			name: "anchored-queue-full-bounded-with-control-progress",
			mode: "anchored-queue-full",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The parked block's counterparty is the harness main thread.
				"SURGE_REMOTE_DEADLOCK_DETECT=0",
			),
		},
		{
			name: "anchored-lifecycle-churn-leaves-no-residue",
			mode: "anchored-leak-audit",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "anchored-cross-producer-order-is-lane-order-not-start-order",
			mode: "anchored-cross-producer-order",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "share-sibling-lease-round-trip-with-trace-proof",
			mode: "share-round-trip",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "share-per-lease-release-is-independent-and-exact",
			mode: "share-release-independence",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "share-through-released-lease-answers-stale",
			mode: "share-from-released-lease",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "share-pin-outlives-all-leases-to-the-reply-edge",
			mode: "share-pin-outlives-leases",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "share-owner-teardown-goes-stale-for-every-lease",
			mode: "share-teardown",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "share-runnable-holder-suppresses-the-deadlock-panic",
			mode: "share-no-deadlock-when-runnable",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "select-ready-arm-wins-without-parking",
			mode: "select-ready-first",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "select-parked-selector-wakes-on-send-exactly-once",
			mode: "select-park-then-send",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "select-tie-break-matches-the-local-scan-order",
			mode: "select-tie-break",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "select-closed-arm-wins-the-registration-race",
			mode: "select-close-before",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "select-close-wakes-the-parked-selector-exactly-once",
			mode: "select-park-then-close",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "select-cancel-before-binding-resumes-cancelled",
			mode: "select-cancel-unbound",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "select-cancel-of-a-parked-selector-resumes-cancelled",
			mode: "select-cancel-parked",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "select-cancel-vs-send-yields-exactly-one-terminal",
			mode: "select-cancel-vs-send",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "select-spurious-caller-wake-mints-no-second-request",
			mode: "select-retry-single-body",
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

// A self-deadlocked anchored block — the channel's only consumer is the
// caller suspended on the block's own reply — must abort the process with
// the actionable remote-channel-deadlock panic instead of hanging. The
// single-worker configuration is excluded deliberately: it starts no worker
// threads, so the park-edge check never runs there and quiescence is the
// driver-side "async deadlock" panic's territory.
func TestRuntimeV2RemoteChannelSelfDeadlockPanics(t *testing.T) {
	bin := buildRemoteTaskBehaviorHarness(t)
	rows := []struct {
		name string
		mode string
		env  []string
		want string
	}{
		{
			name: "one-shard-two-workers",
			mode: "anchored-self-deadlock",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=2"),
			want: "parked on channel send",
		},
		{
			name: "two-shards",
			mode: "anchored-self-deadlock",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
			want: "parked on channel send",
		},
		{
			name: "two-holders-panic-names-the-lease-topology",
			mode: "share-deadlock-two-holders",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
			want: "has 2 leases but every holder is idle",
		},
		{
			name: "deadlock-still-fires-after-the-peer-released",
			mode: "share-deadlock-after-peer-release",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
			want: "consumer is the suspended caller",
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, row.mode, row.env)
			if code == 0 {
				t.Fatalf("deadlock mode exited cleanly\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			combined := stdout + stderr
			if !strings.Contains(combined, "remote channel deadlock") {
				t.Fatalf("missing deadlock panic (code=%d)\nstdout:\n%s\nstderr:\n%s",
					code, stdout, stderr)
			}
			if !strings.Contains(combined, row.want) {
				t.Fatalf("panic missing %q\nstdout:\n%s\nstderr:\n%s", row.want, stdout, stderr)
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
