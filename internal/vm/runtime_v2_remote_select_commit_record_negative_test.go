//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// The commit-vs-shutdown-sweep row (TestRuntimeV2RemoteSelectAbandonEdges)
// pins RV2-DEBT-069: the body records the local select's commit through the
// pending it holds a reference on, so a sweep that resolved the pending
// inside the commit window neither takes the record nor frees the arm table
// under the body.
//
// This is its Rule 13 control. RV2_DEBT_069_NEGATIVE_CONTROL restores the
// registry scan the record used to go through -- keyed on a PENDING status
// the sweep has already changed -- so the record is lost and the same row
// reads NO_COMMIT where it must read the winner. A row that could not tell
// the pointer from the scan would be a row nobody had measured.
func TestRuntimeV2RemoteSelectCommitRecordNegativeControl(t *testing.T) {
	bin := buildRemotePublicationHarnessWithFlags(t, []string{"-DRV2_DEBT_069_NEGATIVE_CONTROL"})
	env := remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY:block")
	stdout, stderr, code := runRemotePublicationHarness(t, bin, "far-select-commit-vs-shutdown-sweep", env)
	if code == 0 {
		t.Fatalf("negative control unexpectedly passed: the row cannot tell a lost commit record from a kept one\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	want := "the sweep took the commit record from the winner"
	if !strings.Contains(stdout+stderr, want) {
		t.Fatalf("negative control failed for the wrong reason; want %q\nstdout:\n%s\nstderr:\n%s", want, stdout, stderr)
	}
}
