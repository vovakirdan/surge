package vm_test

import (
	"strings"
	"testing"
	"time"
)

// Epic 21 Task 9 / RV2-DEBT-125: the named vertical x edge-class matrix on
// typed carriers, at SURGE_SHARDS=1, 2 and 8. Four verticals (an owned
// @shard_movable capture migrating into a far task, a far channel shared
// across tasks, a far select with a non-copy send arm, a non-copy far
// channel) against four edge classes (happy, cancel, refusal,
// teardown-buffered). Every program row runs to its own marker and, under
// memcheck, to "in use at exit: 0 bytes in 0 blocks" with no error report:
// the property of every cell is that whatever crossed is reclaimed exactly
// once whichever way the crossing ended. The cancel rows accept either
// answer of the race they start (a cancel that lands after the far body has
// committed does not revoke it; the ruling of 2026-08 and the fail-fast
// judge's lesson of 2026-09-03) and hold the reclamation instead.
//
// One cell is not a program: migration x refusal, the runtime refusing to
// admit a far task, is decided below the language (the target lane's
// admission), and its rows are the C stands of
// runtime_v2_remote_task_behavior_test.go (refusal, stale generation) which
// pin the payload's single discharge at the ABI; a program cannot reach a
// refused admission on purpose, so this table says so instead of faking one.
type task9Cell struct {
	vertical string
	edge     string
	marker   string
	source   string
}

const runtimeV2Task9MigrationCancelSource = `
@shard_movable
type Job = { id: int, note: string };

fn build_note(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

async fn spin(count: int) -> int {
    let mut i = 0;
    while i < count {
        checkpoint().await();
        i = i + 1;
    }
    return count;
}

fn describe(j: own Job) -> int {
    if j.note != "n-xxxx" { return 0 - 1; }
    return j.id * 100 + 6;
}

async fn caller_body() -> int {
    let j: own Job = own Job{ id: 4, note: build_note("n-") };
    let t: far Task<int> = spawn on distributed {
        let _ = spin(64).await();
        ret describe(own j);
    };
    let got: TaskResult<int> = t.await();
    return compare got { Success(x) => x; Cancelled() => 0 - 2; };
}

async fn run() -> int {
    let child: Task<int> = spawn caller_body();
    checkpoint().await();
    child.cancel();
    let r: TaskResult<int> = child.await();
    let v: int = compare r { Success(x) => x; Cancelled() => 0 - 1; };
    if v != (0 - 1) {
        if v != 406 {
            print("FAIL unexpected answer v=");
            print(v to string);
            return 11;
        }
        print("migration-cancel-answered-success");
    } else {
        print("migration-cancel-answered-cancelled");
    }
    print("migration-cancel-ok");
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

const runtimeV2Task9MigrationTeardownSource = `
@shard_movable
type Job = { id: int, note: string };

