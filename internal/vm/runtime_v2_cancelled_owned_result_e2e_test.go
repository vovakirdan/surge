package vm_test

import (
	"strings"
	"testing"
	"time"
)

// RV2-DEBT-263, the OWNING-value row. Every other row in this lane answers with
// an `int`, and an int proves nothing about reclamation: a task result up to
// RT_VALUE_CELL_INLINE_BYTES lives in the cell's own bytes and owns no block
// (cell_fits_inline, rt_value_cell.c), so a refusal that got the ownership
// wrong would still leak nothing and free nothing twice.
//
// A composite of two strings is wider than that run, so the result slot holds a
// block it allocated AND two counted payloads inside it. That is the shape a
// user type actually has, and after RV2-DEBT-263 "the completion refuses the
// value its body produced" is an ORDINARY path for it: the timer cancels the
// target, the target's body has already run to its `return`, and
// rt_task_result_refuse hands the value to the lane's deferred release --
// dropping the members through the descriptor and freeing the block once the
// lane holds no scheduler lock.
//
// The `produced` marker is the non-vacuity: it is printed by the body
// IMMEDIATELY before the return that produces the value, so a run that prints
// it and still answers Cancelled is a run where a value was produced and
// refused. Without it the row could pass having only ever cancelled tasks
// before they produced anything, which is a different path entirely (the
// suspension carries the cancellation and nothing is ever published).
//
// The `kept` half of each round is the acceptance twin: an uncancelled task
// with the same owning result must still deliver it, so a refusal that fired
// too eagerly -- or a hand-off that emptied a slot it should not have --
// shows up as a missing value rather than as silence.
//
// NEGATIVE CONTROLS, to reproduce deliberately:
//   - make rt_value_cell_hand_off leave cell->state at RT_SLOT_INITIALIZED:
//     the refusal destroys the value and reclaim_task destroys it again, which
//     valgrind reports as an invalid free;
//   - make rt_task_result_refuse return without handing anything off: the
//     block and both strings are definitely lost, once per cancelled round.
const runtimeV2CancelledOwnedResultSource = `type Owned = { label: string, extra: string };

fn owned(label: string) -> Owned {
    return Owned { label = label, extra = "payload" };
}

async fn spin(count: int) -> int {
    let mut i = 0;
    while i < count {
        checkpoint().await();
        i = i + 1;
    }
    return count;
}

async fn main_async() -> int {
    let mut round = 0;
    let mut timed = 0;
    while round < 4 {
        let t = spawn async {
            let _ = spin(2000).await();
            print("produced");
            ret owned("cancelled");
        };
        let r = timeout(t, 5:uint);
        let timed_out = compare r {
            Cancelled() => true;
            Success(_) => false;
        };
        if timed_out {
            timed = timed + 1;
        }
        let k = spawn async {
            ret owned("kept");
        };
        let kr = k.await();
        let kept = compare kr {
            Success(_) => true;
            Cancelled() => false;
        };
        if !kept {
            return 21;
        }
        round = round + 1;
    }
    if timed == 0 {
        return 22;
    }
    print("cancelled-owned-result-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let r = main_async().await();
    let code = compare r {
        Success(v) => v;
        Cancelled() => 99;
    };
    return code;
}
`

func TestRuntimeV2CancelledOwnedResultValgrindZero(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CancelledOwnedResultSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	// One worker makes the refusal run on the lane that polled; four put the
	// completion on a worker other than the canceller, which is the arrangement
	// the deferred release exists for.
	for _, shardCount := range []string{"1", "4"} {
		t.Run("shards_"+shardCount, func(t *testing.T) {
			env := overrideEnvVar(baseEnv, "SURGE_SHARDS", shardCount)
			env = overrideEnvVar(env, "SURGE_THREADS", shardCount)
			stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 300*time.Second)
			if hasValgrindMemcheckError(stderr) {
				t.Fatalf("valgrind reported a real memcheck error (invalid free / use-after-free / uninitialized read)\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
			if exitCode != 0 {
				reasons := map[int]string{
					21: "an uncancelled task with the same owning result did not deliver it",
					22: "the timer never won a round, so no value was ever refused",
					99: "main_async itself resolved Cancelled",
				}
				reason := reasons[exitCode]
				if reason == "" {
					reason = "the program did not run to its verdict"
				}
				t.Fatalf("cancelled owned-result e2e failed (exit=%d -- %s)\nstdout:\n%s\nstderr:\n%s",
					exitCode, reason, stdout, stderr)
			}
			if !strings.Contains(stdout, "cancelled-owned-result-ok") {
				t.Fatalf("cancelled owned-result e2e missing completion marker; stdout=%q", stdout)
			}
			// Non-vacuity, and only at one worker, where it is deterministic:
			// with a single worker the virtual clock fast-forwards to the
			// timer's deadline while the body sits between checkpoints, so the
			// body is always re-polled with its already-DONE child in hand and
			// runs on to its return. At four the cancel can instead land at a
			// suspension, and the body then answers Cancelled without ever
			// producing -- a different path, already correct before this lane,
			// which this row still runs for its reclamation value.
			if shardCount == "1" && !strings.Contains(stdout, "produced") {
				t.Fatalf("no round produced its value before being cancelled, so no refusal was exercised; stdout=%q",
					stdout)
			}
			definiteBytes, definiteBlocks := parseValgrindLeakMatch(valgrindDefiniteLeakRE, stderr)
			indirectBytes, indirectBlocks := parseValgrindLeakMatch(valgrindIndirectLeakRE, stderr)
			if definiteBytes != 0 || definiteBlocks != 0 || indirectBytes != 0 || indirectBlocks != 0 {
				t.Fatalf(
					"a refused task result leaks at shards=%s: definitely_lost=%dB/%dblk indirectly_lost=%dB/%dblk, want strict zero on both\nstderr:\n%s",
					shardCount, definiteBytes, definiteBlocks, indirectBytes, indirectBlocks, stderr,
				)
			}
		})
	}
}
