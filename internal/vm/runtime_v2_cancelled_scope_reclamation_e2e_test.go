package vm_test

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A task cancelled at its first suspension point must give back everything its
// own poll allocated. It did not: the scope block that poll opened was never
// freed, once per cancelled task, forever.
//
// THE SHAPE. Every async body opens a scope at the top of its own poll --
// rt_scope_enter runs in the start variant of the generated state machine
// (internal/mir/async_codegen.go), whether or not the body ever spawns
// anything -- and rt_scope_enter allocates one rt_scope
// (runtime/native/rt_async_scope.c). The block is handed back by
// scope_exit_locked, which the cancelled arm of apply_poll_outcome calls
// (runtime/native/rt_task_complete.c). The single-worker control runner used
// to carry its own copy of that switch (run_ready_one, runtime/native/
// rt_async_poll.c), and the copy had drifted: its cancelled arm went straight
// to mark_done with no scope teardown at all, so the scope the poll opened
// outlived the task that owned it. The copy is gone -- that runner now applies
// its outcome through the same apply_poll_outcome as every other poll site --
// and this row is what notices if a second copy is ever written.
//
// The task IS polled here, whatever "cancelled before it ran" would suggest:
// the cancel is requested before the await, and the body takes it at its first
// suspension point, after rt_scope_enter has already allocated. That is what
// the valgrind stack below shows.
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
// MEASURED, SURGE_SHARDS=1 SURGE_THREADS=1, `in use at exit`, before the fix
// and after it:
//
//	                          10 rounds     200 rounds
//	cancelled, unfixed        24 blocks     214 blocks   (+190)
//	cancelled, fixed          14 blocks      14 blocks   (+0)
//	not cancelled, either     13 blocks      13 blocks   (+0)
//
// 190 blocks over 190 extra rounds: exactly one per cancelled task, 12,160
// bytes, 64 bytes each -- sizeof(rt_scope). Valgrind named the site directly:
// "12,800 bytes in 200 blocks are still reachable ... by rt_scope_enter
// (rt_async_scope.c:114) ... by run_ready_one (rt_async_poll.c)".
//
// WHY ONE WORKER, and what the wide configuration does instead. The arm this
// row is about only runs when no worker thread takes the turn: with more than
// one worker rt_task_await waits on done_cv and never enters this runner at
// all (rt_worker_count(), runtime/native/rt_async_task.c), so a width sweep
// cannot reach the code under test. The wide configuration is also NOT a flat
// control to compare against -- it has a second, unrelated intermittent
// retention on this very program, 200 blocks of 368 bytes allocated by
// spawn_internal_task_locked for the checkpoint task. Measured at
// SURGE_SHARDS=4 SURGE_THREADS=4 over 200 cancelled rounds, 25 interleaved
// samples of each build: 15/25 runs at 26 blocks and 10/25 between 63 and 226
// with the scope fix, 13/25 at 26 blocks and 12/25 between 180 and 228
// without it -- the same distribution on both sides, because this fix cannot
// execute there. That retention is a separate open question and is NOT what
// this row measures. One block of it is visible even here: the fixed cancelled
// figure sits one block and 368 bytes above the uncancelled twin (68,248 vs
// 67,880) at BOTH round counts. Constant, not per-round, so this row's two-count
// difference is blind to it by construction -- which is exactly why the row
// subtracts two counts instead of asserting an absolute total.
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

// TestRuntimeV2CancelledTaskReclaimsItsScope reports 0 blocks and 0 bytes of
// growth over 190 extra cancelled rounds. Restore the control runner's own
// copy of the outcome switch and it reports 190 blocks / 12,160 bytes.
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
	if keptManyBlocks != keptFewBlocks || keptManyBytes != keptFewBytes {
		t.Fatalf(
			"an UNCANCELLED round already retains memory: %d blocks / %d bytes at %d rounds vs %d blocks / %d bytes at %d rounds (%+d blocks, %+d bytes over %d extra rounds); the cancelled measurement below cannot be attributed to cancellation until this is flat",
			keptManyBlocks, keptManyBytes, manyRounds, keptFewBlocks, keptFewBytes, fewRounds,
			keptManyBlocks-keptFewBlocks, keptManyBytes-keptFewBytes, manyRounds-fewRounds,
		)
	}

	cancelledFewBytes, cancelledFewBlocks := runtimeV2CancelledScopeCensus(t, fewRounds, true)
	cancelledManyBytes, cancelledManyBlocks := runtimeV2CancelledScopeCensus(t, manyRounds, true)
	t.Logf("cancelled: rounds=%d in_use_at_exit=%dB/%dblk; rounds=%d in_use_at_exit=%dB/%dblk",
		fewRounds, cancelledFewBytes, cancelledFewBlocks, manyRounds, cancelledManyBytes, cancelledManyBlocks)

	extraRounds := manyRounds - fewRounds
	grownBlocks := cancelledManyBlocks - cancelledFewBlocks
	grownBytes := cancelledManyBytes - cancelledFewBytes
	// Both figures are asserted, not only the count this defect moves: a
	// per-round retention of blocks that happen to be recycled would show up in
	// the byte total alone, and a message that prints a number it does not
	// check is an invitation to read it as checked.
	if grownBlocks != 0 || grownBytes != 0 {
		t.Fatalf(
			"a task cancelled at its first suspension point does not give back the scope its poll opened: %d blocks / %d bytes still live at exit over %d extra cancelled rounds (%.2f blocks and %.1f bytes per cancelled task), while the same program with nothing cancelled is flat at %d blocks. The block is rt_scope: rt_scope_enter allocates it, and a completion path that reaches mark_done without scope_exit_locked abandons it -- which is what the single-worker control runner did while it applied poll outcomes through a copy of apply_poll_outcome's switch instead of through apply_poll_outcome",
			grownBlocks, grownBytes, extraRounds,
			float64(grownBlocks)/float64(extraRounds), float64(grownBytes)/float64(extraRounds),
			keptFewBlocks,
		)
	}
}
