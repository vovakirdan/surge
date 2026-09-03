package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// A far crossing carries its payload at the payload's OWN width, and the
// width comes from the type id the crossing names, not from a machine word.
//
// Three payloads, none of them one word wide:
//
//   - a sixteen-byte composite through a far channel: the owner shard sizes
//     the cell by the element type's descriptor, and the second field either
//     arrives or it does not;
//   - a one-byte scalar through a far channel: narrower than a word, so a
//     word-sized move over-reads the sender's frame and over-writes the
//     receiver's;
//   - the same sixteen-byte composite as a far task's result: the reply is
//     moved out of the typed result slot at the result type's width.
//
// The mutant is the old emitter gate back: only a heap-owning payload named
// its type, everything else crossed as id 0 and was moved as one word. The
// two composite rows then read 40 and 30 instead of 42 -- deterministic, the
// second field never travels -- and the correctness subtest goes red without
// valgrind. The scalar row is what valgrind is for: the over-read and the
// over-write are in the frames, which memcheck cannot see, so the row asserts
// the value round-trips and that nothing is lost, and stands as the
// non-regression of the width once it is right.
const runtimeV2FarPayloadWidthSource = `
@copy
type Pair = { a: int, b: int };

fn wide_sum(p: Pair) -> int {
    return p.a + p.b;
}

async fn run() -> int {
    let ch: far Channel<Pair> = channel_on::<Pair>(shard(0:ShardId), 2);
    let s1: TaskResult<nothing> = on ch { ch.send(Pair { a: 40, b: 2 }); ret nothing; };
    let _ = s1;
    let r1: TaskResult<int> = on ch {
        let got: Option<Pair> = ch.recv();
        ret compare got { Some(p) => wide_sum(p); nothing => 0 - 1; };
    };
    let v1: int = compare r1 { Success(x) => x; Cancelled() => 0 - 2; };

    let bc: far Channel<int8> = channel_on::<int8>(shard(0:ShardId), 2);
    let s2: TaskResult<nothing> = on bc { bc.send(7:int8); ret nothing; };
    let _ = s2;
    let r2: TaskResult<int> = on bc {
        let got: Option<int8> = bc.recv();
        ret compare got { Some(n) => n to int; nothing => 0 - 1; };
    };
    let v2: int = compare r2 { Success(x) => x; Cancelled() => 0 - 2; };

    let t: far Task<Pair> = spawn on distributed { ret Pair { a: 30, b: 12 }; };
    let v3: int = compare t.await() { Success(p) => wide_sum(p); Cancelled() => 0 - 2; };

    if v1 == 42 {
        if v2 == 7 {
            if v3 == 42 {
                print("far-payload-width-ok");
                return 0;
            }
        }
    }
    print("FAIL v1=");
    print(v1 to string);
    print(" v2=");
    print(v2 to string);
    print(" v3=");
    print(v3 to string);
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

func TestRuntimeV2FarPayloadWidth(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2FarPayloadWidthSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))

	for _, shardCount := range []int{1, 2} {
		value := fmt.Sprintf("%d", shardCount)
		env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
		env = overrideEnvVar(env, "SURGE_THREADS", value)
		t.Run(fmt.Sprintf("correctness/shards_%d", shardCount), func(t *testing.T) {
			duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
			if result.exitCode != 0 || strings.Contains(result.stdout, "FAIL") {
				t.Fatalf(
					"far payload width round trip failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode, duration, result.stdout, result.stderr,
				)
			}
			if !strings.Contains(result.stdout, "far-payload-width-ok") {
				t.Fatalf("missing completion marker; stdout=%q", result.stdout)
			}
		})
	}

	t.Run("valgrind_strict_zero", func(t *testing.T) {
		env := overrideEnvVar(baseEnv, "SURGE_SHARDS", "1")
		env = overrideEnvVar(env, "SURGE_THREADS", "1")
		stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 90*time.Second)
		if hasValgrindMemcheckError(stderr) {
			t.Fatalf("valgrind reported a memcheck error\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
		}
		if exitCode != 0 || !strings.Contains(stdout, "far-payload-width-ok") {
			t.Fatalf("far payload width round trip failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
		}
		bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
		if err != nil {
			t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
		}
		if bytesLost != 0 || blocksLost != 0 {
			t.Fatalf("definitely lost: %d bytes in %d blocks, want 0 in 0\nstderr:\n%s", bytesLost, blocksLost, stderr)
		}
	})
}
