package vm_test

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// A remote channel whose element carries a counted block, end to end under the
// production capability. Every entry into the ring hands over a PRIVATE
// reference: a far-select SEND payload is un-shared in the relinquishing
// operand (site 2), and an anchored body's `ch.send(own f)` gives away the
// capture's own reference, made private by the caller when the capture entered
// the state (site 1). Each program keeps a sibling holder of the block alive on
// the sending side and prints it after the crossing, so the un-share has a
// clone to make, and `unshare_clones` on the TRACE_RESIDENT exit line counts
// exactly those clones; the receiving shard reads the value inside its own
// anchored body, because a `float` does not ride the reply yet (S2).
//
// Red on the tree before: every program here was refused at the channel's
// creation (FUT7020); with the gate narrowed and no un-share on the anchored
// send, the first two would share one block between two shards.
const runtimeV2FarSelectFloatSendSource = `
async fn take(ch: far Channel<float>) -> int {
    let seen: TaskResult<int> = on ch {
        let v: Option<float> = ch.recv();
        ret compare v { Some(x) => x > 1.0 ? 1 : 2; nothing => 0; };
    };
    return compare seen { Success(x) => x; Cancelled() => 9; };
}

async fn run() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(1:ShardId), 4);
    let a: float = 1.5;
    let won: int = select {
        ch.send(a) => 1;
    };
    if won != 1 { return 11; }
    let got: int = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 9; };
    if got != 1 { return 12; }
    print(a to string);
    print("unshare-ok");
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

const runtimeV2AnchoredFloatSendSource = `
async fn take(ch: far Channel<float>) -> int {
    let seen: TaskResult<int> = on ch {
        let v: Option<float> = ch.recv();
        ret compare v { Some(x) => x > 1.0 ? 1 : 2; nothing => 0; };
    };
    return compare seen { Success(x) => x; Cancelled() => 9; };
}

async fn run() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(1:ShardId), 4);
    let a: float = 1.5;
    let f: float = a;
    let sent: TaskResult<nothing> = on ch { ch.send(own f); ret nothing; };
    let ok: int = compare sent { Success(_) => 0; Cancelled() => 1; };
    if ok != 0 { return 11; }
    let got: int = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 9; };
    if got != 1 { return 12; }
    print(a to string);
    print("unshare-ok");
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

// A `@copy` union carrying a float, built on the caller and given away by the
// anchored send: the capture's un-share clones the leaf the caller's `a` still
// holds. The receiver reads the leaf inside its anchored body.
const runtimeV2AnchoredCopyUnionSendSource = `
@copy
type P = { v: float };
tag Held(P);
tag Empty();
@copy
type U = Held(P) | Empty();

async fn take(ch: far Channel<U>) -> int {
    let seen: TaskResult<int> = on ch {
        let v: Option<U> = ch.recv();
        ret compare v {
            Some(u) => compare u { Held(p) => p.v > 2.0 ? 1 : 2; Empty() => 3; };
            nothing => 0;
        };
    };
    return compare seen { Success(x) => x; Cancelled() => 9; };
}

async fn run() -> int {
    let ch: far Channel<U> = channel_on::<U>(shard(1:ShardId), 4);
    let a: float = 2.5;
    let held: U = Held(P{ v: a });
    let sent: TaskResult<nothing> = on ch { ch.send(own held); ret nothing; };
    let ok: int = compare sent { Success(_) => 0; Cancelled() => 1; };
    if ok != 0 { return 11; }
    let got: int = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 9; };
    if got != 1 { return 12; }
    print(a to string);
    print("unshare-ok");
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

// A non-Copy union carrying a float, moved by a far-select SEND arm: the
// payload's un-share (site 2) clones the leaf the caller's `a` still holds.
const runtimeV2FarSelectUnionSendSource = `
type P = { v: float };
tag Held(P);
tag Empty();
type U = Held(P) | Empty();

async fn take(ch: far Channel<U>) -> int {
    let seen: TaskResult<int> = on ch {
        let v: Option<U> = ch.recv();
        ret compare v {
            Some(u) => compare u { Held(p) => p.v > 2.0 ? 1 : 2; Empty() => 3; };
            nothing => 0;
        };
    };
    return compare seen { Success(x) => x; Cancelled() => 9; };
}

