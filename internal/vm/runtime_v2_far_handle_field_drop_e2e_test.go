package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A far channel handle held as a FIELD is released when its holder is dropped.
//
// A far local's scope-exit drop has always reached rt_far_channel_handle_drop
// (emitInstrDrop). A far handle inside a composite went through the drop
// glue instead, and the glue's structural walk did not count the lease as
// owned: the composite got a body that reclaimed nothing, and the holder's
// lease on the owner shard's registry entry -- plus the caller-side token --
// outlived the holder (RV2-DEBT-198, the far half). The walk counts it now
// and the glue reaches the same release the local does.
//
// Two holders: one dropped at the end of the frame that built it, one moved
// into a callee that drops it at its own scope exit -- the two places a
// composite's glue runs. The mutant is the walk's far arm removed
// (`typeOwnsHeap` answering NO for a far channel): the program still exits
// 0, and valgrind reports the two tokens and their registry entries as
// definitely lost.
const runtimeV2FarHandleFieldDropSource = `
type Holder = { ch: far Channel<int> };

fn take(h: Holder) -> int {
    return 1;
}

async fn run() -> int {
    let a: far Channel<int> = channel_on::<int>(shard(0:ShardId), 2);
    let kept: Holder = Holder { ch: a };
    let b: far Channel<int> = channel_on::<int>(shard(0:ShardId), 2);
    let given: Holder = Holder { ch: b };
    let n: int = take(given);
    if n == 1 {
        print("far-handle-field-drop-ok");
        return 0;
    }
    return 1;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

func TestRuntimeV2FarHandleFieldDropReleasesTheLease(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2FarHandleFieldDropSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnvVar(baseEnv, "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")

	t.Run("correctness", func(t *testing.T) {
		duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
		if result.exitCode != 0 || !strings.Contains(result.stdout, "far-handle-field-drop-ok") {
			t.Fatalf(
				"far handle field drop program failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
				result.exitCode, duration, result.stdout, result.stderr,
			)
		}
	})

	t.Run("valgrind_strict_zero", func(t *testing.T) {
		stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 90*time.Second)
		if hasValgrindMemcheckError(stderr) {
			t.Fatalf("valgrind reported a memcheck error\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
		}
		if exitCode != 0 || !strings.Contains(stdout, "far-handle-field-drop-ok") {
			t.Fatalf("far handle field drop program failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
		}
		bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
		if err != nil {
			t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
		}
		if bytesLost != 0 || blocksLost != 0 {
			t.Fatalf("definitely lost: %d bytes in %d blocks, want 0 in 0 (a held far lease outlived its holder)\nstderr:\n%s", bytesLost, blocksLost, stderr)
		}
	})
}
