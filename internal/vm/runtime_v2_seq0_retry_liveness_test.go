//go:build runtime_v2_pending

package vm_test

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func buildRemoteTaskBehaviorHarness(t *testing.T) string {
	t.Helper()
	return buildRemoteTaskBehaviorHarnessWithFlags(t, "remote_task_behavior", nil)
}

func buildRemoteTaskBehaviorHarnessWithFlags(t *testing.T, name string, extraFlags []string) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 remote task check: %v", err)
	}
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), name)
	sources, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob runtime sources: %v", err)
	}
	fixtures, err := filepath.Glob(
		filepath.Join(root, "internal", "vm", "testdata", "remote_task_behavior_*.c"))
	if err != nil {
		t.Fatalf("glob behavior fixtures: %v", err)
	}
	sort.Strings(sources)
	sort.Strings(fixtures)
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-DRT_TEST_SYNC_POINTS", "-pthread",
	}
	args = append(args, extraFlags...)
	args = append(args,
		"-I"+filepath.Join(root, "runtime", "native"),
		"-I"+filepath.Join(root, "internal", "vm", "testdata"),
		"-o", bin)
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

func runSeq0RetryStand(t *testing.T, bin string) (string, string, int) {
	t.Helper()
	env := remotePublicationEnv(
		"SURGE_SHARDS=2",
		"SURGE_THREADS=2",
		"SURGE_REMOTE_DEADLOCK_DETECT=0",
		"SURGE_SYNC_POINT=SP_WAKE_BEFORE_STALE_REMOVAL:block")
	return runRemotePublicationHarness(t, bin, "select-seq0-retry-terminal-drain", env)
}

func TestRuntimeV2LifecycleSeq0RemoteReplyRetryTerminalDrain(t *testing.T) {
	bin := buildRemoteTaskBehaviorHarnessWithFlags(t, "seq0_retry_positive", nil)
	classStdout, classStderr, classCode := runRemotePublicationHarness(
		t, bin, "seq0-retry-classification", remotePublicationEnv())
	if classCode != 0 || !strings.Contains(
		classStderr, "seq0 classification: terminal=5 independent=6") {
		t.Fatalf("seq-0 lifecycle classification is not exact (code=%d)\nstdout:\n%s\nstderr:\n%s",
			classCode, classStdout, classStderr)
	}
	stdout, stderr, code := runSeq0RetryStand(t, bin)
	if code != 0 {
		t.Fatalf("seq-0 remote-reply terminal-drain stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	for _, want := range []string{
		"seq0 window: first=1 retry=2 after_removal=2 all_seq0=1",
		"seq0 complete: caller=done entries=0 requests=1 bodies=1 replies=1 after_removal=2",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("positive stand is missing %q\nstdout:\n%s\nstderr:\n%s", want, stdout, stderr)
		}
	}
}

func TestRuntimeV2LifecycleSeq0RemoteReplyRetryNegativeControl(t *testing.T) {
	bin := buildRemoteTaskBehaviorHarnessWithFlags(
		t, "seq0_retry_negative", []string{"-DRV2_SEQ0_RETRY_NEGATIVE_CONTROL"})
	stdout, stderr, code := runSeq0RetryStand(t, bin)
	if code == 0 {
		t.Fatalf("seq-0 negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	for _, want := range []string{
		"seq0 window: first=1 retry=2 after_removal=0 all_seq0=1",
		"seq0 stranded: pending=ok caller=waiting enqueued=0 entries=0 requests=1 replies=1",
		"seq0 negative control swept the fresh remote-reply registration",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("negative control failed without %q (code=%d)\nstdout:\n%s\nstderr:\n%s",
				want, code, stdout, stderr)
		}
	}
}

func TestRuntimeV2LifecycleSeq0TerminalOwnerDrains(t *testing.T) {
	bin := buildRemoteTaskBehaviorHarnessWithFlags(t, "seq0_terminal_owners", nil)
	rows := []struct {
		mode   string
		marker string
		env    []string
	}{
		{"seq0-blocking-cancel-drain", "row=blocking-cancel entries=0",
			remotePublicationEnv("SURGE_SYNC_POINT=SP_BLOCKING_POP_BEFORE_STATUS:block")},
		{"seq0-spawn-abandon-drain", "row=spawn-abandon entries=0", remotePublicationEnv()},
		{"seq0-remote-teardown-drain", "row=remote-await-teardown entries=0",
			remotePublicationEnv()},
		{"seq0-remote-shutdown-drain", "row=remote-queued-shutdown entries=0",
			remotePublicationEnv()},
	}
	for _, row := range rows {
		t.Run(row.mode, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, row.mode, row.env)
			if code != 0 || !strings.Contains(stderr, row.marker) {
				t.Fatalf("seq-0 terminal owner row failed (code=%d, marker=%q)\nstdout:\n%s\nstderr:\n%s",
					code, row.marker, stdout, stderr)
			}
			if row.mode == "seq0-remote-teardown-drain" &&
				!strings.Contains(stderr, "row=remote-cancel-teardown entries=0") {
				t.Fatalf("seq-0 cancel teardown row is missing\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2LifecycleSeq0TerminalOwnerDrainNegativeControl(t *testing.T) {
	bin := buildRemoteTaskBehaviorHarnessWithFlags(t, "seq0_terminal_owners_mutant",
		[]string{"-DRV2_SEQ0_TERMINAL_RETIRE_NEGATIVE_CONTROL"})
	rows := []struct {
		mode string
		env  []string
	}{
		{"seq0-blocking-cancel-drain",
			remotePublicationEnv("SURGE_SYNC_POINT=SP_BLOCKING_POP_BEFORE_STATUS:block")},
		{"seq0-spawn-abandon-drain", remotePublicationEnv()},
		{"seq0-remote-teardown-drain", remotePublicationEnv()},
		{"seq0-remote-shutdown-drain", remotePublicationEnv()},
	}
	for _, row := range rows {
		t.Run(row.mode, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, row.mode, row.env)
			if code == 0 || !strings.Contains(stderr, "seq0 terminal mutant stranded:") ||
				!strings.Contains(stderr,
					"seq0 terminal-retire negative control stranded registrations") {
				t.Fatalf("Rule-13 terminal-drain mutant missed its strand (code=%d)\nstdout:\n%s\nstderr:\n%s",
					code, stdout, stderr)
			}
		})
	}
}