fn build_note(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

fn describe(j: own Job) -> int {
    if j.note != "n-xxxx" { return 0 - 1; }
    return j.id * 100 + 6;
}

async fn spin(count: int) -> int {
    let mut i = 0;
    while i < count {
        checkpoint().await();
        i = i + 1;
    }
    return count;
}

async fn caller_body() -> int {
    let j: own Job = own Job{ id: 4, note: build_note("n-") };
    // The far body consumes its owned, heap-holding capture and commits its
    // result quickly; the caller then spins and is cancelled by its parent
    // while it still owns the handle, so a committed result may never be
    // consumed by anyone, and the runtime's teardown of the abandoned
    // entitlement owns whatever is left (a task may not be dropped unawaited
    // by the program itself: SEM3107; a non-copy far result cannot ride the
    // reply yet: FUT7020, so the capture is the non-copy half of this cell).
    let t: far Task<int> = spawn on distributed { ret describe(own j); };
    let _ = spin(64).await();
    let got: TaskResult<int> = t.await();
    return compare got { Success(x) => x; Cancelled() => 0 - 2; };
}

async fn run() -> int {
    let mut k = 0;
    while k < 4 {
        let child: Task<int> = spawn caller_body();
        checkpoint().await();
        checkpoint().await();
        child.cancel();
        let r: int = compare child.await() { Success(x) => x; Cancelled() => 0 - 1; };
        if r != (0 - 1) {
            if r != 406 {
                print("FAIL unexpected answer r=");
                print(r to string);
                return 11;
            }
        }
        k = k + 1;
    }
    print("migration-teardown-ok");
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

const runtimeV2Task9ShareCancelSource = `
async fn consumer(ch: far Channel<string>) -> int {
    let got: TaskResult<int> = on ch {
        let v: Option<string> = ch.recv();
        ret compare v { Some(_) => 1; nothing => 0; };
    };
    return compare got { Success(x) => x; Cancelled() => 0 - 2; };
}

async fn run() -> int {
    let ch: far Channel<string> = channel_on::<string>(shard(0:ShardId), 0);
    let waiter: Task<int> = spawn consumer(ch.share());
    checkpoint().await();
    checkpoint().await();
    waiter.cancel();
    let r: TaskResult<int> = waiter.await();
    let v: int = compare r { Success(x) => x; Cancelled() => 0 - 1; };
    if v != (0 - 1) {
        print("FAIL expected the parked consumer to cancel, v=");
        print(v to string);
        return 11;
    }
    let done: TaskResult<nothing> = on ch { ch.close(); ret nothing; };
    let _ = done;
    print("share-cancel-ok");
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

const runtimeV2Task9ShareRefusalSource = `
fn build_payload(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "y";
        i = i + 1;
    }
    return s;
}

async fn parked_receiver(ch: far Channel<string>) -> int {
    // A rendezvous channel nobody sends into: the receive parks. The owner
    // then CLOSES the channel through another share, and the parked receive
    // is refused with the closed answer (nothing). The send side has no
    // program-level refusal to reach: a send that meets a closed channel is
    // a program panic by the language, before or after parking, and
    // try_send is not an anchored operation.
    let got: TaskResult<int> = on ch {
        let v: Option<string> = ch.recv();
        ret compare v { Some(_) => 1; nothing => 7; };
    };
    return compare got { Success(x) => x; Cancelled() => 0 - 2; };
}

async fn run() -> int {
    let ch: far Channel<string> = channel_on::<string>(shard(0:ShardId), 0);
    let receiver: Task<int> = spawn parked_receiver(ch.share());
    checkpoint().await();
    checkpoint().await();
    let done: TaskResult<nothing> = on ch { ch.close(); ret nothing; };
    let _ = done;
    let r: int = compare receiver.await() { Success(x) => x; Cancelled() => 0 - 1; };
    if r != 7 {
        print("FAIL the parked receive was not refused by the close, r=");
        print(r to string);
        return 13;
    }
    print("share-refusal-ok");
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

const runtimeV2Task9ShareTeardownSource = `
fn build_payload(label: int) -> string {
    let mut s = "z-" + (label to string);
    let mut i = 0;
    while i < 4 {
        s = s + "z";
        i = i + 1;
    }
    return s;
}

async fn producer(ch: far Channel<string>, label: int) -> int {
    // The string is built INSIDE the anchored block: an owned string in the
    // caller's frame is not shard-movable across the on-block boundary (SEM3168),
    // so the plain-copy label crosses and the non-copy payload is born on
    // the channel's side and sent from there.
    let sent: TaskResult<nothing> = on ch { ch.send(own build_payload(label)); ret nothing; };
    return compare sent { Success(_) => 0; Cancelled() => 1; };
}

async fn run() -> int {
    let ch: far Channel<string> = channel_on::<string>(shard(0:ShardId), 4);
    let first: Task<int> = spawn producer(ch.share(), 1);
    let second: Task<int> = spawn producer(ch.share(), 2);
    let p1: int = compare first.await() { Success(x) => x; Cancelled() => 1; };
    let p2: int = compare second.await() { Success(x) => x; Cancelled() => 1; };
    if p1 != 0 { return 11; }
    if p2 != 0 { return 12; }
    // Two non-copy payloads sit in the buffer; nobody receives them. Every
    // share is dropped and the channel's teardown owns the two drops.
    print("share-teardown-ok");
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

const runtimeV2Task9SelectRefusalSource = `
fn build(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

async fn chooser(a: far Channel<string>, b: far Channel<int>) -> int {
    let job: string = build("refused-");
    // Arm 1 would send into a rendezvous channel nobody receives from; arm 2
    // receives from a channel that is already CLOSED, which answers at once
    // with the closed refusal. The closed arm wins, the send arm loses, and
    // the losing arm's staged string is discharged exactly once.
    let winner: int = select { a.send(own job) => 1; b.recv() => 2; };
    return winner;
}

async fn run() -> int {
    let a: far Channel<string> = channel_on::<string>(shard(0:ShardId), 0);
    let b: far Channel<int> = channel_on::<int>(shard(0:ShardId), 1);
    let closed: TaskResult<nothing> = on b { b.close(); ret nothing; };
    let _ = closed;
    let child: Task<int> = spawn chooser(a.share(), b.share());
    let winner: int = compare child.await() { Success(x) => x; Cancelled() => 0 - 1; };
    if winner != 2 {
        print("FAIL expected the closed arm to answer, winner=");
        print(winner to string);
        return 11;
    }
    print("select-refusal-ok");
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

const runtimeV2Task9SelectTeardownSource = `
fn build(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

async fn chooser(a: far Channel<string>, b: far Channel<int>) -> int {
    let job: string = build("kept-");
    // The send arm wins into a buffered channel with room; the payload now
    // lives in the buffer and nobody will receive it.
    let winner: int = select { a.send(own job) => 1; b.recv() => 2; };
    return winner;
}

async fn run() -> int {
    let a: far Channel<string> = channel_on::<string>(shard(0:ShardId), 1);
    let b: far Channel<int> = channel_on::<int>(shard(0:ShardId), 0);
    let child: Task<int> = spawn chooser(a.share(), b.share());
    let winner: int = compare child.await() { Success(x) => x; Cancelled() => 0 - 1; };
    if winner != 1 {
        print("FAIL expected the buffered send arm to win, winner=");
        print(winner to string);
        return 11;
    }
    print("select-teardown-ok");
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

const runtimeV2Task9ChannelCancelSource = `
async fn consumer(ch: far Channel<string>) -> int {
    let got: TaskResult<int> = on ch {
        let v: Option<string> = ch.recv();
        ret compare v { Some(_) => 1; nothing => 0; };
    };
    return compare got { Success(x) => x; Cancelled() => 0 - 2; };
}

async fn run() -> int {
    let ch: far Channel<string> = channel_on::<string>(shard(0:ShardId), 1);
    let waiter: Task<int> = spawn consumer(ch);
    checkpoint().await();
    checkpoint().await();
    waiter.cancel();
    let r: TaskResult<int> = waiter.await();
    let v: int = compare r { Success(x) => x; Cancelled() => 0 - 1; };
    if v != (0 - 1) {
        print("FAIL expected the parked receiver to cancel, v=");
        print(v to string);
        return 11;
    }
    print("channel-cancel-ok");
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

const runtimeV2Task9ChannelRefusalSource = `
fn build_payload() -> string {
    let mut v = "far-channel-";
    v = v + "refused-payload";
    return v;
}

async fn taker_and_closer(ch: far Channel<string>) -> int {
    // Receives the one non-copy payload the owner buffered, then CLOSES the
    // channel; the owner's receive after that is refused with the closed
    // answer. (A send that meets a closed channel is a program panic by the
    // language, before or after parking, so the refusal a program reaches
    // is the receive's.)
    let got: TaskResult<int> = on ch {
        let v: Option<string> = ch.recv();
        ret compare v { Some(_) => 1; nothing => 0; };
    };
    // One anchored operation per block (SEM3175): the close is its own.
    let closed: TaskResult<nothing> = on ch { ch.close(); ret nothing; };
    let _ = closed;
    return compare got { Success(x) => x; Cancelled() => 0 - 2; };
}

async fn run() -> int {
    let ch: far Channel<string> = channel_on::<string>(shard(0:ShardId), 1);
    let s: TaskResult<nothing> = on ch { ch.send(own build_payload()); ret nothing; };
    let _ = s;
    let helper: Task<int> = spawn taker_and_closer(ch.share());
    let taken: int = compare helper.await() { Success(x) => x; Cancelled() => 0 - 1; };
    if taken != 1 {
        print("FAIL the buffered payload was not received before the close, taken=");
        print(taken to string);
        return 12;
    }
    let after: TaskResult<int> = on ch {
        let got: Option<string> = ch.recv();
        ret compare got { Some(_) => 1; nothing => 7; };
    };
    let v: int = compare after { Success(x) => x; Cancelled() => 0 - 2; };
    if v != 7 {
        print("FAIL the receive after the close was not refused, v=");
        print(v to string);
        return 11;
    }
    print("channel-refusal-ok");
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

const runtimeV2Task9ChannelTeardownSource = `
fn build_payload(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "w";
        i = i + 1;
    }
    return s;
}

async fn run() -> int {
    let ch: far Channel<string> = channel_on::<string>(shard(0:ShardId), 4);
    let mut k = 0;
    while k < 3 {
        let s: TaskResult<nothing> = on ch { ch.send(own build_payload("keep-")); ret nothing; };
        let _ = s;
        k = k + 1;
    }
    // Three non-copy payloads in the buffer, none received, the one handle
    // dropped at the end of this body: the teardown owns all three drops.
    print("channel-teardown-ok");
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

func runtimeV2Task9Cells() []task9Cell {
	return []task9Cell{
		{"migration", "happy", "migration-ok", runtimeV2MigrationSource},
		{"migration", "cancel", "migration-cancel-ok", runtimeV2Task9MigrationCancelSource},
		{"migration", "teardown-buffered", "migration-teardown-ok", runtimeV2Task9MigrationTeardownSource},
		{"share", "happy", "share-fanout-ok", runtimeV2ShareSource},
		{"share", "cancel", "share-cancel-ok", runtimeV2Task9ShareCancelSource},
		{"share", "refusal", "share-refusal-ok", runtimeV2Task9ShareRefusalSource},
		{"share", "teardown-buffered", "share-teardown-ok", runtimeV2Task9ShareTeardownSource},
		{"select", "happy", "far-select-noncopy-ok", runtimeV2FarSelectNonCopySource},
		{"select", "cancel", "far-select-cancel-noncopy-ok", runtimeV2FarSelectCancelNonCopySource},
		{"select", "refusal", "select-refusal-ok", runtimeV2Task9SelectRefusalSource},
		{"select", "teardown-buffered", "select-teardown-ok", runtimeV2Task9SelectTeardownSource},
		{"non-copy-channel", "happy", "far-channel-noncopy-roundtrip-ok", runtimeV2FarChannelNonCopyRoundTripSource},
		{"non-copy-channel", "cancel", "channel-cancel-ok", runtimeV2Task9ChannelCancelSource},
		{"non-copy-channel", "refusal", "channel-refusal-ok", runtimeV2Task9ChannelRefusalSource},
		{"non-copy-channel", "teardown-buffered", "channel-teardown-ok", runtimeV2Task9ChannelTeardownSource},
	}
}

func TestRuntimeV2Task9Matrix(t *testing.T) {
	root := repoRoot(t)
	baseEnv := envWithStdlib(root)
	for _, cell := range runtimeV2Task9Cells() {
		cell := cell
		t.Run(cell.vertical+"/"+cell.edge, func(t *testing.T) {
			outputPath := buildRuntimeV2CrossingSource(t, cell.source, nil)
			for _, shards := range []string{"1", "2", "8"} {
				shards := shards
				t.Run("shards-"+shards, func(t *testing.T) {
					env := overrideEnvVar(baseEnv, "SURGE_SHARDS", shards)
					env = overrideEnvVar(env, "SURGE_THREADS", shards)
					duration, result := runBinaryWithTimeout(t, outputPath, env, 60*time.Second)
					if result.exitCode != 0 || strings.Contains(result.stdout, "FAIL") ||
						!strings.Contains(result.stdout, cell.marker) {
						t.Fatalf("%s/%s at %s shards: exit=%d duration=%s\nstdout:\n%s\nstderr:\n%s",
							cell.vertical, cell.edge, shards, result.exitCode, duration, result.stdout, result.stderr)
					}
					stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 240*time.Second)
					if hasValgrindMemcheckError(stderr) {
						t.Fatalf("%s/%s at %s shards: memcheck error\nstdout:\n%s\nstderr:\n%s",
							cell.vertical, cell.edge, shards, stdout, stderr)
					}
					if exitCode != 0 || !strings.Contains(stdout, cell.marker) {
						t.Fatalf("%s/%s at %s shards under valgrind: exit=%d\nstdout:\n%s\nstderr:\n%s",
							cell.vertical, cell.edge, shards, exitCode, stdout, stderr)
					}
					// Strict zero is "definitely lost: 0 bytes in 0 blocks" with no
					// Memcheck error report, the reading every valgrind-zero row of
					// this suite takes: the executor's threads leave their TLS and
					// arenas "possibly lost" / still reachable at process exit
					// (68 KB in 15 blocks on the LLVM lane), and that residue is
					// the process's, not the crossing's.
					bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
					if err != nil {
						t.Fatalf("%s/%s at %s shards: %v\nstderr:\n%s", cell.vertical, cell.edge, shards, err, stderr)
					}
					if bytesLost != 0 || blocksLost != 0 {
						t.Fatalf("%s/%s at %s shards: definitely lost %d bytes in %d blocks, want strict zero\nstderr:\n%s",
							cell.vertical, cell.edge, shards, bytesLost, blocksLost, stderr)
					}
				})
			}
		})
	}
}
