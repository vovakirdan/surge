//go:build runtime_v2_pending

// The tag is here for exactly one reason: this row is RED, and a red row in
// the untagged set makes `make check` -- and therefore the pre-commit hook --
// refuse every commit in the tree until the defect is fixed. The tag is how
// this repository keeps a known-red gate committable, and it comes off in the
// same commit that makes the row pass.

package vm_test

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A task cancelled before the executor ever polls it must give back everything
// its own first poll allocated. It does not: the scope block that poll opened
// is never freed, once per cancelled task, forever.
//
// THE SHAPE. Every async body opens a scope at the top of its own poll --
// rt_scope_enter runs in the start variant of the generated state machine
// (internal/mir/async_codegen.go), whether or not the body ever spawns
// anything -- and rt_scope_enter allocates one rt_scope
// (runtime/native/rt_async_scope.c). The block is handed back by
// scope_exit_locked, and the only completion path that calls it for a
// cancelled task is apply_poll_outcome's cancelled arm
// (runtime/native/rt_task_complete.c). The single-worker control runner has
// its own copy of that switch (run_ready_one, runtime/native/rt_async_poll.c)
// and its cancelled arm goes straight to mark_done with no scope teardown at
// all, so the scope the poll opened outlives the task that owned it.
//
// WHY VALGRIND'S LEAK SUMMARY IS SILENT ABOUT IT, and why this row does not
// read that summary. Scope ids are monotonic and the executor's segmented
// scope table never drops a segment, so the abandoned block is still POINTED
// AT from the table at exit: valgrind classifies it "still reachable" and
// reports `definitely lost: 0 bytes in 0 blocks`. A strict-zero row on the
// leak summary is green against this defect. What is NOT green is the block
// count still live at exit, measured at two different round counts: a
// reclaimed scope makes that count flat, and an abandoned one makes it grow
// one block per round.
//
// MEASURED on this tree, SURGE_SHARDS=1 SURGE_THREADS=1, `in use at exit`:
//
//	cancelled     10 rounds ->  24 blocks     200 rounds -> 214 blocks   (+190)
//	not cancelled 10 rounds ->  13 blocks     200 rounds ->  13 blocks   (+0)
//
// 190 blocks over 190 extra rounds: exactly one per cancelled task, 12,160
// bytes, 64 bytes each -- sizeof(rt_scope). Valgrind names the site directly:
// "12,800 bytes in 200 blocks are still reachable ... by rt_scope_enter
// (rt_async_scope.c:114) ... by run_ready_one (rt_async_poll.c)".
//
// AT FOUR WORKERS THE SAME PROGRAM IS FLAT (26 blocks at both round counts),
// because the worker turn applies its outcome through apply_poll_outcome,
// which does exit the scope. That is the whole difference between the two
// arms, and it is why this row pins one worker rather than sweeping widths:
// the wide configuration cannot see the defect.
//
// The uncancelled twin is not decoration. Without it this row could pass on a
// tree that never ran the loop at all, or that failed to cancel anything: a
// program whose rounds all resolve Success allocates and frees the same scope
// per round and is flat for a reason that has nothing to do with the fix. The
// twin must stay flat, and the cancelled half must become flat, for this to be
// closed.
//
// The `cancelled != rounds` guard inside the program is the second
// non-vacuity: exit 91 says the rounds ran but did not cancel, which is a
// different program from the one this row measures.
func runtimeV2CancelledScopeSource(rounds int, cancel bool) string {
	cancelLine := ""
	perRound := "0"
	if cancel {
		cancelLine = "        t.cancel();\n"
		perRound = "1"
	}
	// The captured `payload` is what makes this the owning shape: the child's
	// frame holds a string, so a completion that walked the wrong thing would
	// show up here as a double free rather than as silence.
	return fmt.Sprintf(`
async fn child(payload: string) -> int {
    checkpoint().await();
    if payload == "" { return 0; }
    return 42;
}

async fn run() -> int {
    let mut i: int = 0;
    let mut cancelled: int = 0;
    while i < %d {
        let t: Task<int> = spawn child("owned-frame-capture-payload");
%s        let v: int = compare t.await() { Success(_) => 0; Cancelled() => 1; };
        cancelled = cancelled + v;
        i = i + 1;
    }
    if cancelled != %d * %s { return 91; }
    print("cancelled-scope-census-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() { Success(c) => c; Cancelled() => 90; };
}
`, rounds, cancelLine, rounds, perRound)
}

// valgrindInUseAtExitRE reads the heap summary line rather than the leak
// summary, because the block this row is about is reachable at exit and the
// leak summary therefore says nothing about it.
var valgrindInUseAtExitRE = regexp.MustCompile(`in use at exit: ([\d,]+) bytes in ([\d,]+) blocks`)

