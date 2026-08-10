package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A cancelled remote select must have exactly one owner for an in-flight
// non-Copy SEND payload. Once the crossing parks, the runtime pending owns the
// payload; the abandoned async state must not retain a second owning copy.
// Neither arm can become ready, so cancellation cannot take the normal winner
// handback. The source-level marker proves only that the crossing was entered,
// not the exact transport phase at the instant of cancellation; the complete
// non-vacuity proof is intentionally composite with
// TestFarSelectOwnedBindingUsesExplicitReturnPlace (the pending Pc excludes
// the source local) and TestRuntimeV2RemoteSelectAbandonEdges/
// cancel-before-dispatch (a deterministic pending releases the payload once).
const runtimeV2FarSelectCancelNonCopySource = `
fn build(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

async fn parked(a: far Channel<string>, b: far Channel<int>) -> int {
    let job: string = build("cancel-");
    print("far-select-cancel-entered");
    let winner: int = select { a.send(own job) => 1; b.recv() => 2; };
    return winner;
}

async fn run() -> int {
    let a: far Channel<string> = channel_on::<string>(shard(0:ShardId), 0);
    let b: far Channel<int> = channel_on::<int>(shard(0:ShardId), 0);
    let child: Task<int> = spawn parked(a, b);

    // At one shard the first yield lets parked() run through the select and
    // suspend; the second lets its owner-side request settle before cancel.
    checkpoint().await();
    checkpoint().await();
    child.cancel();
    let result: TaskResult<int> = child.await();
    let cancelled: bool = compare result {
        Success(_) => false;
        Cancelled() => true;
    };
    if !cancelled {
        print("FAIL expected parked far select to cancel");
        return 11;
    }
    print("far-select-cancel-noncopy-ok");
    return 0;
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

// Copy-payload A/B control for the cancellation row above. It deliberately
// keeps the same two far-channel handles, task boundary, never-ready select,
// checkpoint schedule, and cancel/await path. The only ownership-bearing
// difference is the non-Copy heap string shipped by the subject program.
const runtimeV2FarSelectCancelCopyControlSource = `
async fn parked(a: far Channel<int>, b: far Channel<int>) -> int {
    print("far-select-cancel-control-entered");
    let winner: int = select { a.send(7) => 1; b.recv() => 2; };
    return winner;
}

