package vm_test

import (
	"strings"
	"testing"
	"time"
)

// RV2-DEBT-324, the program-level row. A holder mints a far channel of
// capacity one on the other shard and runs two anchored sends: the second
// parks the body on a full channel, so the holder is awaiting a reply when
// its parent cancels it. The cancel tears the caller down while the request
// is in flight; the late dispatch answers a caller that is gone and drops the
// UNSHIPPED state through its release glue. That glue used to release the
// anchor field -- a copy of the caller's far-handle token -- while the
// holder's own frame drop released the same token: valgrind read
// `Invalid read of size 4` in rt_far_channel_handle_drop under
// `drop.<state>` in three of six runs. The state's glue now leaves the lease
// field alone (Module.CrossingLeaseFields), so the token has one owner on
// every path.
//
// The row asserts memory SAFETY (no Memcheck error report) and the program's
// answer, not a strict-zero leak count: in about one run in eight the cancel
// lands while the far-channel CREATE is still in flight and the create's
// pending and token are never reclaimed -- RV2-DEBT-328, a different path,
// pinned there rather than absorbed here.
const runtimeV2AnchorLeaseCancelSource = `async fn spin(count: int) -> int {
    let mut i = 0;
    while i < count {
        checkpoint().await();
        i = i + 1;
    }
    return count;
}

async fn main_async() -> int {
    let holder = spawn async {
        let ch: far Channel<int> = channel_on::<int>(shard(1:ShardId), 1);
        let s1: TaskResult<nothing> = on ch { ch.send(41); ret nothing; };
        let _ = s1;
        let s2: TaskResult<nothing> = on ch { ch.send(42); ret nothing; };
        let _ = s2;
        ret 1;
    };
    let _ = spin(64).await();
    holder.cancel();
    let r = holder.await();
    let cancelled = compare r {
        Cancelled() => true;
        Success(_) => false;
    };
    if cancelled {
        print("holder-cancelled");
        return 0;
    }
    print("holder-finished");
    return 2;
}

@entrypoint
fn main() -> int {
    let task = spawn main_async();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

func TestRuntimeV2AnchoredCancelInFlightKeepsOneHandleOwner(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2AnchorLeaseCancelSource, nil)
	env := append(envWithStdlib(repoRoot(t)),
		"SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1")
	// Six rounds: the double drop showed in three of six before the fix, so
	// one green run would say nothing.
	for round := range 6 {
		stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 60*time.Second)
		if m := valgrindMemcheckErrorRE.FindString(stderr); m != "" {
			t.Fatalf("round %d: memcheck reported %q\nstdout:\n%s\nstderr:\n%s", round, m, stdout, stderr)
		}
		// Valgrind's own exit code is the program's when memcheck found no
		// error; a leak (RV2-DEBT-328) does not change it.
		if exitCode != 0 || !strings.Contains(stdout, "holder-cancelled") {
			t.Fatalf("round %d: exit=%d\nstdout:\n%s\nstderr:\n%s", round, exitCode, stdout, stderr)
		}
	}
}
