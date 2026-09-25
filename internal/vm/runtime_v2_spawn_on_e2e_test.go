package vm_test

import (
	"testing"
	"time"
)

// N-TASK-27S end to end. copy_capture_runs: an `int` capture copied into a `spawn on shard(1)` body, whose far task is
// awaited; natively at two shards it exits 46 (the body does not suspend, so RV2-DEBT-344 is not reached); the VM
// refuses the build (FUT7015, FUT7016). hot_task_over_parameter_copy_refused: the review's q17, a task published hot
// through a plain fn that borrows the far body's copy of a parameter, in a caller that ends in `panic`; with this
// packet's transfer it would build and crash, and TC-XB refuses it at the body's `ret` on both backends.

const spawnOnA3PlainBodySource = `fn work(x: int) -> int {
    return x + 40;
}

async fn run() -> int {
    let n: int = 6;
    let ft = spawn on shard(1:ShardId) {
        ret work(n);
    };
    return compare ft.await() {
        Success(v) => v;
        Cancelled() => 101;
    };
}

@entrypoint
fn main() -> int {
    return compare run().await() {
        Success(n) => n;
        Cancelled() => 102;
    };
}
`

const spawnOnQ17UafSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    checkpoint().await();
    let n: int = *x;
    print("peek " + (n to string));
    return n;
}

fn keep(t: Task<int>) -> Task<int> {
    return spawn t;
}

fn mk(n: int) -> string {
    return "abcdefghij" + (n to string);
}

async fn run(s: int) -> nothing {
    let ft = spawn on distributed {
        let h = keep(peek(&s));
        ret 1;
    };
    let r = compare ft.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
    let mut i: int = 0;
    while i < 2000 {
        let junk: string = mk(i);
        checkpoint().await();
        i = i + 1;
    }
    panic("stop");
}

@entrypoint
fn main() -> int {
    return compare run(424242).await() {
        Success(_) => 0;
        Cancelled() => 1;
    };
}
`

const (
	spawnOnA3PlainBodySourceDigest = "0c610096cb36e2b05131c731f9242ba59a555518d77595b690fb608720486897"
	spawnOnQ17UafSourceDigest      = "ecb4539359e8324973ef1eab5e4aa886b3d5f95c99fcf2f24cb9c22d695614ec"
)

func TestRuntimeV2SpawnOn(t *testing.T) {
	t.Run("copy_capture_runs", func(t *testing.T) {
		codes, _ := crossingFrameCompile(t, "spawn_on_copy_capture", spawnOnA3PlainBodySource, spawnOnA3PlainBodySourceDigest)
		if testBackend(t) != backendLLVM {
			if codes != "FUT7015,FUT7016" {
				t.Fatalf("codes %q, want exactly FUT7015,FUT7016: the VM has no cross-shard transport", codes)
			}
			return
		}
		if codes != "" {
			t.Fatalf("codes %q, want none", codes)
		}
		skipTimeoutTests(t)
		outputPath := buildRuntimeV2CrossingSource(t, spawnOnA3PlainBodySource, nil)
		env := overrideEnvVar(overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "2"), "SURGE_THREADS", "2")
		_, res := runBinaryWithTimeout(t, outputPath, env, 60*time.Second)
		if res.exitCode != 46 {
			t.Fatalf("exit %d stdout %q stderr %q, want 46: the far body computed 6 + 40 from its copy", res.exitCode, res.stdout, res.stderr)
		}
	})
	t.Run("hot_task_over_parameter_copy_refused", func(t *testing.T) {
		codes, messages := crossingFrameCompile(t, "spawn_on_q17", spawnOnQ17UafSource, spawnOnQ17UafSourceDigest)
		for _, m := range messages {
			if m == "a task still borrows 's' at this ret" {
				return
			}
		}
		t.Fatalf("codes %q messages %q, want a SEM3021 refusal %q on the %s backend: the task outlives the far body's copy",
			codes, messages, "a task still borrows 's' at this ret", testBackend(t))
	})
}
