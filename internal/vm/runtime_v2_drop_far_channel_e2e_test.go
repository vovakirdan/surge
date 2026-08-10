package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Every `channel_on(...)`/`.share()` allocated a caller-side handle box
// (`rt_far_channel_handle_alloc`) that no generated code path ever freed
// (`rt_far_channel_release`/`rt_far_channel_handle_drop` existed in the
// runtime but had zero compiled callers), and the owner-side `rt_channel`
// object itself had no free path anywhere, for any channel, local or far
// (`rt_channel_new` is a bare rt_alloc with no matching rt_free in the
// whole runtime). Fixed: a new `isFarChannelType` leaf case in
// `emitInstrDrop` (internal/backend/llvm/emit_instr.go) routes a
// far-channel-typed binding's ordinary scope-exit drop to
// `rt_far_channel_handle_drop`; a new `rt_channel_free` (runtime/native/
// rt_async_channel.c) is called from `release_entry` (runtime/native/
// rt_far_channel.c) once the registry's existing active_leases/inflight
// predicate hits zero — the same choke point every reclaim path already
// routes through, and the sole owner of the channel object from the
// mint site on (rt_far_channel_dispatch_create).
//
// This program deliberately only creates and shares a far channel — it
// never sends or receives through it. `on ch { ... }` (the only surface
// for actual channel operations) exercises the immediate-on/anchored
// retry machinery, which carries a PRE-EXISTING, unrelated, separately
// tracked race (intermittent invalid-free/invalid-write under valgrind,
// reproduces on unmodified HEAD before this fix at a similar rate) that
// has nothing to do with this fix but would make a census built on
// top of it flaky. Handle+object lifecycle is fully exercised without
// touching that machinery: create, four independent share() leases
// (each spawned into its own task and consumed), and scope exit.
//
// Verified (manual, not part of this automated gate): the negative
// control (fix reverted) leaks exactly 120 bytes in 5 blocks on this
// program (1 channel_on + 4 share() handles, matching
// rt_far_task_handle's 24-byte size); with the fix, 0 bytes in 0 blocks
// across 10/10 runs, and 18/20 clean plus 2/20 hitting the unrelated
// race noted above on the fuller send/recv repro used during
// investigation (not this file's narrower, race-free program).
const runtimeV2DropFarChannelSource = `
async fn hold(ch: far Channel<int>) -> int {
    return 0;
}

async fn run() -> int {
    let ch: far Channel<int> = channel_on::<int>(shard(0:ShardId), 4);
    let mut k = 0;
    while k < 4 {
        let t: Task<int> = spawn hold(ch.share());
        let r: int = compare t.await() { Success(x) => x; Cancelled() => 0 - 1; };
        if r != 0 { return 1; }
        k = k + 1;
    }
    print("drop-far-channel-witness");
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

func TestRuntimeV2DropFarChannelHandleAndObjectValgrindZero(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2DropFarChannelSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, shardCount := range []string{"1", "2", "8"} {
		t.Run(fmt.Sprintf("shards_%s", shardCount), func(t *testing.T) {
			env := overrideEnvVar(baseEnv, "SURGE_SHARDS", shardCount)
			env = overrideEnvVar(env, "SURGE_THREADS", shardCount)
			stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 60*time.Second)
			if hasValgrindMemcheckError(stderr) {
				t.Fatalf("valgrind reported a real memcheck error (invalid free / use-after-free / uninitialized read)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			if exitCode != 0 {
				t.Fatalf("far-channel handle+object drop e2e failed (program exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
			}
			if !strings.Contains(stdout, "drop-far-channel-witness") {
				t.Fatalf("far-channel handle+object drop e2e missing completion marker; stdout=%q", stdout)
			}
			bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
			if err != nil {
				t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
			}
			if bytesLost != 0 || blocksLost != 0 {
				t.Fatalf(
					"far-channel handle+object leak regressed at shards=%s: got %d bytes in %d blocks, want strict zero\nstderr:\n%s",
					shardCount, bytesLost, blocksLost, stderr,
				)
			}
		})
	}
}
