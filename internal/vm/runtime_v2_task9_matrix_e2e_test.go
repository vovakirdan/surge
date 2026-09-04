package vm_test

import (
	"strings"
	"testing"
	"time"
)

// Epic 21 Task 9 / RV2-DEBT-125: the named vertical x edge-class matrix on
// typed carriers, at SURGE_SHARDS=1, 2 and 8. Four verticals (an owned
// @shard_movable capture migrating into a far task, a far channel shared
// across tasks, a far select with a non-copy send arm, a non-copy far
// channel) against four edge classes (happy, cancel, refusal,
// teardown-buffered). Every program row runs to its own marker and, under
// memcheck, to "definitely lost: 0 bytes in 0 blocks" with no error report
// (the programs live in runtime_v2_task9_matrix_sources_test.go):
// the property of every cell is that whatever crossed is reclaimed exactly
// once whichever way the crossing ended. The cancel rows accept either
// answer of the race they start (a cancel that lands after the far body has
// committed does not revoke it; the ruling of 2026-08 and the fail-fast
// judge's lesson of 2026-09-03) and hold the reclamation instead.
//
// One cell is not a program: migration x refusal, the runtime refusing to
// admit a far task, is decided below the language (the target lane's
// admission), and its rows are the C stands of
// runtime_v2_remote_task_behavior_test.go (refusal, stale generation) which
// pin the payload's single discharge at the ABI; a program cannot reach a
// refused admission on purpose, so this table says so instead of faking one.
type task9Cell struct {
	vertical string
	edge     string
	marker   string
	source   string
}

func runtimeV2Task9Cells() []task9Cell {
	return []task9Cell{
		{"migration", "happy", "migration-ok", runtimeV2MigrationSource},
		{"migration", "cancel", "migration-cancel-ok", runtimeV2Task9MigrationCancelSource},
		{"migration", "teardown-buffered", "migration-teardown-ok", runtimeV2Task9MigrationTeardownSource},
		{"share", "happy", "share-fanout-ok", runtimeV2ShareSource},
		{"share", "cancel", "share-cancel-ok", runtimeV2Task9ShareCancelSource},
		{"share", "refusal", "share-refusal-ok", runtimeV2Task9ShareRefusalSource},
		{"share", "teardown-buffered", "share-teardown-ok", runtimeV2Task9ShareTeardownSource},
		{"select", "happy", "far-select-noncopy-ok", runtimeV2FarSelectNonCopySource},
		{"select", "cancel", "far-select-cancel-noncopy-ok", runtimeV2FarSelectCancelNonCopySource},
		{"select", "refusal", "select-refusal-ok", runtimeV2Task9SelectRefusalSource},
		{"select", "teardown-buffered", "select-teardown-ok", runtimeV2Task9SelectTeardownSource},
		{"non-copy-channel", "happy", "far-channel-noncopy-roundtrip-ok", runtimeV2FarChannelNonCopyRoundTripSource},
		{"non-copy-channel", "cancel", "channel-cancel-ok", runtimeV2Task9ChannelCancelSource},
		{"non-copy-channel", "refusal", "channel-refusal-ok", runtimeV2Task9ChannelRefusalSource},
		{"non-copy-channel", "teardown-buffered", "channel-teardown-ok", runtimeV2Task9ChannelTeardownSource},
	}
}

func TestRuntimeV2Task9Matrix(t *testing.T) {
	root := repoRoot(t)
	baseEnv := envWithStdlib(root)
	for _, cell := range runtimeV2Task9Cells() {
		t.Run(cell.vertical+"/"+cell.edge, func(t *testing.T) {
			outputPath := buildRuntimeV2CrossingSource(t, cell.source, nil)
			for _, shards := range []string{"1", "2", "8"} {
				t.Run("shards-"+shards, func(t *testing.T) {
					env := overrideEnvVar(baseEnv, "SURGE_SHARDS", shards)
					env = overrideEnvVar(env, "SURGE_THREADS", shards)
					duration, result := runBinaryWithTimeout(t, outputPath, env, 60*time.Second)
					if result.exitCode != 0 || strings.Contains(result.stdout, "FAIL") ||
						!strings.Contains(result.stdout, cell.marker) {
						t.Fatalf("%s/%s at %s shards: exit=%d duration=%s\nstdout:\n%s\nstderr:\n%s",
							cell.vertical, cell.edge, shards, result.exitCode, duration, result.stdout, result.stderr)
					}
					stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 240*time.Second)
					if hasValgrindMemcheckError(stderr) {
						t.Fatalf("%s/%s at %s shards: memcheck error\nstdout:\n%s\nstderr:\n%s",
							cell.vertical, cell.edge, shards, stdout, stderr)
					}
					if exitCode != 0 || !strings.Contains(stdout, cell.marker) {
						t.Fatalf("%s/%s at %s shards under valgrind: exit=%d\nstdout:\n%s\nstderr:\n%s",
							cell.vertical, cell.edge, shards, exitCode, stdout, stderr)
					}
					// Strict zero is "definitely lost: 0 bytes in 0 blocks" with no
					// Memcheck error report, the reading every valgrind-zero row of
					// this suite takes: the executor's threads leave their TLS and
					// arenas "possibly lost" / still reachable at process exit
					// (68 KB in 15 blocks on the LLVM lane), and that residue is
					// the process's, not the crossing's.
					bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
					if err != nil {
						t.Fatalf("%s/%s at %s shards: %v\nstderr:\n%s", cell.vertical, cell.edge, shards, err, stderr)
					}
					if bytesLost != 0 || blocksLost != 0 {
						t.Fatalf("%s/%s at %s shards: definitely lost %d bytes in %d blocks, want strict zero\nstderr:\n%s",
							cell.vertical, cell.edge, shards, bytesLost, blocksLost, stderr)
					}
				})
			}
		})
	}
}