async fn run() -> int {
    let a: far Channel<int> = channel_on::<int>(shard(0:ShardId), 0);
    let b: far Channel<int> = channel_on::<int>(shard(0:ShardId), 0);
    let child: Task<int> = spawn parked(a, b);
    checkpoint().await();
    checkpoint().await();
    child.cancel();
    let result: TaskResult<int> = child.await();
    let cancelled: bool = compare result {
        Success(_) => false;
        Cancelled() => true;
    };
    if !cancelled {
        print("FAIL expected parked far select control to cancel");
        return 11;
    }
    print("far-select-cancel-control-ok");
    return 0;
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

func TestRuntimeV2FarSelectCancelNonCopySendArm(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2FarSelectCancelNonCopySource, nil)
	controlPath := buildRuntimeV2CrossingSource(t, runtimeV2FarSelectCancelCopyControlSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnvVar(baseEnv, "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")

	t.Run("cancelled_outcome", func(t *testing.T) {
		duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
		if result.exitCode != 0 {
			t.Fatalf(
				"cancelled non-copy far select failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
				result.exitCode, duration, result.stdout, result.stderr,
			)
		}
		if strings.Contains(result.stdout, "FAIL") {
			t.Fatalf("printed a FAIL marker; stdout=%q", result.stdout)
		}
		if !strings.Contains(result.stdout, "far-select-cancel-entered") {
			t.Fatalf("child never reached the far select; stdout=%q", result.stdout)
		}
		if !strings.Contains(result.stdout, "far-select-cancel-noncopy-ok") {
			t.Fatalf("missing cancellation completion marker; stdout=%q", result.stdout)
		}
	})

	t.Run("valgrind", func(t *testing.T) {
		controlStdout, controlStderr, controlExitCode := runBinaryUnderValgrind(t, controlPath, env, 90*time.Second)
		if hasValgrindMemcheckError(controlStderr) {
			t.Fatalf("copy-payload control reported a valgrind memcheck error\nstdout:\n%s\nstderr:\n%s", controlStdout, controlStderr)
		}
		if controlExitCode != 0 {
			t.Fatalf("copy-payload cancellation control failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s", controlExitCode, controlStdout, controlStderr)
		}
		if !strings.Contains(controlStdout, "far-select-cancel-control-entered") ||
			!strings.Contains(controlStdout, "far-select-cancel-control-ok") {
			t.Fatalf("missing copy-control cancellation witnesses under valgrind; stdout=%q", controlStdout)
		}

		stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 90*time.Second)
		if hasValgrindMemcheckError(stderr) {
			t.Fatalf("valgrind reported a double drop or use-after-free while cancelling the parked SEND payload\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
		}
		if exitCode != 0 {
			t.Fatalf("cancelled non-copy far select failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
		}
		if !strings.Contains(stdout, "far-select-cancel-entered") || !strings.Contains(stdout, "far-select-cancel-noncopy-ok") {
			t.Fatalf("missing cancellation witnesses under valgrind; stdout=%q", stdout)
		}
		controlBytesLost, controlBlocksLost, err := parseValgrindDefinitelyLost(controlStderr)
		if err != nil {
			t.Fatalf("parse copy-control valgrind leak summary: %v\nstderr:\n%s", err, controlStderr)
		}
		const controlBaselineBytes = 48
		const controlBaselineBlocks = 2
		if controlBytesLost != controlBaselineBytes || controlBlocksLost != controlBaselineBlocks {
			t.Fatalf(
				"copy-control RV2-DEBT-062 baseline changed: got %dB/%d blocks, want %dB/%d blocks\nstderr:\n%s",
				controlBytesLost, controlBlocksLost, controlBaselineBytes, controlBaselineBlocks, controlStderr,
			)
		}
		bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
		if err != nil {
			t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
		}
		// RV2-DEBT-062 leaves the same cancelled-async baseline in both
		// binaries: measured here as 48B/2 far-channel handle boxes, each
		// loss stack rooted at rt_far_channel_handle_alloc, with zero
		// indirect loss. The abandoned outer frame itself is reclaimed, so
		// this is specifically 062's nested handle-drop gap, not a new 059
		// outer-frame leak. Do not hide or fix that separate debt in this
		// epic. Exact A/B equality is the strict-zero DELTA gate: the
		// non-Copy SEND payload may add no definitely-lost bytes or blocks
		// over the Copy control.
		if bytesLost != controlBytesLost || blocksLost != controlBlocksLost {
			t.Fatalf(
				"cancelled non-copy far select leaked %dB/%d blocks versus copy control %dB/%d blocks; want strict zero incremental payload loss\ncontrol stderr:\n%s\nnon-copy stderr:\n%s",
				bytesLost, blocksLost, controlBytesLost, controlBlocksLost, controlStderr, stderr,
			)
		}
		controlIndirectBytes, controlIndirectBlocks := parseValgrindLeakMatch(valgrindIndirectLeakRE, controlStderr)
		indirectBytes, indirectBlocks := parseValgrindLeakMatch(valgrindIndirectLeakRE, stderr)
		if controlIndirectBytes != 0 || controlIndirectBlocks != 0 {
			t.Fatalf(
				"copy-control RV2-DEBT-062 baseline gained indirect loss: got %dB/%d blocks, want strict zero",
				controlIndirectBytes, controlIndirectBlocks,
			)
		}
		if indirectBytes != controlIndirectBytes || indirectBlocks != controlIndirectBlocks {
			t.Fatalf(
				"cancelled non-copy far select changed indirect loss: got %dB/%d blocks, copy control %dB/%d blocks",
				indirectBytes, indirectBlocks, controlIndirectBytes, controlIndirectBlocks,
			)
		}
		t.Logf("valgrind A/B: copy=%dB/%dblk noncopy=%dB/%dblk indirect_delta=%dB/%dblk",
			controlBytesLost, controlBlocksLost, bytesLost, blocksLost,
			indirectBytes-controlIndirectBytes, indirectBlocks-controlIndirectBlocks)
	})
}