async fn run() -> int {
    let ch: far Channel<U> = channel_on::<U>(shard(1:ShardId), 4);
    let a: float = 2.5;
    let held: U = Held(P{ v: a });
    let won: int = select {
        ch.send(own held) => 1;
    };
    if won != 1 { return 11; }
    let got: int = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 9; };
    if got != 1 { return 12; }
    print(a to string);
    print("unshare-ok");
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

// Two anchored producers on a capacity-1 channel: both park on capacity and
// re-enter their body from the top when woken. Each keeps a sibling of its
// value alive, so each capture is cloned ONCE, when it enters the state on the
// producer's own thread -- and never again on a replay, which re-derives the
// local from the state and finds it already private. The count is two
// whatever the parks did.
const runtimeV2AnchoredFloatSendParkSource = `
async fn producer(ch: far Channel<float>, value: float) -> int {
    let keep: float = value;
    let sent: TaskResult<nothing> = on ch { ch.send(own value); ret nothing; };
    let ok: int = compare sent { Success(_) => 0; Cancelled() => 1; };
    if keep < 0.0 { return 8; }
    return ok;
}

async fn take(ch: far Channel<float>) -> int {
    let seen: TaskResult<int> = on ch {
        let v: Option<float> = ch.recv();
        ret compare v { Some(x) => x > 1.0 ? 1 : 2; nothing => 0; };
    };
    return compare seen { Success(x) => x; Cancelled() => 9; };
}

async fn run() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(1:ShardId), 1);
    let first: Task<int> = spawn producer(ch.share(), 1.5);
    let second: Task<int> = spawn producer(ch.share(), 2.5);
    let third: Task<int> = spawn producer(ch.share(), 3.5);
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
    print("unshare-ok");
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

// The same three producers, drained in a BURST: the takes follow one another
// with no checkpoint between them, so a value the wake of a parked producer
// just moved into the ring is taken and released before that producer runs
// again. Its replayed prefix then re-derives the capture's local from the
// state -- bits naming a block the receiver has already freed -- and must not
// touch it: not a retain, not a clone, not even the read of the count an
// un-share would make. Valgrind reads no invalid access, and the count is
// still one clone per capture.
const runtimeV2AnchoredFloatSendBurstDrainSource = `
async fn producer(ch: far Channel<float>, value: float) -> int {
    let keep: float = value;
    let sent: TaskResult<nothing> = on ch { ch.send(own value); ret nothing; };
    let ok: int = compare sent { Success(_) => 0; Cancelled() => 1; };
    if keep < 0.0 { return 8; }
    return ok;
}

async fn take(ch: far Channel<float>) -> int {
    let seen: TaskResult<int> = on ch {
        let v: Option<float> = ch.recv();
        ret compare v { Some(x) => x > 1.0 ? 1 : 2; nothing => 0; };
    };
    return compare seen { Success(x) => x; Cancelled() => 9; };
}

async fn run() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(1:ShardId), 1);
    let first: Task<int> = spawn producer(ch.share(), 1.5);
    let second: Task<int> = spawn producer(ch.share(), 2.5);
    let third: Task<int> = spawn producer(ch.share(), 3.5);
    checkpoint().await();
    checkpoint().await();
    checkpoint().await();
    let a: int = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 9; };
    let b: int = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 9; };
    let c: int = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 9; };
    let p1: int = compare first.await() { Success(x) => x; Cancelled() => 1; };
    let p2: int = compare second.await() { Success(x) => x; Cancelled() => 1; };
    let p3: int = compare third.await() { Success(x) => x; Cancelled() => 1; };
    if p1 + p2 + p3 != 0 { return 11; }
    if a + b + c != 3 { return 12; }
    print("unshare-ok");
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

