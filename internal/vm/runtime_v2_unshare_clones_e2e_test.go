package vm_test

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The relinquishing barrier, measured end to end.
//
// A reference-counted scalar is Copy: `own P{ v: a }` retains `a`'s block into
// the field, so the moved value and the caller's live `a` hold one block, and
// the count behind it is not atomic. Before the state ships, the compiler
// un-shares every counted leaf in the relinquishing operand: a block with one
// holder keeps its pointer, a block with two is duplicated and this value's
// reference to the original given up (rt_bigfloat_unshare). The clone branch
// is the only writer of `unshare_clones` on the TRACE_RESIDENT exit line, so
// that field counts exactly the blocks that HAD a sibling at a boundary.
//
// The program crosses four times, each with a sibling alive on the source
// side while the body runs -- one per operand shape the lowering handles:
//
//   - `spawn on shard(1)` MOVES `own P{ v: a }` out of a bare local and then
//     reads `a` again (the un-share sits on the local itself);
//   - `on shard(1)` captures a bare `f` by copy, a RETAIN whose binding stays
//     live by definition (the retain is materialized into a transfer temp);
//   - `on shard(1)` captures a `@copy C{ v: d }` by copy, a CopyValue of a
//     composite (the same temp, one level down);
//   - `blocking` moves `Q{ v: c }` while `c` stays live.
//
// So the exit line reads unshare_clones=4, on 2 shards and on 8; the same
// program with every block minted at its crossing reads 0; and the runtime
// built with RV2_BIGFLOAT_UNSHARE_NEGATIVE_CONTROL (the leaf is the identity)
// cannot report 4 -- that is the Rule-13 red. The values each body reads back
// are checked too, because a barrier that shipped the wrong block would count
// just as well.
const runtimeV2UnshareClonesSource = `
@shard_movable
type P = { v: float };

@copy
type C = { v: float };

type Q = { v: float };

fn weigh_p(p: own P) -> int {
    if p.v > 1.0 { return 1; }
    return 0;
}

fn weigh_c(c: C) -> int {
    if c.v > 4.0 { return 1; }
    return 0;
}

fn weigh_q(q: own Q) -> int {
    if q.v > 3.0 { return 1; }
    return 0;
}

async fn run() -> int {
    let a: float = 1.5;
    let p: own P = own P{ v: a };
    let task: far Task<int> = spawn on shard(1:ShardId) { ret weigh_p(own p); };
    let b: float = a;
    let v1: int = compare task.await() { Success(x) => x; Cancelled() => 0 - 2; };

    let f: float = 2.5;
    let reply: TaskResult<int> = on shard(1:ShardId) { let g: float = f; if g > 2.0 { ret 1; } ret 0; };
    let v2: int = compare reply { Success(x) => x; Cancelled() => 0 - 2; };

    let d: float = 4.5;
    let cv: C = C{ v: d };
    let reply2: TaskResult<int> = on shard(1:ShardId) { ret weigh_c(cv); };
    let v4: int = compare reply2 { Success(x) => x; Cancelled() => 0 - 2; };

    let c: float = 3.5;
    let q: Q = Q{ v: c };
    let job: Task<int> = blocking { ret weigh_q(own q); };
    let v3: int = compare job.await() { Success(x) => x; Cancelled() => 0 - 2; };

    if v1 == 1 {
        if v2 == 1 {
            if v3 == 1 {
                if v4 == 1 {
                    print("unshare-ok");
                    print((b + f + c + d + cv.v) to string);
                    return 0;
                }
            }
        }
    }
    print("FAIL v1=");
    print(v1 to string);
    print(" v2=");
    print(v2 to string);
    print(" v3=");
    print(v3 to string);
    print(" v4=");
    print(v4 to string);
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

// The same three crossings with every block minted at the literal: no
// sibling holds any of them at the boundary, so the un-share keeps every
// pointer and clones nothing. A Copy capture cannot be in this program -- its
// binding is the sibling -- so the middle crossing moves a struct too.
const runtimeV2UnshareNoSiblingSource = `
@shard_movable
type P = { v: float };

type Q = { v: float };

fn weigh_p(p: own P) -> int {
    if p.v > 1.0 { return 1; }
    return 0;
}

fn weigh_q(q: own Q) -> int {
    if q.v > 3.0 { return 1; }
    return 0;
}