func parseValgrindInUseAtExit(t *testing.T, stderr string) (bytes, blocks int) {
	t.Helper()
	match := valgrindInUseAtExitRE.FindStringSubmatch(stderr)
	if match == nil {
		t.Fatalf("valgrind printed no heap summary to read\nstderr:\n%s", stderr)
	}
	bytes, err := strconv.Atoi(strings.ReplaceAll(match[1], ",", ""))
	if err != nil {
		t.Fatalf("unreadable heap-summary byte count %q: %v", match[1], err)
	}
	blocks, err = strconv.Atoi(strings.ReplaceAll(match[2], ",", ""))
	if err != nil {
		t.Fatalf("unreadable heap-summary block count %q: %v", match[2], err)
	}
	return bytes, blocks
}

func runtimeV2CancelledScopeCensus(t *testing.T, rounds int, cancel bool) (bytes, blocks int) {
	t.Helper()
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CancelledScopeSource(rounds, cancel), nil)
	env := envWithStdlib(repoRoot(t))
	// One worker on one shard: the control runner is the applier that drops the
	// scope, and it is the applier only when no worker thread takes the turn.
	env = overrideEnvVar(env, "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 300*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a real memcheck error at rounds=%d cancel=%v\nstdout:\n%s\nstderr:\n%s",
			rounds, cancel, stdout, stderr)
	}
	if exitCode != 0 {
		reason := "the program did not run to its verdict"
		switch exitCode {
		case 90:
			reason = "the driver task was itself cancelled"
		case 91:
			reason = "the rounds ran but did not produce the cancellations this row measures"
		}
		t.Fatalf("census program failed at rounds=%d cancel=%v (exit=%d -- %s)\nstdout:\n%s\nstderr:\n%s",
			rounds, cancel, exitCode, reason, stdout, stderr)
	}
	if !strings.Contains(stdout, "cancelled-scope-census-ok") {
		t.Fatalf("census program missing its completion marker at rounds=%d cancel=%v; stdout=%q",
			rounds, cancel, stdout)
	}
	return parseValgrindInUseAtExit(t, stderr)
}

// TestRuntimeV2CancelledTaskReclaimsItsScope is RED. It reports 190 blocks of
// growth over 190 extra cancelled rounds; a tree that reclaims the scope
// reports 0.
func TestRuntimeV2CancelledTaskReclaimsItsScope(t *testing.T) {
	const (
		fewRounds  = 10
		manyRounds = 200
	)

	// The acceptance twin first: same program, same worker count, nothing
	// cancelled. It must already be flat, or the measurement below is not
	// measuring cancellation.
	keptFewBytes, keptFewBlocks := runtimeV2CancelledScopeCensus(t, fewRounds, false)
	keptManyBytes, keptManyBlocks := runtimeV2CancelledScopeCensus(t, manyRounds, false)
	t.Logf("uncancelled: rounds=%d in_use_at_exit=%dB/%dblk; rounds=%d in_use_at_exit=%dB/%dblk",
		fewRounds, keptFewBytes, keptFewBlocks, manyRounds, keptManyBytes, keptManyBlocks)
	if keptManyBlocks != keptFewBlocks {
		t.Fatalf(
			"an UNCANCELLED round already retains memory: %d blocks at %d rounds vs %d blocks at %d rounds (%+d over %d extra rounds); the cancelled measurement below cannot be attributed to cancellation until this is flat",
			keptManyBlocks, manyRounds, keptFewBlocks, fewRounds,
			keptManyBlocks-keptFewBlocks, manyRounds-fewRounds,
		)
	}

	cancelledFewBytes, cancelledFewBlocks := runtimeV2CancelledScopeCensus(t, fewRounds, true)
	cancelledManyBytes, cancelledManyBlocks := runtimeV2CancelledScopeCensus(t, manyRounds, true)
	t.Logf("cancelled: rounds=%d in_use_at_exit=%dB/%dblk; rounds=%d in_use_at_exit=%dB/%dblk",
		fewRounds, cancelledFewBytes, cancelledFewBlocks, manyRounds, cancelledManyBytes, cancelledManyBlocks)

	extraRounds := manyRounds - fewRounds
	grownBlocks := cancelledManyBlocks - cancelledFewBlocks
	grownBytes := cancelledManyBytes - cancelledFewBytes
	if grownBlocks != 0 {
		t.Fatalf(
			"a task cancelled before its first poll never gives back the scope that poll opened: %d blocks / %d bytes still live at exit over %d extra cancelled rounds (%.2f blocks and %.1f bytes per cancelled task), while the same program with nothing cancelled is flat at %d blocks. The block is rt_scope: rt_scope_enter allocates it, and the single-worker control runner's cancelled arm completes the task without the scope teardown its multi-worker twin performs",
			grownBlocks, grownBytes, extraRounds,
			float64(grownBlocks)/float64(extraRounds), float64(grownBytes)/float64(extraRounds),
			keptFewBlocks,
		)
	}
}
