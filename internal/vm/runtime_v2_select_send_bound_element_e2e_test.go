package vm_test

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// The way out the borrow refusal names, run end to end: an element bound to a
// name before the select, then given away.
//
// This is the program the defect was measured on, one line different. As
// `select { ch.send(own ps[0]) => 1; stop.recv() => 2; }` it compiled with NO
// diagnostic, and a reader of `p.b` answered 11 where the sender's second field
// is 22, at SURGE_SHARDS/THREADS 2 and at 8; sharpened to `Pair{ a: 0, b: 22 }`
// it answered 0. Those are the two numbers this row exists to make impossible,
// and they are the reason it pins the ANSWER rather than the exit code: that
// reader EXITED 0 on all forty runs, twenty at each width, so a row demanding
// only a clean exit would have been GREEN on the defect and would have proved
// nothing when the refusal landed.
//
// The exit code is no better a witness in the other direction: what arrives is
// an ADDRESS, and a reader that touches the field it landed on dereferences it
// -- `p.a + p.b` SIGSEGVs inside rt_bigint_add, sixty runs out of sixty on
// another x86-64 Linux host at this commit. Silent wrong answer and crash are
// the same corruption seen through different fields.
//
// `ch.send(own ps[0])` no longer compiles at all (SEM3105 names the borrow and
// this spelling as the way out; internal/buildpipeline holds that table), so
// what is left to prove here is that the advice DELIVERS: 33 and 22, at both
// widths.
const runtimeV2SelectSendBoundElementSource = `
@copy @shard_movable
type Pair = { a: int, b: int };

async fn take(ch: far Channel<Pair>) -> int {
    let seen: TaskResult<int> = on ch {
        let got: Option<Pair> = ch.recv();
        ret compare got { Some(p) => p.a + p.b; nothing => -1; };
    };
    return compare seen { Success(x) => x; Cancelled() => -2; };
}

async fn feed(ch: far Channel<Pair>, stop: far Channel<int>, first: int, second: int) -> int {
    let ps: Pair[] = [Pair{ a: first, b: second }, Pair{ a: 1, b: 2 }];
    let v: Pair = ps[0];
    if select { ch.send(own v) => 1; stop.recv() => 2; } != 1 { return -9; }
    return compare take(ch.share()).await() { Success(x) => x; Cancelled() => -3; };
}

async fn run() -> int {
    let ch: far Channel<Pair> = channel_on::<Pair>(shard(1:ShardId), 4);
    let stop: far Channel<int> = channel_on::<int>(shard(1:ShardId), 4);

    let sum: int = compare feed(ch.share(), stop.share(), 11, 22).await() {
        Success(x) => x;
        Cancelled() => -4;
    };
    print("sum=");
    print(sum to string);
    if sum != 33 { return 11; }

    let sharpened: int = compare feed(ch.share(), stop.share(), 0, 22).await() {
        Success(x) => x;
        Cancelled() => -4;
    };
    print("sharpened=");
    print(sharpened to string);
    if sharpened != 22 { return 12; }

    print("select-send-bound-element-ok");
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

func TestRuntimeV2SelectSendOfABoundElementDeliversTheWholeStruct(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2SelectSendBoundElementSource, nil)
	for _, shards := range []int{2, 8} {
		env := envWithStdlib(repoRoot(t))
		env = overrideEnvVar(env, "SURGE_SHARDS", strconv.Itoa(shards))
		env = overrideEnvVar(env, "SURGE_THREADS", strconv.Itoa(shards))
		_, result := runBinaryWithTimeout(t, outputPath, env, 60*time.Second)
		t.Logf("shards=%d exit=%d stdout=%q", shards, result.exitCode, result.stdout)
		if result.exitCode != 0 || !strings.Contains(result.stdout, "select-send-bound-element-ok") {
			t.Fatalf("shards=%d: the bound element did not arrive whole (exit=%d)\nstdout:\n%s\nstderr:\n%s",
				shards, result.exitCode, result.stdout, result.stderr)
		}
		// Pinned by value, not by exit code: 11 and 0 are what the reader
		// answered while the reference was on the ring.
		if !strings.Contains(result.stdout, "33") || !strings.Contains(result.stdout, "22") {
			t.Fatalf("shards=%d: stdout does not carry both sums:\n%s", shards, result.stdout)
		}
	}
}
