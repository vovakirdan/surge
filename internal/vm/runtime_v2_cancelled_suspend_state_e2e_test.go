package vm_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// A child cancelled before its own first suspend point ever resumes never
// gets to unpack the state box that suspend point built: rt_async_yield's
// cancellation check fires on the child's very first poll, at its very
// first internal await, and the completion that follows never reads the
// box back out (apply_poll_outcome's direct-to-mark_done path for a task
// with no scope children in flight). This is deliberately local-only (no
// far/distributed task, no crossing) and deterministic at SURGE_SHARDS=1:
// spawn just marks the child READY without running it, so nothing can poll
// the child between spawn and cancel on a single worker.
//
// KNOWN RESIDUAL (reported here, not fixed — separate from what this row
// proves): the abandoned state's own box now frees correctly (measured via
// git-stash A/B: 32B direct + 24B indirect before this fix, 24B direct + 0
// indirect after — the 32B box is gone). The remaining 24B is the awaited
// checkpoint's own Task<T> local, packed live into the abandoned payload:
// typeOwnsHeapRec's default case (emit_drop_glue.go) explicitly excludes
// handle-shaped types (Task<T>, and empirically Channel<T> too) from the
// generic recursive composite walk, because they need their own dedicated
// release call, not a raw rt_free — the same gap that presumably affects
// any struct/tuple/union field of one of these handle types going through
// requireDropGlue anywhere else in the compiler, not just here.
const runtimeV2CancelledSuspendStateSource = `
async fn child_body() -> int {
    checkpoint().await();
    return 42;
}

async fn run() -> int {
    let child: Task<int> = spawn child_body();
    child.cancel();
    let r: TaskResult<int> = child.await();
    let outcome: int = compare r { Success(_) => 0; Cancelled() => 1; };
    if outcome != 1 {
        print("FAIL expected the child to resolve Cancelled");
        return 1;
    }
    print("cancelled-suspend-state-ok");
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

// TestRuntimeV2CancelledSuspendStateReclaimed proves the abandoned
// suspend-point state fix: a child cancelled before its own checkpoint ever
// runs must reclaim the state box itself, at SHARDS=1 where the race is
// deterministic (spawn/cancel/await all run on one worker with nothing else
// able to poll the child in between). The assertion is bounded, not zero —
// see the KNOWN RESIDUAL note above for the separate, pre-existing gap this
// row does not claim to fix.
func TestRuntimeV2CancelledSuspendStateReclaimed(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CancelledSuspendStateSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnvVar(baseEnv, "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")

	t.Run("cancelled_outcome", func(t *testing.T) {
		duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
		if result.exitCode != 0 {
			t.Fatalf(
				"expected a clean exit (exit=0), got exit=%d (duration=%s)\nstdout:\n%s\nstderr:\n%s",
				result.exitCode, duration, result.stdout, result.stderr,
			)
		}
		if strings.Contains(result.stdout, "FAIL") {
			t.Fatalf("printed a FAIL marker; stdout=%q", result.stdout)
		}
		if !strings.Contains(result.stdout, "cancelled-suspend-state-ok") {
			t.Fatalf("missing completion marker; stdout=%q", result.stdout)
		}
	})

	t.Run("valgrind_bounded", func(t *testing.T) {
		vg, err := exec.LookPath("valgrind")
		if err != nil {
			t.Skip("valgrind not found on PATH; skipping leak census")
		}
		root := repoRoot(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		args := []string{
			"--leak-check=full",
			"--show-leak-kinds=definite,indirect",
			"--errors-for-leak-kinds=definite,indirect",
			"--error-exitcode=97",
			outputPath,
		}
		cmd := exec.CommandContext(ctx, vg, args...)
		cmd.Dir = root
		cmd.Env = env
		stdout, stderr, exitCode := runCommand(t, cmd, "")
		if ctx.Err() == context.DeadlineExceeded {
			t.Fatalf("valgrind run timed out\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
		}
		if !strings.Contains(stdout, "cancelled-suspend-state-ok") {
			t.Fatalf("missing completion marker under valgrind; stdout=%q\nstderr:\n%s", stdout, stderr)
		}
		if m := valgrindCorruptionRE.FindString(stderr); m != "" {
			t.Fatalf("valgrind reports a crash-class error (%q)\nstderr:\n%s", m, stderr)
		}
		definiteBytes, definiteBlocks := parseValgrindLeakMatch(valgrindDefiniteLeakRE, stderr)
		indirectBytes, indirectBlocks := parseValgrindLeakMatch(valgrindIndirectLeakRE, stderr)
		t.Logf(
			"valgrind census: definitely_lost=%dB/%dblk indirectly_lost=%dB/%dblk exit=%d",
			definiteBytes, definiteBlocks, indirectBytes, indirectBlocks, exitCode,
		)
		// The state box itself (32B) must be gone — that's what this row
		// proves. The known residual is exactly one Task<T> handle (24B
		// direct, 0 indirect); anything indirect, or any direct count
		// that isn't a multiple of that unit, means either the state box
		// fix regressed or a NEW leak shape appeared.
		const perOccurrenceHandleBytes = 24
		if indirectBytes != 0 || indirectBlocks != 0 {
			t.Fatalf(
				"unexpected indirect leak (definitely_lost=%dB/%dblk indirectly_lost=%dB/%dblk); the state box fix looks incomplete\nstdout:\n%s\nstderr:\n%s",
				definiteBytes, definiteBlocks, indirectBytes, indirectBlocks, stdout, stderr,
			)
		}
		if definiteBytes%perOccurrenceHandleBytes != 0 {
			t.Fatalf(
				"definitely-lost bytes (%d) is not a multiple of the known per-occurrence handle unit (%d); this looks like a NEW leak shape, not the documented residual\nstdout:\n%s\nstderr:\n%s",
				definiteBytes, perOccurrenceHandleBytes, stdout, stderr,
			)
		}
	})
}
