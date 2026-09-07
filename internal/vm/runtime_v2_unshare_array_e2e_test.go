package vm_test

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The buffer walk of a dynamic array, measured end to end.
//
// A `float` is a heap block behind a NON-ATOMIC count. Before a value crosses
// a shard boundary the compiler un-shares every counted leaf inside it: a
// block only the mover holds travels as it is, a block a sibling on this shard
// still holds is duplicated and this value's reference to the original given
// up. For a `float[]` the leaf is not reachable from the type alone -- the
// elements live in a buffer whose length is a run-time fact -- so the emitter
// hands the slot to rt_array_unshare_walk, which steps the buffer and runs the
// element's un-share on every slot. The clone branch of the scalar un-share is
// the ONLY writer of `unshare_clones` on the TRACE_RESIDENT exit line, so that
// field counts exactly the blocks the barrier had to duplicate, and a walk
// that never happened cannot put a number there.
//
// The rows below pin those numbers rather than the shape of the emitted IR: an
// IR test can be satisfied by a call that the run never makes, and the whole
// point of a run-time iterator is that its trip count is decided at run time.
//
// The marker every program prints is `array-unshare-ok`, deliberately a
// SUPERSTRING of the `unshare-ok` that the shared runUnshareProgram helper
// looks for -- shortening it would make that helper fatal for a reason no
// reader of this file would connect to the edit.
//
// The runtime refuses two shapes, and BOTH are reachable from a plain Surge
// program: a VIEW handed to a crossing, and a BASE that crosses while one of
// its views is still alive. Their rows live in the companion file beside this
// one. Sema refuses only the spelling that leaves the slice in a local the
// mover can see -- measured on this tree, `let v: float[] = xs[[0..2]];`
// followed by `blocking { ret total3(own xs); }` answers
// `error SEM3020 cannot move 'xs' while it is shared-borrowed` -- and that
// borrow ends with the frame that took it, so a slice handed back by a callee
// or carried out in a struct field draws no diagnostic at all and reaches the
// walk.

// Row (a): one program carrying three of the FOUR crossing sinks the walk now
// serves. The fourth is `spawn on`, whose capture must be a @shard_movable
// composite and so cannot be a bare `float[]`; it has a row of its own below,
// and that row is also the only one whose array sits at a NON-ZERO offset
// inside the value being moved. Each sink here is measured alone in a probe of
// its own, and the total below is their sum -- write the decomposition down,
// because a future red on 6 has to say WHICH sink moved:
//
//	3  a `blocking` capture of `[a, a, a]` while `a` is still held;
//	3  a far `Channel<float[]>` filled by a select SEND arm and read back
//	   inside an anchored body on the other shard;
//	0  a `blocking` RESULT, whose elements are literals nothing else holds.
//
// Every sum is checked exactly inside the program, on both sides of every
// crossing, because a barrier that shipped the wrong block would count just as
// well as one that shipped the right one.
const runtimeV2UnshareArraySinksSource = `
fn total3(xs: own float[]) -> float {
    return xs[0] + xs[1] + xs[2];
}

async fn take(ch: far Channel<float[]>) -> int {
    let seen: TaskResult<int> = on ch {
        let v: Option<float[]> = ch.recv();
        ret compare v {
            Some(ys) => (ys[0] + ys[1] + ys[2]) == 4.5 ? 1 : 2;
            nothing => 0;
        };
    };
    return compare seen { Success(x) => x; Cancelled() => 9; };
}

async fn run() -> int {
    let a: float = 1.5;

    let xs: float[] = [a, a, a];
    let job: Task<float> = blocking { ret total3(own xs); };
    let s1: float = compare job.await() { Success(v) => v; Cancelled() => 0.0; };
    if s1 != 4.5 { return 11; }

    let ch: far Channel<float[]> = channel_on::<float[]>(shard(1:ShardId), 4);
    let ys: float[] = [a, a, a];
    let won: int = select {
        ch.send(own ys) => 1;
    };
    if won != 1 { return 12; }
    let got: int = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 9; };
    if got != 1 { return 13; }

    let job2: Task<float[]> = blocking { let zs: float[] = [1.5, 2.5]; ret zs; };
    let n: int = compare job2.await() {
        Success(zs) => ((zs.__len() to int) * 10) + ((zs[0] + zs[1]) to int);
        Cancelled() => 0 - 2;
    };
    if n != 24 { return 14; }

    if a == 1.5 {
        print("array-unshare-ok");
        print(s1 to string);
        return 0;
    }
    print("FAIL a=");
    print(a to string);
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

// Row (b): the same three sinks with every element minted at its own literal
// and no `a` binding anywhere. Nothing on this shard holds a block when the
// buffer is walked, so the walk keeps every pointer and clones nothing. This
// is the control that says the number above is SIBLINGS and not a per-element
// cost the walk charges for showing up.
const runtimeV2UnshareArrayNoSiblingSource = `
fn total3(xs: own float[]) -> float {
    return xs[0] + xs[1] + xs[2];
}

