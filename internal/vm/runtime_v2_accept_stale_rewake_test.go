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

// buildRuntimeV2AcceptStaleRewakeStand compiles testdata/accept_stale_rewake_stand.c
// against the full native runtime (rt_entry.c excluded), mirroring
// buildRuntimeV2LifecycleHarnessWithFlags. negativeControl restores the
// pre-fix behaviour through RV2_DEBT_313_NEGATIVE_CONTROL, which is what makes
// this stand non-vacuous: the same stand must go red on that build.
func buildRuntimeV2AcceptStaleRewakeStand(t *testing.T, negativeControl bool) string {
	t.Helper()

	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping native accept stale-rewake stand")
	}

	root := repoRoot(t)
	standPath := filepath.Join(root, "internal", "vm", "testdata", "accept_stale_rewake_stand.c")
	if _, statErr := os.Stat(standPath); statErr != nil {
		t.Fatalf("stand source missing: %v", statErr)
	}

	name := "accept_stale_rewake_stand"
	args := []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-pthread"}
	if negativeControl {
		name += "_negative"
		args = append(args, "-DRV2_DEBT_313_NEGATIVE_CONTROL")
	}
	binPath := filepath.Join(t.TempDir(), name)

	sources, globErr := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if globErr != nil {
		t.Fatalf("glob runtime sources: %v", globErr)
	}
	sort.Strings(sources)

	args = append(args, "-I"+filepath.Join(root, "runtime", "native"), "-o", binPath, standPath)
	for _, src := range sources {
		if filepath.Base(src) == "rt_entry.c" {
			continue
		}
		args = append(args, src)
	}

	buildCmd := exec.Command(clang, args...)
	buildCmd.Dir = root
	buildOut, buildErr, buildCode := runCommand(t, buildCmd, "")
	if buildCode != 0 {
		t.Fatalf("build accept stale-rewake stand failed (negative=%v, code=%d)\nstdout:\n%s\nstderr:\n%s",
			negativeControl, buildCode, buildOut, buildErr)
	}
	return binPath
}

func runRuntimeV2AcceptStaleRewakeStand(t *testing.T, binPath string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binPath)
	cmd.Env = append(os.Environ(), "SURGE_SHARDS=2", "SURGE_THREADS=2")
	return runCommand(t, cmd, "")
}

// TestRuntimeV2AcceptStaleSiblingWakeDoesNotReownTheAcceptor pins the
// RV2-DEBT-313 owner transition.
//
// A multi-member listener registers one accept waiter per member, and
// add_waiter files each under the shard that owns that member's fd, so a
// single accept task sits in several shards' waiter stores at once. The shard
// whose member became readable delivers readiness; the task accepts,
// self-places onto that member's shard (rt_net_place_current_task_on_owner)
// and clears its wait keys, which removes its sibling entries. But
// collect-then-wake pops each batch under the store shard's lock and releases
// it before taking control, so a losing shard's poller can still hold the task
// id. Applying the accept re-own from that already-popped batch overwrites the
// owner the accept just placed, leaving the accepted conn owned by one shard
// while every task that handles it is placed on another -- and the conn's
// first use then fails net_conn_owner_local and is counted as
// non_owner_conn_denied.
func TestRuntimeV2AcceptStaleSiblingWakeDoesNotReownTheAcceptor(t *testing.T) {
	stdout, stderr, code := runRuntimeV2AcceptStaleRewakeStand(
		t, buildRuntimeV2AcceptStaleRewakeStand(t, false))
	if code != 0 {
		t.Fatalf("a stale sibling accept wake re-owned the acceptor away from the shard it accepted on (exit=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	if !strings.Contains(stdout, "OK: stale accept wake left owner=0") {
		t.Fatalf("stand did not report its verdict; it may have exited without asserting\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
}

// TestRuntimeV2AcceptStaleSiblingWakeStandFailsOnRevert is the non-vacuity
// proof for the test above: on a build that restores the pre-fix behaviour the
// same stand must go red, and red for the stated reason. Without this, a stand
// that silently stopped exercising the path would keep reporting green.
func TestRuntimeV2AcceptStaleSiblingWakeStandFailsOnRevert(t *testing.T) {
	stdout, stderr, code := runRuntimeV2AcceptStaleRewakeStand(
		t, buildRuntimeV2AcceptStaleRewakeStand(t, true))
	if code == 0 {
		t.Fatalf("negative control passed: the stand no longer detects the stale accept re-own, so its green is vacuous\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if !strings.Contains(stdout, "FAIL: stale accept wake re-owned the task") {
		t.Fatalf("negative control failed for some other reason than the owner transition\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
}
