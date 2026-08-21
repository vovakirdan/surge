package vm_test

import (
	"strings"
	"testing"
	"time"
)

// Both local channels are unbuffered and have no peer, so neither select arm
// can win. Cancellation therefore exercises the abandoned Pc state: it must
// contain one owner for the heap string SEND payload, never the original plus
// unary/select aliases. The entered/completion markers prove the child reached
// the select and resolved Cancelled rather than exiting through a winner arm.
const runtimeV2LocalSelectCancelNonCopySource = `
fn build(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

async fn parked() -> int {
    let send_ch = Channel::<string>::new(0:uint);
    let stop_ch = Channel::<int>::new(0:uint);
    let job: string = build("local-cancel-");
    print("local-select-cancel-entered");
    let winner: int = select {
        send_ch.send(own job) => 1;
        stop_ch.recv() => 2;
    };
    return winner;
}

async fn run() -> int {
    let child: Task<int> = spawn parked();
    checkpoint().await();
    checkpoint().await();
    child.cancel();
    let result: TaskResult<int> = child.await();
    let cancelled: bool = compare result {
        Success(_) => false;
        Cancelled() => true;
    };
    if !cancelled {
        print("FAIL expected parked local select to cancel");
        return 11;
    }
    print("local-select-cancel-noncopy-ok");
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

// Copy-payload control for the same cancellation schedule. It isolates any
// pre-existing abandoned Channel-handle residual from the heap SEND payload
// this test owns: the non-Copy program may add zero lost bytes or blocks.
const runtimeV2LocalSelectCancelCopyControlSource = `
async fn parked() -> int {
    let send_ch = Channel::<int>::new(0:uint);
    let stop_ch = Channel::<int>::new(0:uint);
    print("local-select-cancel-control-entered");
    let winner: int = select {
        send_ch.send(7) => 1;
        stop_ch.recv() => 2;
    };
    return winner;
}

async fn run() -> int {
    let child: Task<int> = spawn parked();
    checkpoint().await();
    checkpoint().await();
    child.cancel();
    let result: TaskResult<int> = child.await();
    let cancelled: bool = compare result {
        Success(_) => false;
        Cancelled() => true;
    };
    if !cancelled {
        print("FAIL expected parked local select control to cancel");
        return 11;
    }
    print("local-select-cancel-control-ok");
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

func TestRuntimeV2LocalSelectCancelNonCopySendArm(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2LocalSelectCancelNonCopySource, nil)
	controlPath := buildRuntimeV2CrossingSource(t, runtimeV2LocalSelectCancelCopyControlSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnvVar(baseEnv, "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")

	t.Run("cancelled_outcome", func(t *testing.T) {
		duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
		if result.exitCode != 0 {
			t.Fatalf(
				"cancelled non-copy local select failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
				result.exitCode, duration, result.stdout, result.stderr,
			)
		}
		assertLocalSelectCancelMarkers(t, result.stdout)
	})

	t.Run("valgrind", func(t *testing.T) {
		controlStdout, controlStderr, controlExitCode := runBinaryUnderValgrind(t, controlPath, env, 90*time.Second)
		if hasValgrindMemcheckError(controlStderr) {
			t.Fatalf("copy-payload control reported a valgrind memcheck error\nstdout:\n%s\nstderr:\n%s", controlStdout, controlStderr)
		}
		if controlExitCode != 0 {
			t.Fatalf("copy-payload local cancellation control failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s", controlExitCode, controlStdout, controlStderr)
		}
		assertLocalSelectControlMarkers(t, controlStdout)
		controlBytesLost, controlBlocksLost, err := parseValgrindDefinitelyLost(controlStderr)
		if err != nil {
			t.Fatalf("parse copy-control valgrind leak summary: %v\nstderr:\n%s", err, controlStderr)
		}
		// RV2-DEBT-062: abandoned composite drop glue does not dispatch the
		// dedicated release for nested local Channel<T> handles. The two
		// never-ready channels therefore leave this exact pre-existing baseline;
		// it is held visible here instead of being fixed in the ownership epic.
		const controlBaselineBytes = 96
		const controlBaselineBlocks = 2
		if controlBytesLost != controlBaselineBytes || controlBlocksLost != controlBaselineBlocks {
			t.Fatalf(
				"copy-control RV2-DEBT-062 baseline changed: got %dB/%d blocks, want %dB/%d blocks\nstderr:\n%s",
				controlBytesLost, controlBlocksLost, controlBaselineBytes, controlBaselineBlocks, controlStderr,
			)
		}

		stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 90*time.Second)
		if hasValgrindMemcheckError(stderr) {
			t.Fatalf("valgrind reported a double drop or use-after-free while cancelling the parked local SEND payload\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
		}
		if exitCode != 0 {
			t.Fatalf("cancelled non-copy local select failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
		}
		assertLocalSelectCancelMarkers(t, stdout)
		bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
		if err != nil {
			t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
		}
		if bytesLost != controlBytesLost || blocksLost != controlBlocksLost {
			t.Fatalf(
				"cancelled non-copy local select leaked %dB/%d blocks versus copy control %dB/%d blocks; want strict zero incremental payload loss\ncontrol stderr:\n%s\nnon-copy stderr:\n%s",
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
				"cancelled non-copy local select changed indirect loss: got %dB/%d blocks, copy control %dB/%d blocks",
				indirectBytes, indirectBlocks, controlIndirectBytes, controlIndirectBlocks,
			)
		}
		t.Logf("local select cancellation valgrind A/B: copy=%dB/%dblk+%dB/%dblk indirect noncopy=%dB/%dblk+%dB/%dblk indirect",
			controlBytesLost, controlBlocksLost, controlIndirectBytes, controlIndirectBlocks,
			bytesLost, blocksLost, indirectBytes, indirectBlocks)
	})
}

func assertLocalSelectCancelMarkers(t *testing.T, stdout string) {
	t.Helper()
	if strings.Contains(stdout, "FAIL") {
		t.Fatalf("printed a FAIL marker; stdout=%q", stdout)
	}
	if !strings.Contains(stdout, "local-select-cancel-entered") {
		t.Fatalf("child never reached the local select; stdout=%q", stdout)
	}
	if !strings.Contains(stdout, "local-select-cancel-noncopy-ok") {
		t.Fatalf("missing cancellation completion marker; stdout=%q", stdout)
	}
}

func assertLocalSelectControlMarkers(t *testing.T, stdout string) {
	t.Helper()
	if strings.Contains(stdout, "FAIL") {
		t.Fatalf("copy control printed a FAIL marker; stdout=%q", stdout)
	}
	if !strings.Contains(stdout, "local-select-cancel-control-entered") ||
		!strings.Contains(stdout, "local-select-cancel-control-ok") {
		t.Fatalf("copy control missed cancellation markers; stdout=%q", stdout)
	}
}
