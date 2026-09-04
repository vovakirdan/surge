package vm_test

import (
	"testing"
	"time"
)

// Rule 13 for RV2-DEBT-331, the leak the Task 9 matrix found: an anchored
// body cancelled while parked in `on ch { ch.recv() }` never released its
// state, because the anchored operations handed 0 as the state's descriptor
// id to the yield and the cancelled return, so the stash `mark_done`
// releases stayed empty.
//
// The fix passes the real id (`rt_remote_task_anchored_state_type_id_current`),
// and this is the mutant that shows the matrix could tell: with
// RV2_DEBT_331_NEGATIVE_CONTROL the call answers 0 again and the share x
// cancel cell -- the same program, the same shard count, the same memcheck
// reading -- goes back to losing the state.
//
// The control has to reach the RUNTIME of the program under test, which no C
// stand can do for this defect: the leak is only observable through a
// compiled crossing. SURGE_INTERNAL_RUNTIME_NEGATIVE_CONTROL carries it into
// that program's own runtime build (internal/buildpipeline), the way
// SURGE_INTERNAL_TEST_SYNC_POINTS carries the sync-point build; every
// runtime object of this build, and only this build, is compiled with it.
func TestRuntimeV2Task9AnchoredStateReleaseNegativeControl(t *testing.T) {
	t.Setenv("SURGE_INTERNAL_RUNTIME_NEGATIVE_CONTROL", "RV2_DEBT_331_NEGATIVE_CONTROL")
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2Task9ShareCancelSource, nil)

	// Two shards: the cancel reaches a body parked on another shard's lane,
	// which is where the matrix read the sixteen bytes.
	env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "2")
	env = overrideEnvVar(env, "SURGE_THREADS", "2")

	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 240*time.Second)
	if exitCode != 0 {
		t.Fatalf("the mutant must still RUN the program, and it exited %d:\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("read the memcheck total: %v\nstderr:\n%s", err, stderr)
	}
	if bytesLost == 0 && blocksLost == 0 {
		t.Fatalf("the negative control is green: with the anchored state's descriptor id "+
			"forced back to 0 the cancelled body must lose its state, and memcheck read a "+
			"strict zero. Either the control no longer reaches the runtime build, or the "+
			"matrix cell cannot tell the fix from its absence.\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
}
