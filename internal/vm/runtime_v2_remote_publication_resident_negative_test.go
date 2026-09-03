//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// Every publication-harness mode that passes also leaves nothing resident
// on the source side (resident_quiescent_after, run by the harness's main
// after every mode): the byte half of the exact-return contract, on every
// edge the harness takes.
//
// This is that check's Rule 13 control. RV2_E5_RESIDENT_NEGATIVE_CONTROL
// keeps a shipped state charged past the publication-accepted handoff, so a
// mode whose crossing succeeds -- the immediate happy path -- passes its own
// assertions and then fails the balance. A check that read zero with that
// release gone would be a check nobody had measured.
func TestRuntimeV2RemotePublicationResidentBalanceNegativeControl(t *testing.T) {
	bin := buildRemotePublicationHarnessWithFlags(t, []string{"-DRV2_E5_RESIDENT_NEGATIVE_CONTROL"})
	env := remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, code := runRemotePublicationHarness(t, bin, "anchored-happy-path", env)
	if code == 0 {
		t.Fatalf("negative control unexpectedly passed: the balance check cannot see a state the handoff kept charged\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	want := "bytes still resident after the mode ran"
	if !strings.Contains(stdout+stderr, want) {
		t.Fatalf("negative control failed for the wrong reason; want %q\nstdout:\n%s\nstderr:\n%s", want, stdout, stderr)
	}
}