async fn take(ch: far Channel<float[]>) -> int {
    let seen: TaskResult<int> = on ch {
        let v: Option<float[]> = ch.recv();
        ret compare v {
            Some(ys) => (ys[0] + ys[1] + ys[2]) == 4.5 ? 1 : 2;
            nothing => 0;
        };
    };
    return compare seen { Success(x) => x; Cancelled() => 9; };
}

async fn run() -> int {
    let xs: float[] = [1.5, 1.5, 1.5];
    let job: Task<float> = blocking { ret total3(own xs); };
    let s1: float = compare job.await() { Success(v) => v; Cancelled() => 0.0; };
    if s1 != 4.5 { return 11; }

    let ch: far Channel<float[]> = channel_on::<float[]>(shard(1:ShardId), 4);
    let ys: float[] = [1.5, 1.5, 1.5];
    let won: int = select {
        ch.send(own ys) => 1;
    };
    if won != 1 { return 12; }
    let got: int = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 9; };
    if got != 1 { return 13; }

    let job2: Task<float[]> = blocking { let zs: float[] = [1.5, 2.5]; ret zs; };
    let n: int = compare job2.await() {
        Success(zs) => ((zs.__len() to int) * 10) + ((zs[0] + zs[1]) to int);
        Cancelled() => 0 - 2;
    };
    if n != 24 { return 14; }

    print("array-unshare-ok");
    print(s1 to string);
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

// Row (d-i): an array of arrays. The outer walk reaches the inner buffers
// through the element's own un-share body, and the sum on the far side proves
// the leaves that arrived are the values that left. Written as two named rows
// because `[[a, a], [a]]` inline is a type error -- rows of different length
// infer as different fixed arrays.
const runtimeV2UnshareNestedArraySource = `
fn total_nested(xs: own float[][]) -> float {
    return xs[0][0] + xs[0][1] + xs[1][0];
}

async fn run() -> int {
    let a: float = 1.5;
    let r1: float[] = [a, a];
    let r2: float[] = [a];
    let xs: float[][] = [r1, r2];
    let job: Task<float> = blocking { ret total_nested(own xs); };
    let s: float = compare job.await() { Success(v) => v; Cancelled() => 0.0; };
    if s == 4.5 {
        if a == 1.5 {
            print("array-unshare-ok");
            print(s to string);
            return 0;
        }
    }
    print("FAIL s=");
    print(s to string);
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

// Row (d-ii): an array whose element is a `@copy` struct, PADDED so that its
// width is not a float's. That padding is the row, not decoration. The walk
// steps the buffer by a stride the emitter computes from the element type; a
// one-float struct is eight bytes wide, exactly what a bare `float[]` uses, so
// an element like that cannot tell a right stride from a wrong one. This one
// is 32 bytes wide (measured in the emitted IR: `i64 32` here against `i64 8`
// for a `float[]`), and two of its four fields are counted -- so a walk that
// stepped by a counted scalar's width instead would read element boundaries
// that are not there.
const runtimeV2UnshareCopyStructArraySource = `
@copy
type Pad = { v: float, w: float, b: bool, n: int };