async fn run() -> int {
    let p: own P = own P{ v: 1.5 };
    let task: far Task<int> = spawn on shard(1:ShardId) { ret weigh_p(own p); };
    let v1: int = compare task.await() { Success(x) => x; Cancelled() => 0 - 2; };

    let p2: own P = own P{ v: 2.5 };
    let reply: TaskResult<int> = on shard(1:ShardId) { ret weigh_p(own p2); };
    let v2: int = compare reply { Success(x) => x; Cancelled() => 0 - 2; };

    let q: Q = Q{ v: 3.5 };
    let job: Task<int> = blocking { ret weigh_q(own q); };
    let v3: int = compare job.await() { Success(x) => x; Cancelled() => 0 - 2; };

    if v1 == 1 {
        if v2 == 1 {
            if v3 == 1 {
                print("unshare-ok");
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

// runUnshareProgram runs a built program with the exec trace on and returns
// the exit line's fields; a program that failed is reported with its output.
func runUnshareProgram(t *testing.T, outputPath string, shards int) (map[string]uint64, string) {
	t.Helper()
	env := envWithStdlib(repoRoot(t))
	env = overrideEnvVar(env, "SURGE_SHARDS", strconv.Itoa(shards))
	env = overrideEnvVar(env, "SURGE_THREADS", strconv.Itoa(shards))
	env = overrideEnvVar(env, "SURGE_TRACE_EXEC", "1")
	duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
	if result.exitCode != 0 || !strings.Contains(result.stdout, "unshare-ok") {
		t.Fatalf("program failed (shards=%d exit=%d duration=%s)\nstdout:\n%s\nstderr:\n%s",
			shards, result.exitCode, duration, result.stdout, result.stderr)
	}
	values, line := runtimeV2ResidentTraceValues(t, result.stderr, "exit")
	return values, line
}

func unshareClonesField(t *testing.T, values map[string]uint64, line string) uint64 {
	t.Helper()
	got, ok := values["unshare_clones"]
	if !ok {
		t.Fatalf("TRACE_RESIDENT exit line has no unshare_clones field:\n%s", line)
	}
	return got
}

func TestRuntimeV2UnshareClonesCountTheSharedCaptures(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2UnshareClonesSource, nil)
	for _, shards := range []int{2, 8} {
		values, line := runUnshareProgram(t, outputPath, shards)
		t.Logf("shards=%d %s", shards, line)
		if clones := unshareClonesField(t, values, line); clones != 4 {
			t.Fatalf("shards=%d: unshare_clones = %d, want 4 (one shared block per crossing):\n%s",
				shards, clones, line)
		}
		if underflows := values["underflows"]; underflows != 0 {
			t.Fatalf("shards=%d: %d releases outran their acquires:\n%s", shards, underflows, line)
		}
	}
}

func TestRuntimeV2UnshareClonesAreZeroWithoutASibling(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2UnshareNoSiblingSource, nil)
	values, line := runUnshareProgram(t, outputPath, 2)
	t.Logf("%s", line)
	if clones := unshareClonesField(t, values, line); clones != 0 {
		t.Fatalf("unshare_clones = %d, want 0 (every block had one holder at its boundary):\n%s",
			clones, line)
	}
}

// The Rule-13 red: with the leaf compiled as the identity, the shared
// program's count is 0, not 4 -- the count row above would go red on this
// build, and no other writer reaches the field. The program is required to
// reach its exit line: the sharing it now performs is the body's drop and
// the caller's `let b = a` on one count from two threads, which loses an
// update at worst, and a crash here would be a finding to read, not an
// outcome to accept.
func TestRuntimeV2UnshareClonesNegativeControl(t *testing.T) {
	t.Setenv("SURGE_INTERNAL_RUNTIME_NEGATIVE_CONTROL", "RV2_BIGFLOAT_UNSHARE_NEGATIVE_CONTROL")
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2UnshareClonesSource, nil)
	values, line := runUnshareProgram(t, outputPath, 2)
	t.Logf("%s", line)
	if clones := unshareClonesField(t, values, line); clones != 0 {
		t.Fatalf("negative control: unshare_clones = %d with the leaf compiled as the identity; "+
			"the clone branch has another writer:\n%s", clones, line)
	}
}

// The shared program under valgrind: the un-share duplicates a block and gives
// up one reference, and nothing is lost or freed twice on either side.
func TestRuntimeV2UnshareCapturesLeakNothing(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2UnshareClonesSource, nil)
	requireOwnershipValgrind(t, exec.LookPath)
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
}

// A moved capture whose address a child task had borrowed lives in a RESIDENT
// field of the async frame after the split, not in a per-poll slot. The
// un-share was emitted on the bare local and walks the frame field's storage
// in place; one clone, because `a` still holds the block. Found by the
// refuter of 2026-09-06, when the post-split shape rule refused the field.
const runtimeV2UnshareResidentSource = `
@shard_movable
type P = { v: float };

fn weigh_p(p: own P) -> int {
    if p.v > 1.0 { return 1; }
    return 0;
}

async fn peek(p: &P) -> int { return 0; }

async fn run() -> int {
    let a: float = 1.5;
    let p: own P = own P{ v: a };
    let t: Task<int> = spawn peek(&p);
    let seen: int = compare t.await() { Success(x) => x; Cancelled() => 0 - 2; };
    let task: far Task<int> = spawn on shard(1:ShardId) { ret weigh_p(own p); };
    let b: float = a;
    let v1: int = compare task.await() { Success(x) => x; Cancelled() => 0 - 2; };
    if v1 == 1 {
        if seen == 0 {
            print("unshare-ok");
            print(b to string);
            return 0;
        }
    }
    print("FAIL v1=");
    print(v1 to string);
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

func TestRuntimeV2UnshareOfAResidentCapture(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2UnshareResidentSource, nil)
	values, line := runUnshareProgram(t, outputPath, 2)
	t.Logf("%s", line)
	if clones := unshareClonesField(t, values, line); clones != 1 {
		t.Fatalf("unshare_clones = %d, want 1 (the resident's block has a sibling holder):\n%s", clones, line)
	}
}
