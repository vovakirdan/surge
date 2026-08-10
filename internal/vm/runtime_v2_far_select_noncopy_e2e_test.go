package vm_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Remote select with a non-Copy SEND arm: the arm table ships once and the
// payload's ownership resolves exactly once per round — the winning send
// delivers into the owner-side buffer, a losing send reclaims through the
// per-arm payload_drop_fn_id keyed on select_committed_index.
//
// This row is the proof RV2-DEBT-064 blocked. The defect was DOUBLE
// OWNERSHIP on the normal completion path, not the retry re-evaluation that
// row originally described (see its ledger entry for the correction): the
// runtime dropped every non-committed SEND payload in
// rt_remote_task_pending_release's arm loop, while compiled code ALSO
// dropped the losing payloads through sema's per-arm drop synthesis in the
// winning arm's block. Both fired, in that order. The fix hands ownership
// back to the caller exactly when a winner reply reaches compiled code
// (select_finish_retry, runtime/native/rt_far_channel_select.c).
//
// The exit-code subtests below are NOT the real gate: glibc's tcache
// double-free detector is probabilistic, and the pre-fix binary passed them
// outright on some runs while valgrind still reported the invalid free on
// the very same binary. The valgrind subtest is what actually holds.
//
// Every send goes through `select`, never `on ch {...}`: the anchored
// send/recv surface sits in RV2-DEBT-061's neighborhood (a pre-existing,
// intermittent invalid-free under valgrind), and a rare hit there would be
// indistinguishable from a regression of this row's own subject.
// TestRuntimeV2DropFarChannelHandleAndObjectValgrindZero avoids it the same
// way and for the same reason.
//
// Winner determinism is structural, never timing: in every round exactly one
// arm is ready, forced by capacity and by which channel already holds a
// value. `c` exists only to give the filler round a permanently-not-ready
// second arm (an empty channel nothing ever sends to).
const runtimeV2FarSelectNonCopySource = `
fn build(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

async fn run() -> int {
    let a: far Channel<string> = channel_on::<string>(shard(0:ShardId), 1);
    let b: far Channel<int> = channel_on::<int>(shard(0:ShardId), 1);
    let c: far Channel<int> = channel_on::<int>(shard(0:ShardId), 1);

    // Round 1 — the SEND arm wins. b is empty, so b.recv() cannot proceed;
    // a is empty with capacity 1, so the send is the only ready arm. The
    // payload moves into a's owner-side buffer and must NOT be dropped by
    // the select: it is now the buffer's, reclaimed at channel teardown.
    let s1: string = build("w-");
    let v1: int = select { a.send(own s1) => 1; b.recv() => 2; };
    if v1 != 1 {
        print("FAIL winner-arm v1=");
        print(v1 to string);
        print("\n");
        return 11;
    }

    // Round 2 — filler, all-Copy. c is empty and nothing ever sends to it,
    // so c.recv() is permanently not ready; b.send(7) is the only ready arm.
    let v2: int = select { b.send(7) => 1; c.recv() => 2; };
    if v2 != 1 {
        print("FAIL filler-arm v2=");
        print(v2 to string);
        print("\n");
        return 12;
    }

    // Round 3 — the SEND arm LOSES. a is full (round 1's payload is still
    // buffered) so its send cannot proceed; b holds round 2's value so its
    // recv is the only ready arm. s3 is never delivered anywhere, so the
    // select's own free path owns it: exactly one drop, and specifically
    // NOT a drop of the committed arm.
    let s3: string = build("l-");
    let v3: int = select { a.send(own s3) => 1; b.recv() => 2; };
    if v3 != 2 {
        print("FAIL loser-arm v3=");
        print(v3 to string);
        print("\n");
        return 13;
    }

    print("far-select-noncopy-ok");
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

func TestRuntimeV2FarSelectNonCopySendArm(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2FarSelectNonCopySource, nil)
	baseEnv := envWithStdlib(repoRoot(t))

	for _, shardCount := range []int{1, 2, 8} {
		t.Run(fmt.Sprintf("shards_%d", shardCount), func(t *testing.T) {
			value := strconv.Itoa(shardCount)
			env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
			env = overrideEnvVar(env, "SURGE_THREADS", value)
			duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
			if result.exitCode != 0 {
				t.Fatalf(
					"non-copy far select failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode, duration, result.stdout, result.stderr,
				)
			}
			if strings.Contains(result.stdout, "FAIL") {
				t.Fatalf("printed a FAIL marker; stdout=%q", result.stdout)
			}
			if !strings.Contains(result.stdout, "far-select-noncopy-ok") {
				t.Fatalf("missing completion marker; stdout=%q", result.stdout)
			}
		})
	}

	// The pre-fix failure was a deterministic `free(): double free detected
	// in tcache 2` — a memcheck error, not a leak. Both halves of the
	// assertion matter: no invalid free (the payload is never dropped twice)
	// and no growth in definitely-lost (a retry's rebuilt payload is never
	// silently discarded).
	t.Run("valgrind", func(t *testing.T) {
		env := overrideEnvVar(baseEnv, "SURGE_SHARDS", "1")
		env = overrideEnvVar(env, "SURGE_THREADS", "1")
		stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 90*time.Second)
		if hasValgrindMemcheckError(stderr) {
			t.Fatalf("valgrind reported a memcheck error — the RV2-DEBT-064 signature is a double free of a SEND arm payload\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
		}
		if exitCode != 0 {
			t.Fatalf("non-copy far select failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
		}
		if !strings.Contains(stdout, "far-select-noncopy-ok") {
			t.Fatalf("missing completion marker under valgrind; stdout=%q", stdout)
		}
		bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
		if err != nil {
			t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
		}
		// STRICT ZERO, measured not assumed. Three far channels, three
		// selects, two heap payloads: one delivered into a buffer and
		// reclaimed at channel teardown, one abandoned as a losing arm and
		// reclaimed by the winning arm's compiled drop. Nothing here is
		// allowed a residual — this program never .share()s, so it avoids
		// the accumulating lease-struct bound the census rows carry.
		if bytesLost != 0 || blocksLost != 0 {
			t.Fatalf(
				"non-copy far select leaked %d bytes in %d blocks, want strict zero: either a losing SEND arm's payload was never reclaimed or a buffered winner outlived its channel\nstderr:\n%s",
				bytesLost, blocksLost, stderr,
			)
		}
	})
}

