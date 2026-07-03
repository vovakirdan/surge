//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The lock-split behavior contracts run at both shard configurations: the
// SURGE_SHARDS=1 compatibility path and a multi-shard path. They must pass
// before the executor lock split starts and after every migration task.
func runLockSplitMode(t *testing.T, mode string) {
	t.Helper()
	binPath := buildRuntimeV2LockSplitHarness(t)
	cases := []struct {
		name string
		env  []string
	}{
		{
			name: "shards-1",
			env: lockSplitEnv(
				"SURGE_SHARDS=1",
				"SURGE_THREADS=2",
				"SURGE_BLOCKING_THREADS=1",
			),
		},
		{
			name: "shards-3",
			env: lockSplitEnv(
				"SURGE_SHARDS=3",
				"SURGE_THREADS=3",
				"SURGE_BLOCKING_THREADS=1",
			),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(binPath, mode)
			cmd.Env = tc.env
			stdout, stderr, exitCode := runCommand(t, cmd, "")
			if exitCode != 0 {
				t.Fatalf("lock-split mode %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					mode, exitCode, stdout, stderr)
			}
		})
	}
}

func lockSplitEnv(values ...string) []string {
	env := os.Environ()
	for _, value := range values {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) != 2 {
			continue
		}
		env = overrideEnvVar(env, parts[0], parts[1])
	}
	return env
}

func TestRuntimeV2LockSplitCrossShardJoin(t *testing.T) {
	runLockSplitMode(t, "cross-join")
}

func TestRuntimeV2LockSplitCrossShardCancel(t *testing.T) {
	runLockSplitMode(t, "cross-cancel")
}

func TestRuntimeV2LockSplitCrossShardChannelFifoAndClose(t *testing.T) {
	runLockSplitMode(t, "cross-channel")
}

func TestRuntimeV2LockSplitChannelCloseWakesParkedReceiver(t *testing.T) {
	runLockSplitMode(t, "close-wakes")
}

func TestRuntimeV2LockSplitBlockingCompletionCrossShard(t *testing.T) {
	runLockSplitMode(t, "blocking-completion")
}

func TestRuntimeV2LockSplitSleepIdleAdvanceMultiShard(t *testing.T) {
	runLockSplitMode(t, "sleep-idle-advance")
}

func TestRuntimeV2LockSplitSelectAcrossOwners(t *testing.T) {
	runLockSplitMode(t, "select-across-owners")
}

func TestRuntimeV2LockSplitTimeoutAcrossOwners(t *testing.T) {
	runLockSplitMode(t, "timeout-across-owners")
}

func TestRuntimeV2LockSplitShutdownWakesAllLanes(t *testing.T) {
	runLockSplitMode(t, "shutdown-liveness")
}
