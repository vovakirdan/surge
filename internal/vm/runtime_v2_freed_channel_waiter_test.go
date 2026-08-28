//go:build runtime_v2_pending

package vm_test

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// RV2-DEBT-199 — a channel waiter key outlives its channel, and the ROUTING
// path used to dereference it.
//
// rt_waiter_route.c resolved WAKER_CHAN_SEND / WAKER_CHAN_RECV by casting
// key.id back to rt_channel* and reading the object, under the claim that
// "channels are never freed". rt_far_channel.c's release_entry calls
// rt_channel_free once the last lease and the last pin are gone. The two meet
// in wake_task_with_policy's deferred stale-key removal: the wake captures a
// parked task's channel key under the owner shard lock, drops that lock, and
// only then routes the key — and in that gap the task it just woke can run to
// completion, and its completion is exactly what unpins the registry entry and
// frees the channel.
//
// The window is SP_WAKE_BEFORE_STALE_REMOVAL, which already existed. The row
// is a pair, never a single green run: the negative-control build restores the
// dereference and MUST be caught by ASan at rt_channel_owner_shard_id.
const freedChannelWaiterSyncPoint = "SP_WAKE_BEFORE_STALE_REMOVAL"

const freedChannelWaiterMode = "anchored-freed-channel-waiter"

func buildFreedChannelWaiterHarness(t *testing.T, negativeControl bool) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping freed-channel waiter proof")
	}
	root := repoRoot(t)
	name := "freed_channel_waiter"
	if negativeControl {
		name += "_negative"
	}
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
		"-g", "-fno-omit-frame-pointer", "-fsanitize=address",
		"-I" + filepath.Join(root, "runtime", "native"),
		"-I" + filepath.Join(root, "internal", "vm", "testdata"),
		"-o", bin,
	}
	if negativeControl {
		// TWO controls, because there are now two defences and the pre-fix
		// world is the absence of both. The first restores the dereference of
		// a channel key during routing. The second removes the internal pin a
		// store entry holds: with it in place the stale registration keeps the
		// object alive, the restored dereference reads live storage, and this
		// build would exit cleanly -- a proof that passes without running.
		// Neither control substitutes for the other: the routing path is
		// reached with a key a caller merely CARRIES, which is a copy nothing
		// counts, so the fix under test here is still the one that must hold.
		args = append(args,
			"-DRV2_DEBT_199_NEGATIVE_CONTROL",
			"-DRV2_CHANNEL_PIN_NEGATIVE_CONTROL")
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
		t.Fatalf("build freed-channel waiter harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	return bin
}

func runFreedChannelWaiterHarness(t *testing.T, bin string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, freedChannelWaiterMode)
	// The row deliberately parks an anchored body whose only consumer is the
	// harness thread, so the remote-deadlock detector is opted out exactly as
	// the sibling anchored rows do.
	cmd.Env = lifecycleEnv(
		"SURGE_SHARDS=2",
		"SURGE_THREADS=2",
		"SURGE_REMOTE_DEADLOCK_DETECT=0",
		"SURGE_SYNC_POINT="+freedChannelWaiterSyncPoint+":block",
	)
	return runCommand(t, cmd, "")
}

func TestRuntimeV2FreedChannelWaiterRouting(t *testing.T) {
	t.Run("negative_control_reproduces_the_use_after_free", func(t *testing.T) {
		bin := buildFreedChannelWaiterHarness(t, true)
		stdout, stderr, code := runFreedChannelWaiterHarness(t, bin)
		if code == 0 {
			t.Fatalf("negative control exited cleanly; the proof is vacuous\nstdout:\n%s\nstderr:\n%s",
				stdout, stderr)
		}
		combined := stdout + stderr
		if !strings.Contains(combined, "AddressSanitizer: heap-use-after-free") {
			t.Fatalf("negative control failed without the use-after-free (code=%d)\nstdout:\n%s\nstderr:\n%s",
				code, stdout, stderr)
		}
		for _, want := range []string{"rt_channel_owner_shard_id", "rt_waiter_key_shard", "rt_channel_free"} {
			if !strings.Contains(combined, want) {
				t.Fatalf("negative control report is missing %q; it may be a different defect\nstdout:\n%s\nstderr:\n%s",
					want, stdout, stderr)
			}
		}
	})
	t.Run("routing_reads_the_key_not_the_channel", func(t *testing.T) {
		bin := buildFreedChannelWaiterHarness(t, false)
		stdout, stderr, code := runFreedChannelWaiterHarness(t, bin)
		if code != 0 {
			t.Fatalf("freed-channel waiter row failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
				code, stdout, stderr)
		}
		if strings.Contains(stdout+stderr, "AddressSanitizer") {
			t.Fatalf("sanitizer report on the fixed build\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
		}
	})
}
