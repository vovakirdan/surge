package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// A far handle that crosses an `on` boundary has ONE owner, and the two
// controls RV2-DEBT-082 asked for show it from both sides.
//
// The anchored program: the caller keeps the handle it anchors a block on.
// The block reaches the channel through the lease the owner shard pins for
// its lifetime; the handle is neither moved into the block nor shipped in
// its state, a sibling minted from it is an independent lease, and the
// caller's scope exit is the one release of each. The plain move-in program:
// a handle captured into a non-anchored body MOVES -- the caller's binding
// ends at the crossing (a later use is SEM3130), the body owns the lease and
// its own scope exit gives it back.
//
// Both run under valgrind at strict zero. The mutant for the move-in half is
// the caller's binding left live after the capture (the old rule, sema not
// observing the move): the caller's scope exit then releases a lease the body
// already gave back, and valgrind reports the invalid free. The mutant for
// the anchored half is the lease check removed: `let held = ch;` inside the
// block compiles again, and TestAnchorLeaseMisuseIsRejected goes red.
const runtimeV2FarHandleAnchoredOwnerSource = `
async fn run() -> int {
    let ch: far Channel<int> = channel_on::<int>(shard(0:ShardId), 2);
    let sib: far Channel<int> = ch.share();
    let s1: TaskResult<nothing> = on ch { ch.send(41); ret nothing; };
    let _ = s1;
    let s2: TaskResult<nothing> = on sib { sib.send(1); ret nothing; };
    let _ = s2;
    let r1: TaskResult<int> = on ch {
        let v: Option<int> = ch.recv();
        ret compare v { Some(x) => x; nothing => 0 - 1; };
    };
    let a: int = compare r1 { Success(x) => x; Cancelled() => 0 - 2; };
    let r2: TaskResult<int> = on ch {
        let v: Option<int> = ch.recv();
        ret compare v { Some(x) => x; nothing => 0 - 1; };
    };
    let b: int = compare r2 { Success(x) => x; Cancelled() => 0 - 2; };
    if a + b == 42 {
        print("far-handle-anchored-owner-ok");
        return 0;
    }
    return 1;
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

const runtimeV2FarHandleMoveInSource = `
async fn run() -> int {
    let ch: far Channel<int> = channel_on::<int>(shard(0:ShardId), 2);
    let t: far Task<int> = spawn on distributed { let held = ch; ret 7; };
    let v: int = compare t.await() { Success(x) => x; Cancelled() => 0 - 2; };
    if v == 7 {
        print("far-handle-move-in-ok");
        return 0;
    }
    return 1;
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

func TestRuntimeV2FarHandleHasOneOwnerAcrossACrossing(t *testing.T) {
	baseEnv := envWithStdlib(repoRoot(t))
	for _, program := range []struct {
		name   string
		source string
		marker string
		shards []int
	}{
		{name: "anchored", source: runtimeV2FarHandleAnchoredOwnerSource, marker: "far-handle-anchored-owner-ok", shards: []int{1, 2}},
		{name: "move_in", source: runtimeV2FarHandleMoveInSource, marker: "far-handle-move-in-ok", shards: []int{1, 2}},
	} {
		t.Run(program.name, func(t *testing.T) {
			outputPath := buildRuntimeV2CrossingSource(t, program.source, nil)
			for _, shardCount := range program.shards {
				value := fmt.Sprintf("%d", shardCount)
				env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
				env = overrideEnvVar(env, "SURGE_THREADS", value)
				t.Run(fmt.Sprintf("correctness/shards_%d", shardCount), func(t *testing.T) {
					duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
					if result.exitCode != 0 || !strings.Contains(result.stdout, program.marker) {
						t.Fatalf(
							"program failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
							result.exitCode, duration, result.stdout, result.stderr,
						)
					}
				})
				t.Run(fmt.Sprintf("valgrind_strict_zero/shards_%d", shardCount), func(t *testing.T) {
					stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
					if hasValgrindMemcheckError(stderr) {
						t.Fatalf("valgrind reported a memcheck error (a lease released twice or after its holder freed the handle)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
					}
					if exitCode != 0 || !strings.Contains(stdout, program.marker) {
						t.Fatalf("program failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
					}
					bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
					if err != nil {
						t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
					}
					if bytesLost != 0 || blocksLost != 0 {
						t.Fatalf("definitely lost: %d bytes in %d blocks, want 0 in 0 (a lease outlived its one owner)\nstderr:\n%s", bytesLost, blocksLost, stderr)
					}
				})
			}
		})
	}
}
