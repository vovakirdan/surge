package vm_test

import (
	"strings"
	"testing"
	"time"
)

// The ordering rule cancel_task's wake placement implements (rt_task_complete.c):
// a cancel reaches the leaves before it makes anyone runnable again, so the task
// it was aimed at is polled with its cancelled children already DONE.
//
// Without that placement the parent is enqueued ahead of the children the same
// cancel is about to reach. It is polled first, the child it awaits is still
// WAITING, rt_task_poll has nothing to deliver, and rt_async_yield ends the
// parent at its suspension -- so the awaited result the child is about to
// publish is unobservable to it, and the whole parent turn can only re-park it
// on its scope key. This row is the fast assertion of the fixed order; the
// reclamation consequence (a value produced after the cancel and refused by the
// completion) is what TestRuntimeV2CancelledOwnedResultValgrindZero measures.
//
// The row is deliberately ONE shard and ONE worker. That is the topology where
// the ready queue is a single order and the rule is therefore observable at all;
// at four shards the parent and its child sit on different carriers and which
// one is polled first is not this rule's question.
//
// Non-vacuity is two markers, not one. `timed-out` proves a cancel was actually
// delivered -- without it a run where the timer never won would print
// `parent-resumed` from an ordinary uncancelled return and say nothing about
// cancel ordering. `parent-resumed` then proves the cancelled parent got its
// turn WITH the child's answer in hand rather than unwinding at the suspension.
//
// Revert check: move the wake in cancel_task back above the child walk and this
// row fails with `parent-resumed` missing.
const runtimeV2CancelReachesLeavesFirstSource = `async fn spin(count: int) -> int {
    let mut i = 0;
    while i < count {
        checkpoint().await();
        i = i + 1;
    }
    return count;
}

async fn main_async() -> int {
    let t = spawn async {
        let inner = spin(4096).await();
        let child_answered = compare inner {
            Success(_) => 1;
            Cancelled() => 2;
        };
        print("parent-resumed");
        ret child_answered;
    };
    let r = timeout(t, 5:uint);
    let timed_out = compare r {
        Cancelled() => true;
        Success(_) => false;
    };
    if !timed_out {
        return 21;
    }
    print("timed-out");
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

func TestRuntimeV2CancelReachesLeavesBeforeItWakesTheTarget(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CancelReachesLeavesFirstSource, nil)
	env := envWithStdlib(repoRoot(t))
	env = overrideEnvVar(env, "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")

	_, result := runBinaryWithTimeout(t, outputPath, env, 60*time.Second)
	if result.exitCode != 0 {
		reason := "the program did not run to its verdict"
		switch result.exitCode {
		case 21:
			reason = "the timer never won, so nothing was cancelled"
		case 99:
			reason = "main_async itself resolved Cancelled"
		}
		t.Fatalf("cancel-order row failed (exit=%d -- %s)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode, reason, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "timed-out") {
		t.Fatalf("cancel-order row never delivered a cancel, so it asserts nothing; stdout=%q",
			result.stdout)
	}
	if !strings.Contains(result.stdout, "parent-resumed") {
		t.Fatalf("the cancel woke its target before it reached the target's children: the parent "+
			"unwound at its suspension instead of being polled with the cancelled child's answer "+
			"in hand; stdout=%q", result.stdout)
	}
}
