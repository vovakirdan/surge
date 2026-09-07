package vm_test

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The sink the widened `on` capture gate newly reaches: an anchored body's
// `ch.send`, given the capture itself.
//
// Opening the gate to a bare `[T]` made a payload nobody could write before
// writable -- `on ch { ch.send(own xs); ret nothing; }` over a far
// `Channel<int[]>` -- and it arrived with TWO owners. The ring keeps the
// array's header, and the body still owed that capture a drop
// (registerCrossingBodyOwnership), so the body's scope exit freed the buffer
// the ring then freed again, or that a receiver had already taken and
// released. Sema now holds the payload to `own <captured binding>` and records
// the move, and the lowering withholds the body's drop for it, exactly as both
// have always done for a payload whose element shares a counted block.
//
// Red on the tree before that arm: this program built and died with a
// SEGMENTATION FAULT (exit 139) at SURGE_SHARDS/THREADS 2 and at 8 -- the
// receiver had taken the array and released it before the producer's body
// ended. The same program with nothing received died with "free(): double free
// detected in tcache 2" instead. At 63ecd58b neither compiled: the capture was
// refused one line earlier with SEM3168.
//
// Two routes, because the rule has two arms and a red must say which. The
// `int[]` route is the new arm's -- an `int` shares no counted block, so the
// element question never noticed the array. The `float[]` route is the OLDER
// arm's, unchanged, and it is here as the control on the order: the two arms
// must not both answer one payload.
const runtimeV2AnchoredArraySendSource = `
async fn take_ints(ch: far Channel<int[]>) -> int {
    let seen: TaskResult<int> = on ch {
        let got: Option<int[]> = ch.recv();
        ret compare got { Some(ys) => ys[0] + ys[1] + ys[2]; nothing => 0; };
    };
    return compare seen { Success(x) => x; Cancelled() => 9; };
}

async fn take_floats(ch: far Channel<float[]>) -> int {
    let seen: TaskResult<int> = on ch {
        let got: Option<float[]> = ch.recv();
        ret compare got { Some(ys) => ys[0] > 1.0 ? 1 : 2; nothing => 0; };
    };
    return compare seen { Success(x) => x; Cancelled() => 9; };
}

async fn run() -> int {
    let ci: far Channel<int[]> = channel_on::<int[]>(shard(1:ShardId), 4);
    let xs: int[] = [1, 2, 3];
    let si: TaskResult<nothing> = on ci { ci.send(own xs); ret nothing; };
    if compare si { Success(_) => 0; Cancelled() => 1; } != 0 { print("FAIL int send"); return 11; }
    let gi: int = compare take_ints(ci.share()).await() { Success(x) => x; Cancelled() => 9; };
    if gi != 6 { print("FAIL int got="); print(gi to string); return 12; }

    let cf: far Channel<float[]> = channel_on::<float[]>(shard(1:ShardId), 4);
    let fs: float[] = [1.5, 2.5];
    let sf: TaskResult<nothing> = on cf { cf.send(own fs); ret nothing; };
    if compare sf { Success(_) => 0; Cancelled() => 1; } != 0 { print("FAIL float send"); return 13; }
    let gf: int = compare take_floats(cf.share()).await() { Success(x) => x; Cancelled() => 9; };
    if gf != 1 { print("FAIL float got="); print(gf to string); return 14; }

    print("anchored-array-unshare-ok");
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

// The replay question, asked of an array. An anchored body has no async split:
// a send that parks on capacity re-enters the body from its FIRST instruction
// when it wakes, so a give-away is only safe if the replayed prefix touches
// nothing. Here it touches nothing because there is nothing to touch -- the
// prefix is the unpack of the capture from the state, and the reference the
// caller made private travels out with the send.
//
// Three producers on a capacity-1 far `Channel<int[]>` therefore park, wake and
// replay, and each array still arrives exactly once. Red before the arm: this
// program segfaults (exit 139) at 2 and at 8 shards.
const runtimeV2AnchoredArraySendParkSource = `
async fn producer(ch: far Channel<int[]>, v: int) -> int {
    let xs: int[] = [v, v, v];
    let sent: TaskResult<nothing> = on ch { ch.send(own xs); ret nothing; };
    return compare sent { Success(_) => 0; Cancelled() => 1; };
}

