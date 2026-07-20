//go:build !golden

package vm_test

import (
	"strings"
	"testing"
	"time"
)

// RV2-DEBT-059's abandoned-state fix reclaims a suspend-point state box;
// this is the reply-edge/buffer-edge counterpart it does NOT cover: a far
// Channel<T>'s buffered/mailbox payload for a non-Copy T, previously
// blocked entirely by the ChannelCreate crossing guard. Now open: the
// runtime threads a payload_drop_fn_id from the channel_on::<T> crossing
// site through the buffer (rt_channel_free) and the parked-receiver
// mailbox (rt_channel_recv's cancellation path).
//
// A remote-select counterpart to this row (two SEND arms, winner delivered
// exactly once, loser reclaimed exactly once via the new
// select_committed_index/rt_far_channel_select_arm.payload_drop_fn_id
// mechanism) was attempted here and pulled: it hit a genuine, pre-existing,
// separate crossing-lowering bug (RV2-DEBT-064 — emitChannelSelectCrossing
// re-evaluates owned SEND-arm payload expressions on every retry, not just
// the true first attempt, a deterministic double-free once a payload is
// heap-carried). The select_committed_index tracking itself was verified
// correct by direct instrumentation before that root cause was found; see
// RV2-DEBT-064 for the full trace. Not this row's bug to fix.
//
// This row deliberately exercises `on ch {...}` (the only real send/recv
// surface for far channels), which sits in RV2-DEBT-061's neighborhood — a
// documented, pre-existing, unrelated intermittent race (see that debt row
// and TestRuntimeV2DropFarChannelHandleAndObjectValgrindZero's own note).
// A rare non-crash-class flake here is that debt row's, not this one's.

const runtimeV2FarChannelNonCopyRoundTripSource = `
fn build_payload() -> string {
    let mut v = "far-channel-";
    v = v + "noncopy-payload";
    return v;
}

async fn run() -> int {
    let ch: far Channel<string> = channel_on::<string>(shard(0:ShardId), 1);
    let s: TaskResult<nothing> = on ch { ch.send(own build_payload()); ret nothing; };
    let _ = s;
    // Some(_) => 1 (a wildcard, never binding the payload) sidesteps
    // RV2-DEBT-058: any compare-arm binding that is USED rather than moved
    // straight out (Success(x) => x) never reaches registerDroppableBinding
    // and leaks — a real, separate, pre-existing bug, confirmed independent
    // of far channels entirely by reproducing on a plain local
    // Channel<string> during this row's own investigation. Not this row's
    // bug to fix; content-blind wildcard verification plus valgrind's zero-
    // leak assertion is the correctness proof this row actually needs
    // (the reclaim paths, not string equality) — the anchored block's own
    // reply must stay plain-copy data anyway (a separate, still-active
    // gate), which already forces unwrapping inside the block before ret.
    let r: TaskResult<int> = on ch {
        let got: Option<string> = ch.recv();
        ret compare got { Some(_) => 1; nothing => 0; };
    };
    let v: int = compare r { Success(x) => x; Cancelled() => 0 - 2; };
    if v != 1 {
        print("FAIL unexpected recv outcome v=");
        print(v to string);
        return 1;
    }
    print("far-channel-noncopy-roundtrip-ok");
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

func TestRuntimeV2FarChannelNonCopyRoundTrip(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2FarChannelNonCopyRoundTripSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnvVar(baseEnv, "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")

	t.Run("correctness", func(t *testing.T) {
		duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
		if result.exitCode != 0 {
			t.Fatalf(
				"non-copy far-channel round trip failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
				result.exitCode, duration, result.stdout, result.stderr,
			)
		}
		if strings.Contains(result.stdout, "FAIL") {
			t.Fatalf("printed a FAIL marker; stdout=%q", result.stdout)
		}
		if !strings.Contains(result.stdout, "far-channel-noncopy-roundtrip-ok") {
			t.Fatalf("missing completion marker; stdout=%q", result.stdout)
		}
	})

	t.Run("valgrind_bounded", func(t *testing.T) {
		stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 60*time.Second)
		if hasValgrindMemcheckError(stderr) {
			t.Fatalf("valgrind reported a real memcheck error (see RV2-DEBT-061 if this is intermittent and non-reproducing)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
		}
		if exitCode != 0 {
			t.Fatalf("non-copy far-channel round trip failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
		}
		if !strings.Contains(stdout, "far-channel-noncopy-roundtrip-ok") {
			t.Fatalf("missing completion marker under valgrind; stdout=%q", stdout)
		}
		bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
		if err != nil {
			t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
		}
		// KNOWN RESIDUAL, pre-existing and unrelated to this row: a
		// channel_on'd far channel that is sent-to/received-from directly
		// (never .share()'d) leaks exactly 8 bytes in 1 block at scope exit
		// today — confirmed identical with a plain Copy `int` element on
		// the same shape (create + one on-ch send + on-ch recv, no
		// share()), so this is not a non-copy-payload issue this row's own
		// fixes are responsible for. TestRuntimeV2DropFarChannelHandleAndObjectValgrindZero
		// achieves true zero via create+share()+consume, a different shape.
		// This row asserts the payload itself never leaks (bytesLost stays
		// pinned to exactly this known baseline, not a growing number).
		const knownResidualBytes = 8
		const knownResidualBlocks = 1
		if bytesLost != knownResidualBytes || blocksLost != knownResidualBlocks {
			t.Fatalf(
				"non-copy far-channel round trip leaked %d bytes in %d blocks, want exactly the known pre-existing baseline (%d bytes in %d blocks); this looks like a NEW leak (the payload itself), not the documented residual\nstderr:\n%s",
				bytesLost, blocksLost, knownResidualBytes, knownResidualBlocks, stderr,
			)
		}
	})
}
