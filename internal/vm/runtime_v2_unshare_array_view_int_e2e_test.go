package vm_test

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// A view of an array whose ELEMENT holds no counted block, at a crossing.
//
// The companion file's rows are `float[]`: their elements are counted blocks,
// so the compiler already un-shared them at every crossing and the runtime
// already saw the header. An `int[]` shares no count, so before this lane the
// compiler emitted nothing for it -- no instruction in MIR, no walk body, no
// call -- and the runtime was never asked. Measured on this tree at the commit
// that landed the runtime half, each program below COMPILED and printed its
// alias: the worker thread wrote 777 (or 1665, or 555) through the view into
// the buffer the origin shard still owns and reads.
//
// Every crossing of a value that carries a dynamic array now reaches
// rt_array_unshare_walk, whatever the element holds, and the registry answers.
//
// Each red row is paired with a control that runs the SAME program with the
// registry check cut. The float rows next door use `unshare_clones` for that,
// which cannot serve here: an int element never reaches the counted scalar's
// clone branch, so the field stays 0 whether the walk ran or not. The control
// asserts THE ALIASING ITSELF instead -- the program survives and prints the
// value the remote thread wrote into the origin's buffer -- which is exactly
// the write the refusal prevents, and exactly what the base commit measured.

// aliasProbeMarker: each program prints one number, `remote * 10000 + base[1]`.
// Two prints of two values would let a row pass on a partial match; one number
// says both halves at once, and the second half is the origin shard's own
// storage read after the crossing.

// Row (i): THE DEFECT. A view given to a `blocking` capture. Sema sees an owned
// `int[]` and admits it; the callee writes 777 into slot 0 of the view, which
// is slot 1 of the base.
const runtimeV2UnshareIntArrayViewSource = `
fn bump(ys: own int[]) -> int {
    let mut zs: int[] = own ys;
    zs[0] = 777;
    return zs[0];
}

async fn run() -> int {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    let job: Task<int> = blocking { ret bump(own v); };
    let r: int = compare job.await() { Success(x) => x; Cancelled() => 0 - 2; };
    print("array-unshare-ok");
    print(((r * 10000) + base[1]) to string);
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`

// Row (ii): the immediate `on` twin. A bare `int[]` capture is refused by
// `error SEM3168 this owned value is not shard-movable`, so the view rides a
// @shard_movable field -- which also makes this the row whose array sits at a
// non-zero offset inside the moved value.
const runtimeV2UnshareIntArrayViewOnSource = `
@shard_movable
type Holder = { xs: int[] };

fn bump_holder(h: own Holder) -> int {
    let mut zs: int[] = own h.xs;
    zs[0] = 777;
    return zs[0];
}

async fn run() -> int {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    let h1: Holder = Holder{ xs: v };
    let reply: TaskResult<int> = on shard(1:ShardId) { ret bump_holder(own h1); };
    let r: int = compare reply { Success(x) => x; Cancelled() => 0 - 2; };
    print("array-unshare-ok");
    print(((r * 10000) + base[1]) to string);
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`

// Row (iii): the `spawn on` twin of the row above, awaited as a far Task. The
// same holder, the fourth relinquishing sink.
const runtimeV2UnshareIntArrayViewSpawnOnSource = `
@shard_movable
type Holder = { xs: int[] };

fn bump_holder(h: own Holder) -> int {
    let mut zs: int[] = own h.xs;
    zs[0] = 777;
    return zs[0];
}

async fn run() -> int {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    let h1: Holder = Holder{ xs: v };
    let job: far Task<int> = spawn on shard(1:ShardId) { ret bump_holder(own h1); };
    let r: int = compare job.await() { Success(x) => x; Cancelled() => 0 - 2; };
    print("array-unshare-ok");
    print(((r * 10000) + base[1]) to string);
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`

// runUnshareArrayProgramForItsAlias runs a program that is expected to SURVIVE
// -- the negative control's build -- and hands back what it printed.
func runUnshareArrayProgramForItsAlias(t *testing.T, outputPath string, shards int) string {
	t.Helper()
	env := envWithStdlib(repoRoot(t))
	env = overrideEnvVar(env, "SURGE_SHARDS", strconv.Itoa(shards))
	env = overrideEnvVar(env, "SURGE_THREADS", strconv.Itoa(shards))
	duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
	if result.exitCode != 0 {
		t.Fatalf("the control build must run to completion (shards=%d exit=%d duration=%s)\nstdout:\n%s\nstderr:\n%s",
			shards, result.exitCode, duration, result.stdout, result.stderr)
	}
	return result.stdout
}

// assertUnshareArrayAliasesWithoutTheCheck is the Rule-13 red for a refusal row
// above: with the registry check cut, the same program crosses, the remote
// thread writes through the view, and the origin shard reads the write back out
// of its own base. wantCode is `remote * 10000 + base[1]`, so a row cannot pass
// on the remote half alone.
func assertUnshareArrayAliasesWithoutTheCheck(t *testing.T, source string, wantCode int) {
	t.Helper()
	t.Setenv("SURGE_INTERNAL_RUNTIME_NEGATIVE_CONTROL", "RV2_ARRAY_UNSHARE_WALK_NEGATIVE_CONTROL")
	outputPath := buildRuntimeV2CrossingSource(t, source, nil)
	stdout := runUnshareArrayProgramForItsAlias(t, outputPath, 2)
	want := strconv.Itoa(wantCode)
	if !strings.Contains(stdout, want) {
		t.Fatalf("negative control: stdout does not carry %s (remote*10000 + base[1]); "+
			"without the write-through the refusal prevents, the rows above pin nothing\nstdout:\n%s", want, stdout)
	}
}

func TestRuntimeV2UnshareOfAnIntArrayViewIsRefusedByTheRuntime(t *testing.T) {
	assertUnshareArrayRefusal(t, runtimeV2UnshareIntArrayViewSource, runtimeV2ArrayViewRefusalText)
}

func TestRuntimeV2UnshareOfAnIntArrayViewNegativeControl(t *testing.T) {
	assertUnshareArrayAliasesWithoutTheCheck(t, runtimeV2UnshareIntArrayViewSource, 7770777)
}

func TestRuntimeV2UnshareOfAnIntArrayViewInAnOnHolderIsRefused(t *testing.T) {
	assertUnshareArrayRefusal(t, runtimeV2UnshareIntArrayViewOnSource, runtimeV2ArrayViewRefusalText)
}

func TestRuntimeV2UnshareOfAnIntArrayViewInAnOnHolderNegativeControl(t *testing.T) {
	assertUnshareArrayAliasesWithoutTheCheck(t, runtimeV2UnshareIntArrayViewOnSource, 7770777)
}

func TestRuntimeV2UnshareOfAnIntArrayViewInASpawnOnHolderIsRefused(t *testing.T) {
	assertUnshareArrayRefusal(t, runtimeV2UnshareIntArrayViewSpawnOnSource, runtimeV2ArrayViewRefusalText)
}

func TestRuntimeV2UnshareOfAnIntArrayViewInASpawnOnHolderNegativeControl(t *testing.T) {
	assertUnshareArrayAliasesWithoutTheCheck(t, runtimeV2UnshareIntArrayViewSpawnOnSource, 7770777)
}