fn total_pads(ps: own Pad[]) -> float {
    return ps[0].v + ps[0].w + ps[1].v + ps[1].w + ps[2].v + ps[2].w;
}

async fn run() -> int {
    let a: float = 1.5;
    let ps: Pad[] = [
        Pad{ v: a, w: a, b: true, n: 1 },
        Pad{ v: a, w: a, b: false, n: 2 },
        Pad{ v: a, w: a, b: true, n: 3 },
    ];
    let job: Task<float> = blocking { ret total_pads(own ps); };
    let s: float = compare job.await() { Success(v) => v; Cancelled() => 0.0; };
    if s == 9.0 {
        if a == 1.5 {
            print("array-unshare-ok");
            print(s to string);
            return 0;
        }
    }
    print("FAIL s=");
    print(s to string);
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

// Row (d-iii): the fourth crossing sink, and the only shape here whose array
// is not the whole value being moved. `spawn on` publishes a @shard_movable
// composite to another shard, and the array is that composite's SECOND field,
// so the emitter has to step over the first one before handing the slot to the
// walk. Written `{ mark: int, xs: float[] }` on purpose: with the array first
// the offset is zero and a walk handed the wrong member would still find the
// right buffer.
const runtimeV2UnshareSpawnOnArrayFieldSource = `
@shard_movable
type Payload = { mark: int, xs: float[] };

fn total3(p: own Payload) -> float {
    return p.xs[0] + p.xs[1] + p.xs[2];
}

async fn run() -> int {
    let a: float = 1.5;
    let p: Payload = Payload{ mark: 7, xs: [a, a, a] };
    let job: far Task<float> = spawn on shard(1:ShardId) { ret total3(own p); };
    let s: float = compare job.await() { Success(v) => v; Cancelled() => 0.0; };
    if s == 4.5 {
        if a == 1.5 {
            print("array-unshare-ok");
            print(s to string);
            return 0;
        }
    }
    print("FAIL s=");
    print(s to string);
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

// assertUnshareArrayClones builds one program, runs it at each shard count and
// requires the exact clone total and a clean release ledger.
func assertUnshareArrayClones(t *testing.T, source string, want uint64, why string) {
	t.Helper()
	outputPath := buildRuntimeV2CrossingSource(t, source, nil)
	for _, shards := range []int{2, 8} {
		values, line := runUnshareProgram(t, outputPath, shards)
		t.Logf("shards=%d %s", shards, line)
		if clones := unshareClonesField(t, values, line); clones != want {
			t.Fatalf("shards=%d: unshare_clones = %d, want %d (%s):\n%s", shards, clones, want, why, line)
		}
		if underflows := values["underflows"]; underflows != 0 {
			t.Fatalf("shards=%d: %d releases outran their acquires:\n%s", shards, underflows, line)
		}
	}
}

func TestRuntimeV2UnshareWalksAFloatArrayBufferAtEveryCrossingSink(t *testing.T) {
	assertUnshareArrayClones(t, runtimeV2UnshareArraySinksSource, 6,
		"three elements at the blocking capture, three at the far channel's send arm, "+
			"none at the blocking result whose elements are literals")
}

func TestRuntimeV2UnshareOfAFloatArrayClonesNothingWithoutASibling(t *testing.T) {
	assertUnshareArrayClones(t, runtimeV2UnshareArrayNoSiblingSource, 0,
		"every element block had exactly one holder when its buffer was walked")
}

// The Rule-13 red for the rows above. With the leaf's clone branch compiled as
// the identity the walk still runs, every crossing still happens and the
// program still prints its marker -- so a row that asserted only the marker
// would be green here, on a build where the barrier duplicates nothing and two
// shards share one non-atomic count. The NUMBER is the row.
func TestRuntimeV2UnshareOfAFloatArrayNegativeControl(t *testing.T) {
	t.Setenv("SURGE_INTERNAL_RUNTIME_NEGATIVE_CONTROL", "RV2_BIGFLOAT_UNSHARE_NEGATIVE_CONTROL")
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2UnshareArraySinksSource, nil)
	values, line := runUnshareProgram(t, outputPath, 2)
	t.Logf("%s", line)
	if clones := unshareClonesField(t, values, line); clones != 0 {
		t.Fatalf("negative control: unshare_clones = %d with the leaf compiled as the identity; "+
			"the clone branch has another writer:\n%s", clones, line)
	}
}

func TestRuntimeV2UnshareWalksAnArrayOfArraysToItsFloatLeaves(t *testing.T) {
	assertUnshareArrayClones(t, runtimeV2UnshareNestedArraySource, 3,
		"two leaves in the first row and one in the second, all held by the caller's `a`")
}

func TestRuntimeV2UnshareWalksAnArrayOfPaddedCopyStructsToTheirFloatFields(t *testing.T) {
	assertUnshareArrayClones(t, runtimeV2UnshareCopyStructArraySource, 6,
		"two counted fields in each of three elements, every one of them still held by the caller's `a`")
}

func TestRuntimeV2UnshareWalksAnArrayFieldMovedBySpawnOn(t *testing.T) {
	assertUnshareArrayClones(t, runtimeV2UnshareSpawnOnArrayFieldSource, 3,
		"three elements of the composite's array field, each still held by the caller's `a`")
}

// The three-sink program under valgrind: the walk duplicates a block and gives
// up one reference per element, and nothing is lost or freed twice on either
// shard.
//
// An array whose element is a UNION is deliberately not in this row, and the
// reason is a measurement, not a hunch. A purely local program -- no crossing
// anywhere in it -- that builds `let xs: Option<float>[] = [Some(1.5),
// Some(2.5), Some(3.5)];` and reads the three elements back through a
// reference loses 72 bytes in 3 blocks, one per populated arm, while the same
// program with a plain `float[]` loses nothing. So the loss belongs to the
// union payload's own reclamation, the same defect the far-channel family
// already keeps out of its valgrind row, and a union element here would
// measure that rather than this barrier. (A mixed literal is not the spelling
// to reach for: `[Some(a), nothing, Some(a)]` is refused by
// `error SEM3015 array elements must have the same type`.)
func TestRuntimeV2UnshareOfAFloatArrayLeaksNothing(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2UnshareArraySinksSource, nil)
	requireOwnershipValgrind(t, exec.LookPath)
	for _, shards := range []string{"2", "8"} {
		env := envWithStdlib(repoRoot(t))
		env = overrideEnvVar(env, "SURGE_SHARDS", shards)
		env = overrideEnvVar(env, "SURGE_THREADS", shards)
		stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 180*time.Second)
		if exitCode != 0 || !strings.Contains(stdout, "array-unshare-ok") {
			t.Fatalf("program failed under valgrind (shards=%s exit=%d)\nstdout:\n%s\nstderr:\n%s",
				shards, exitCode, stdout, stderr)
		}
		lostBytes, lostBlocks, err := parseValgrindDefinitelyLost(stderr)
		if err != nil {
			t.Fatalf("could not read the valgrind leak summary: %v\nstderr:\n%s", err, stderr)
		}
		if hasValgrindMemcheckError(stderr) || lostBytes != 0 || lostBlocks != 0 {
			t.Fatalf("memory gate (shards=%s): memcheck_error=%t definitely_lost=%d bytes/%d blocks, want none\nstderr:\n%s",
				shards, hasValgrindMemcheckError(stderr), lostBytes, lostBlocks, stderr)
		}
	}
}