// The zero control: the moved union is its block's ONLY holder at the SEND
// arm (the literal's temp was released when the binding was built), so the
// un-share finds a count of one and clones nothing. A literal sent directly
// is NOT a zero: the lowering materializes it into a temp that is still alive
// at the arm, and the relinquishing read of that temp clones once.
const runtimeV2FarSelectSoleHolderSendSource = `
type P = { v: float };
tag Held(P);
tag Empty();
type U = Held(P) | Empty();

async fn take(ch: far Channel<U>) -> int {
    let seen: TaskResult<int> = on ch {
        let v: Option<U> = ch.recv();
        ret compare v {
            Some(u) => compare u { Held(p) => p.v > 1.0 ? 1 : 2; Empty() => 3; };
            nothing => 0;
        };
    };
    return compare seen { Success(x) => x; Cancelled() => 9; };
}

async fn run() -> int {
    let ch: far Channel<U> = channel_on::<U>(shard(1:ShardId), 4);
    let held: U = Held(P{ v: 1.5 });
    let won: int = select {
        ch.send(own held) => 1;
    };
    if won != 1 { return 11; }
    let got: int = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 9; };
    if got != 1 { return 12; }
    print("unshare-ok");
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

func TestRuntimeV2UnshareClonesOnTheWayIntoARemoteChannel(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		clones uint64
	}{
		{"far-select send of a float with a sibling alive", runtimeV2FarSelectFloatSendSource, 1},
		{"anchored send of a float captured beside a sibling", runtimeV2AnchoredFloatSendSource, 1},
		{"anchored send of a copy union carrying a float", runtimeV2AnchoredCopyUnionSendSource, 1},
		{"far-select send of a union carrying a float", runtimeV2FarSelectUnionSendSource, 1},
		{"three anchored producers parking on a capacity-1 channel", runtimeV2AnchoredFloatSendParkSource, 3},
		{"three anchored producers drained in a burst", runtimeV2AnchoredFloatSendBurstDrainSource, 3},
		{"far-select send of a union that is its block's only holder", runtimeV2FarSelectSoleHolderSendSource, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outputPath := buildRuntimeV2CrossingSource(t, tc.src, nil)
			for _, shards := range []int{2, 8} {
				values, line := runUnshareProgram(t, outputPath, shards)
				t.Logf("shards=%d %s", shards, line)
				if clones := unshareClonesField(t, values, line); clones != tc.clones {
					t.Fatalf("shards=%d: unshare_clones = %d, want %d:\n%s", shards, clones, tc.clones, line)
				}
				if underflows := values["underflows"]; underflows != 0 {
					t.Fatalf("shards=%d: %d releases outran their acquires:\n%s", shards, underflows, line)
				}
			}
		})
	}
}

// The Rule-13 red: with the leaf compiled as the identity, the count is 0 and
// the row above would go red on this build.
func TestRuntimeV2UnshareClonesOnTheWayIntoARemoteChannelNegativeControl(t *testing.T) {
	t.Setenv("SURGE_INTERNAL_RUNTIME_NEGATIVE_CONTROL", "RV2_BIGFLOAT_UNSHARE_NEGATIVE_CONTROL")
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2FarSelectFloatSendSource, nil)
	values, line := runUnshareProgram(t, outputPath, 2)
	t.Logf("%s", line)
	if clones := unshareClonesField(t, values, line); clones != 0 {
		t.Fatalf("negative control: unshare_clones = %d with the leaf compiled as the identity:\n%s", clones, line)
	}
}

// The two float programs and the parking program under valgrind: the ring's
// reference is private, the sibling's stays with the sender, nothing is lost or
// freed twice on either shard. The union programs read their leaf through a
// compare on a `@copy` payload, which leaks on its own (RV2-DEBT-340), so they
// are not in this row.
func TestRuntimeV2UnshareIntoARemoteChannelLeaksNothing(t *testing.T) {
	requireOwnershipValgrind(t, exec.LookPath)
	for name, src := range map[string]string{
		"far-select send of a float":                  runtimeV2FarSelectFloatSendSource,
		"anchored send of a float":                    runtimeV2AnchoredFloatSendSource,
		"three anchored producers park":               runtimeV2AnchoredFloatSendParkSource,
		"three anchored producers drained in a burst": runtimeV2AnchoredFloatSendBurstDrainSource,
	} {
		t.Run(name, func(t *testing.T) {
			outputPath := buildRuntimeV2CrossingSource(t, src, nil)
			env := envWithStdlib(repoRoot(t))
			env = overrideEnvVar(env, "SURGE_SHARDS", "2")
			env = overrideEnvVar(env, "SURGE_THREADS", "2")
			stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 180*time.Second)
			if exitCode != 0 || !strings.Contains(stdout, "unshare-ok") {
				t.Fatalf("program failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
			}
			lostBytes, lostBlocks, err := parseValgrindDefinitelyLost(stderr)
			if err != nil {
				t.Fatalf("could not read the valgrind leak summary: %v\nstderr:\n%s", err, stderr)
			}
			if hasValgrindMemcheckError(stderr) || lostBytes != 0 || lostBlocks != 0 {
				t.Fatalf("memory gate: memcheck_error=%t definitely_lost=%d bytes/%d blocks, want none\nstderr:\n%s",
					hasValgrindMemcheckError(stderr), lostBytes, lostBlocks, stderr)
			}
		})
	}
}
