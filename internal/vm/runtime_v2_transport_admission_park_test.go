//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// The producer park is what replaced the caller-visible QUEUE_FULL: a request
// that finds its target's data lane exhausted parks its task on the shard's
// slot key and answers PENDING, and a data slot freed on that lane wakes it.
// The positive row lives in the behaviour table
// (anchored-saturation-parks-the-producer-and-a-freed-slot-wakes-it) and pins
// the numbers: one park, one wake, two slot-credit stalls (the refusal and the
// verify retry), every reply reservation given back.
//
// This is its Rule 13 control. RV2_DEBT_031_NEGATIVE_CONTROL restores the
// shape the park replaced -- drain the target once, retry once, hand the
// refusal to the caller -- so the flooded caller answers QUEUE_FULL, no park
// is recorded, and the same stand goes red on its first assertion. A park
// that could not be told from the old refusal by that stand would be a park
// nobody had measured.
func TestRuntimeV2TransportSaturationParkNegativeControl(t *testing.T) {
	bin := buildRemoteTaskBehaviorHarnessWithFlags(t,
		"remote_task_behavior_admission_park_negative",
		[]string{"-DRV2_DEBT_031_NEGATIVE_CONTROL"})
	env := remotePublicationEnv(
		"SURGE_SHARDS=2", "SURGE_THREADS=2",
		"SURGE_REMOTE_DEADLOCK_DETECT=0",
	)
	stdout, stderr, code := runRemotePublicationHarness(t, bin, "anchored-saturation-parks", env)
	if code == 0 {
		t.Fatalf("negative control unexpectedly passed: the stand cannot tell the park from the old refusal\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	want := "saturated data lane did not park the producer"
	if !strings.Contains(stdout+stderr, want) {
		t.Fatalf("negative control failed for the wrong reason; want %q\nstdout:\n%s\nstderr:\n%s", want, stdout, stderr)
	}
}
