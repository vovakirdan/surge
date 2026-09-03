//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// The resident-byte row (resident-bytes-of-one-crossing-are-exact-and-given-
// back, in the behaviour table) pins the widths one crossing charges and that
// every balance is back at zero once it completed.
//
// This is its Rule 13 control. RV2_E5_RESIDENT_NEGATIVE_CONTROL drops the one
// release at the publication-accepted handoff -- the state stays charged to
// the transport after the body owns it -- so the same stand goes red on its
// first assertion. A ledger that read zero with that release gone would be a
// ledger nobody had measured.
func TestRuntimeV2RemoteTaskResidentBytesNegativeControl(t *testing.T) {
	bin := buildRemoteTaskBehaviorHarnessWithFlags(t,
		"remote_task_behavior_resident_negative",
		[]string{"-DRV2_E5_RESIDENT_NEGATIVE_CONTROL"})
	env := remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2")
	stdout, stderr, code := runRemotePublicationHarness(t, bin, "resident-handoff-balance", env)
	if code == 0 {
		t.Fatalf("negative control unexpectedly passed: the row cannot see a payload the handoff kept charged\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	want := "payload bytes still resident after the crossing completed"
	if !strings.Contains(stdout+stderr, want) {
		t.Fatalf("negative control failed for the wrong reason; want %q\nstdout:\n%s\nstderr:\n%s", want, stdout, stderr)
	}
}
