package vm_test

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// A `float` rides the reply of a crossing, end to end under the production
// capability. The producer's `ret` un-shares the result in its relinquishing
// operand (site 3) before the reply names it, and the asker moves it exactly
// once; the capture that fed the result was made private when it entered the
// state (site 1). Each program keeps a sibling holder alive so the un-shares
// have clones to make, prints the sibling after the crossing, and reads the
// result on the ASKER's shard — the shape the reply gate refused until step 5.
//
// The counts are per site, and every program here reads ONE: a capture read
// beside a live sibling is one clone at site 1, and the un-share of the result
// at site 3 finds a count of one every time — rewriteSpawnOnPollReturns and
// rewriteBlockingReturns drop the body's captures BEFORE the result's
// un-share, so a sibling the body kept (`let x = a; ret x`) is released first
// and the result leaves as the block's only holder. Site 3 is the belt for a
// holder that outlives the body's drops, which no body here has.
const runtimeV2ReplyFloatSource = `
async fn run() -> int {
    let a: float = 1.5;
    let t: far Task<float> = spawn on shard(1:ShardId) { ret a; };
    let got: float = compare t.await() { Success(x) => x; Cancelled() => 0.0; };
    if got < 1.0 { return 11; }
    print(got to string);
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

const runtimeV2ReplyFloatBesideSiblingSource = `
async fn run() -> int {
    let a: float = 1.5;
    let t: far Task<float> = spawn on shard(1:ShardId) { let x: float = a; ret x; };
    let got: float = compare t.await() { Success(x) => x; Cancelled() => 0.0; };
    if got < 1.0 { return 11; }
    print(got to string);
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

const runtimeV2ReplyFloatImmediateOnSource = `
async fn run() -> int {
    let a: float = 2.5;
    let r: TaskResult<float> = on shard(1:ShardId) { ret a; };
    let got: float = compare r { Success(x) => x; Cancelled() => 0.0; };
    if got < 2.0 { return 11; }
    print(got to string);
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

// A `blocking` body was never gated on the reply axis; its `ret` has carried
// the same un-share since step 4. The control that the narrowing changed
// nothing there: one clone for the capture, none for the derived result.
const runtimeV2ReplyFloatBlockingSource = `
async fn run() -> int {
    let a: float = 1.5;
    let job: Task<float> = blocking { let x: float = a; ret x; };
    let got: float = compare job.await() { Success(x) => x; Cancelled() => 0.0; };
    if got < 1.0 { return 11; }
    print(got to string);
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

// An anchored body that takes a float OUT of a remote channel's ring and
// returns it: the ring's reference is private (the far-select send un-shared
// it), the `Some(x)` payload moves out of the option, the body's `ret`
// un-shares a block with one holder, and the asker on the other shard takes
// it by a single move. One clone, the sender's (site 2, a sibling alive).
const runtimeV2ReplyFloatFromRingSource = `
async fn take(ch: far Channel<float>) -> float {
    let seen: TaskResult<float> = on ch {
        let v: Option<float> = ch.recv();
        ret compare v { Some(x) => x; nothing => 0.0; };
    };
    return compare seen { Success(x) => x; Cancelled() => 0.0; };
}

async fn run() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(1:ShardId), 4);
    let a: float = 1.5;
    let won: int = select {
        ch.send(a) => 1;
    };
    if won != 1 { return 11; }
    let got: float = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 0.0; };
    if got < 1.0 { return 12; }
    print(got to string);
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

func TestRuntimeV2UnshareClonesOnTheWayBackAsAReply(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		clones uint64
	}{
		{"spawn on returning its capture", runtimeV2ReplyFloatSource, 1},
		{"spawn on returning a value derived beside the capture", runtimeV2ReplyFloatBesideSiblingSource, 1},
		{"immediate on returning its capture", runtimeV2ReplyFloatImmediateOnSource, 1},
		{"blocking returning a value derived beside the capture", runtimeV2ReplyFloatBlockingSource, 1},
		{"anchored body returning a float taken out of the ring", runtimeV2ReplyFloatFromRingSource, 1},
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
func TestRuntimeV2UnshareClonesOnTheWayBackAsAReplyNegativeControl(t *testing.T) {
	t.Setenv("SURGE_INTERNAL_RUNTIME_NEGATIVE_CONTROL", "RV2_BIGFLOAT_UNSHARE_NEGATIVE_CONTROL")
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2ReplyFloatBesideSiblingSource, nil)
	values, line := runUnshareProgram(t, outputPath, 2)
	t.Logf("%s", line)
	if clones := unshareClonesField(t, values, line); clones != 0 {
		t.Fatalf("negative control: unshare_clones = %d with the leaf compiled as the identity:\n%s", clones, line)
	}
}

// The reply programs under valgrind: the result the asker takes is private,
// the producer's siblings stay with the producer, nothing is lost or freed
// twice on either shard.
func TestRuntimeV2UnshareReplyLeaksNothing(t *testing.T) {
	requireOwnershipValgrind(t, exec.LookPath)
	for name, src := range map[string]string{
		"spawn on returning its capture":                runtimeV2ReplyFloatSource,
		"spawn on returning a derived value":            runtimeV2ReplyFloatBesideSiblingSource,
		"immediate on returning its capture":            runtimeV2ReplyFloatImmediateOnSource,
		"blocking returning a derived value":            runtimeV2ReplyFloatBlockingSource,
		"anchored body returning a float from the ring": runtimeV2ReplyFloatFromRingSource,
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
