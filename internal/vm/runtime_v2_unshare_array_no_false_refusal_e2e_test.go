package vm_test

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The other side of the widening, and the more important one: an ORDINARY
// owned array must still cross.
//
// Every crossing of a value carrying a dynamic array now takes the array view
// registry's lock and asks two questions. rt_array_unshare_walk refuses only a
// header whose capacity marks it a view, or a base some live view still reads;
// an owned, never-sliced array is neither. That is a reading of the runtime's
// source, and a reading is not a measurement -- these rows are. A false refusal
// here would be worse than the hole this lane closes, so they are first-class
// rows and not a smoke test.
//
// One program carries five routes, so a red says WHICH one broke by the value
// it prints: a `blocking` capture, an immediate `on` through a @shard_movable
// holder, a `string[]` through the same holder shape (a handle element, whose
// callback is null for a different reason than an int's), a far
// `Channel<int[]>` filled by a select SEND arm, and a `blocking` RESULT.
const runtimeV2UnshareOwnedArraysStillCrossSource = `
@shard_movable
type IntHolder = { xs: int[] };

@shard_movable
type StrHolder = { ss: string[] };

fn total(xs: own int[]) -> int {
    return xs[0] + xs[1] + xs[2];
}

fn total_holder(h: own IntHolder) -> int {
    return h.xs[0] + h.xs[1] + h.xs[2];
}

fn widths(h: own StrHolder) -> int {
    return (h.ss[0].__len() to int) + (h.ss[1].__len() to int);
}

async fn take(ch: far Channel<int[]>) -> int {
    let seen: TaskResult<int> = on ch {
        let got: Option<int[]> = ch.recv();
        ret compare got {
            Some(ys) => ys[0] + ys[1] + ys[2];
            nothing => 0;
        };
    };
    return compare seen { Success(x) => x; Cancelled() => 9; };
}

async fn run() -> int {
    let xs: int[] = [1, 2, 3];
    let job: Task<int> = blocking { ret total(own xs); };
    let a: int = compare job.await() { Success(x) => x; Cancelled() => 0 - 2; };
    if a != 6 { print("FAIL a="); print(a to string); return 1; }

    let ys: int[] = [4, 5, 6];
    let h: IntHolder = IntHolder{ xs: ys };
    let reply: TaskResult<int> = on shard(1:ShardId) { ret total_holder(own h); };
    let b: int = compare reply { Success(x) => x; Cancelled() => 0 - 2; };
    if b != 15 { print("FAIL b="); print(b to string); return 1; }

    let ss: string[] = ["ab", "cde"];
    let sh: StrHolder = StrHolder{ ss: ss };
    let job2: Task<int> = blocking { ret widths(own sh); };
    let c: int = compare job2.await() { Success(x) => x; Cancelled() => 0 - 2; };
    if c != 5 { print("FAIL c="); print(c to string); return 1; }

    let ch: far Channel<int[]> = channel_on::<int[]>(shard(1:ShardId), 4);
    let zs: int[] = [7, 8, 9];
    let won: int = select {
        ch.send(own zs) => 1;
    };
    if won != 1 { print("FAIL won="); print(won to string); return 1; }
    let d: int = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 9; };
    if d != 24 { print("FAIL d="); print(d to string); return 1; }

    let job3: Task<int[]> = blocking { let rs: int[] = [1, 1, 1]; ret rs; };
    let e: int = compare job3.await() {
        Success(rs) => rs[0] + rs[1] + rs[2];
        Cancelled() => 0 - 2;
    };
    if e != 3 { print("FAIL e="); print(e to string); return 1; }

    print("array-unshare-ok");
    print(((a + b + c + d + e) * 1) to string);
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`

// Five owned arrays cross five different sinks, at 2 shards and at 8, and every
// one of them arrives with the values it left with. The clone ledger is checked
// too: the calls this lane adds carry a null callback and clone nothing, so a
// number here would mean the widening reached an element it should not walk.
func TestRuntimeV2UnshareOfOwnedIntAndStringArraysIsNotRefused(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2UnshareOwnedArraysStillCrossSource, nil)
	for _, shards := range []int{2, 8} {
		values, line := runUnshareProgram(t, outputPath, shards)
		t.Logf("shards=%d %s", shards, line)
		if clones := unshareClonesField(t, values, line); clones != 0 {
			t.Fatalf("shards=%d: unshare_clones = %d, want 0 (no element here is counted, so the added "+
				"calls must clone nothing):\n%s", shards, clones, line)
		}
		if underflows := values["underflows"]; underflows != 0 {
			t.Fatalf("shards=%d: %d releases outran their acquires:\n%s", shards, underflows, line)
		}
	}
}

// The same program under valgrind. `underflows == 0` above is the runtime's own
// ledger; this is a different instrument, and it is the one that would catch a
// buffer freed twice or a header leaked by the extra registry visit.
func TestRuntimeV2UnshareOfAnIntArrayLeaksNothing(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2UnshareOwnedArraysStillCrossSource, nil)
	requireOwnershipValgrind(t, exec.LookPath)
	for _, shards := range []string{"2", "8"} {
		env := envWithStdlib(repoRoot(t))
		env = overrideEnvVar(env, "SURGE_SHARDS", shards)
		env = overrideEnvVar(env, "SURGE_THREADS", shards)
		stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 180*time.Second)
		if exitCode != 0 || !strings.Contains(stdout, "array-unshare-ok") {
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
}
