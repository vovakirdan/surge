//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// The commit-vs-shutdown-sweep row (TestRuntimeV2RemoteSelectAbandonEdges)
// also pins the pin half of RV2-DEBT-322 for a body that completes: the
// sweep answers the caller but leaves a pin-holding body its owner
// registration, so the body's completion still finds its pending through
// the task's own pointer and gives the arm pins back, and the channel entry
// they kept alive is reclaimed.
//
// This is that half's Rule 13 control. RV2_DEBT_322_NEGATIVE_CONTROL severs
// every registration in the sweep, as it did before, so the completion finds
// nothing and the same row reads the pin still held. A row that read zero
// either way would be a row nobody had measured.
func TestRuntimeV2RemoteSelectSweptPinNegativeControl(t *testing.T) {
	bin := buildRemotePublicationHarnessWithFlags(t, []string{"-DRV2_DEBT_322_NEGATIVE_CONTROL"})
	env := remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY:block")
	stdout, stderr, code := runRemotePublicationHarness(t, bin, "far-select-commit-vs-shutdown-sweep", env)
	if code == 0 {
		t.Fatalf("negative control unexpectedly passed: the row cannot see a pin the sweep stranded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	want := "commit-vs-sweep race leaked a pin"
	if !strings.Contains(stdout+stderr, want) {
		t.Fatalf("negative control failed for the wrong reason; want %q\nstdout:\n%s\nstderr:\n%s", want, stdout, stderr)
	}
}