async fn take(ch: far Channel<int[]>) -> int {
    let seen: TaskResult<int> = on ch {
        let got: Option<int[]> = ch.recv();
        ret compare got { Some(ys) => ys[0] + ys[1] + ys[2] > 0 ? 1 : 2; nothing => 0; };
    };
    return compare seen { Success(x) => x; Cancelled() => 9; };
}

async fn run() -> int {
    let ch: far Channel<int[]> = channel_on::<int[]>(shard(1:ShardId), 1);
    let first: Task<int> = spawn producer(ch.share(), 1);
    let second: Task<int> = spawn producer(ch.share(), 2);
    let third: Task<int> = spawn producer(ch.share(), 3);
    checkpoint().await();
    checkpoint().await();
    let a: int = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 9; };
    checkpoint().await();
    let b: int = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 9; };
    checkpoint().await();
    let c: int = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 9; };
    let p1: int = compare first.await() { Success(x) => x; Cancelled() => 1; };
    let p2: int = compare second.await() { Success(x) => x; Cancelled() => 1; };
    let p3: int = compare third.await() { Success(x) => x; Cancelled() => 1; };
    if p1 + p2 + p3 != 0 { return 11; }
    if a + b + c != 3 { return 12; }
    print("anchored-array-park-unshare-ok");
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

// Both arrays reach the far side with the values they left with, at 2 shards
// and at 8, and so do the three that park on the way. The clone ledger is read
// too: no capture here has a sibling holder alive, so the walk finds a count of
// one and clones nothing -- a number would mean the give-away had let the body
// keep a reference after all.
func TestRuntimeV2AnchoredSendGivesACapturedArrayAway(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"an int array and a float array, each received back", runtimeV2AnchoredArraySendSource},
		{"three producers parking on a capacity-1 channel", runtimeV2AnchoredArraySendParkSource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertAnchoredArraySendArrives(t, tc.src)
		})
	}
}

func assertAnchoredArraySendArrives(t *testing.T, src string) {
	t.Helper()
	outputPath := buildRuntimeV2CrossingSource(t, src, nil)
	for _, shards := range []int{2, 8} {
		values, line := runUnshareProgram(t, outputPath, shards)
		t.Logf("shards=%d %s", shards, line)
		if clones := unshareClonesField(t, values, line); clones != 0 {
			t.Fatalf("shards=%d: unshare_clones = %d, want 0 (neither capture is shared):\n%s",
				shards, clones, line)
		}
		if underflows := values["underflows"]; underflows != 0 {
			t.Fatalf("shards=%d: %d releases outran their acquires:\n%s", shards, underflows, line)
		}
	}
}

// The instrument that would have caught the defect: `underflows == 0` above is
// the runtime's own ledger, and a buffer freed by two owners never reaches it.
// Both programs report ERROR SUMMARY 0 and lose nothing here; before the arm
// the first reported 2 invalid frees and 6 invalid reads.
func TestRuntimeV2AnchoredSendOfACapturedArrayLeaksNothing(t *testing.T) {
	requireOwnershipValgrind(t, exec.LookPath)
	for name, src := range map[string]string{
		"an int array and a float array, each received back": runtimeV2AnchoredArraySendSource,
		"three producers parking on a capacity-1 channel":    runtimeV2AnchoredArraySendParkSource,
	} {
		t.Run(name, func(t *testing.T) {
			outputPath := buildRuntimeV2CrossingSource(t, src, nil)
			for _, shards := range []string{"2", "8"} {
				env := envWithStdlib(repoRoot(t))
				env = overrideEnvVar(env, "SURGE_SHARDS", shards)
				env = overrideEnvVar(env, "SURGE_THREADS", shards)
				stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 180*time.Second)
				if exitCode != 0 || !strings.Contains(stdout, "unshare-ok") {
					t.Fatalf("program failed under valgrind (shards=%s exit=%d)\nstdout:\n%s\nstderr:\n%s",
						shards, exitCode, stdout, stderr)
				}
				lostBytes, lostBlocks, err := parseValgrindDefinitelyLost(stderr)
				if err != nil {
					t.Fatalf("could not read the valgrind leak summary: %v\nstderr:\n%s", err, stderr)
				}
				if hasValgrindMemcheckError(stderr) || lostBytes != 0 || lostBlocks != 0 {
					t.Fatalf("memory gate (shards=%s): memcheck_error=%t definitely_lost=%d bytes/%d blocks, want none\nstderr:\n%s",
						shards, hasValgrindMemcheckError(stderr), lostBytes, lostBlocks, stderr)
				}
			}
		})
	}
}