// A CONST arm operand is the one shape whose evaluation the crossing block
// repeats per retry round. Place operands — a call result, a binding, a
// lease-minting receiver — are temp'd into a preceding block by
// splitAsyncAwaits, so a resumed retry only re-loads them; a const is
// embedded in the crossing instruction itself and lowers inline, and
// several const kinds ALLOCATE there (an int/uint/float literal outside the
// fixnum inline range, a string const). This row pins the emitter's
// init/retry split, which is what keeps that evaluation to one.
//
// The literal MUST exceed the fixnum inline range: re-measured with a small
// literal the leak is 0 both ways, so an obvious-looking `send(7)` row would
// silently prove nothing.
const runtimeV2FarSelectConstArmSource = `
async fn run() -> int {
    let c: far Channel<int> = channel_on::<int>(shard(0:ShardId), 1);
    let d: far Channel<int> = channel_on::<int>(shard(0:ShardId), 1);
    // c is empty and nothing ever sends to it, so its recv is permanently
    // not ready and the const send arm is the only ready arm.
    let v: int = select {
        d.send(123456789012345678901234567890123456789012345678901234567890) => 1;
        c.recv() => 2;
    };
    if v != 1 {
        print("FAIL const-arm v=");
        print(v to string);
        print("\n");
        return 11;
    }
    print("far-select-const-arm-ok");
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

func TestRuntimeV2FarSelectConstArmEvaluatedOnce(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2FarSelectConstArmSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnvVar(baseEnv, "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")

	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 90*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a memcheck error\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("const-arm far select failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "far-select-const-arm-ok") {
		t.Fatalf("missing completion marker; stdout=%q", stdout)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	// ONE bigint, not two. The single remaining block is a SEPARATE,
	// pre-existing gap and deliberately not this row's subject: typeOwnsHeap
	// returns false for a bignum int, so the delivered payload gets drop-fn
	// id 0 and the channel's teardown drain never reclaims it. What this row
	// gates is the COUNT: without the init/retry split the crossing block
	// evaluates the literal again on the resumed retry and orphans that
	// second bigint, measured at 72 bytes in 2 blocks.
	const knownSingleEvaluationBytes = 36
	const knownSingleEvaluationBlocks = 1
	if bytesLost != knownSingleEvaluationBytes || blocksLost != knownSingleEvaluationBlocks {
		t.Fatalf(
			"const-arm far select leaked %d bytes in %d blocks, want exactly one un-reclaimed bigint (%d bytes in %d blocks); a doubling here means the crossing block evaluated the const operand on a retry round\nstderr:\n%s",
			bytesLost, blocksLost, knownSingleEvaluationBytes, knownSingleEvaluationBlocks, stderr,
		)
	}
}
