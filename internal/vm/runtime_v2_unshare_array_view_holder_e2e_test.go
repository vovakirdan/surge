package vm_test

import "testing"

// The HOLDER routes to the same refusal: the value that crosses is owned and
// never sliced, and the VIEW is one of its elements or members. These are the
// shapes a sibling lane's reviewers measured, and they are what proves the
// element callback must be the element's real body wherever the element itself
// carries an array -- an `int[][]` whose outer buffer is fine and whose first
// slot is a view.
//
// A row here that passed on `ptr null` all the way down would mean the outer
// array was shown to the registry and the inner one never was, which is exactly
// the hole the aliasing came through.

// Row (iv): `xs.push(v)` on an `int[][]`. The outer array is owned; the view is
// its only element, reached by the per-element body the outer walk passes.
const runtimeV2UnshareIntArrayViewPushedSource = `
fn bump_nested(xss: own int[][]) -> int {
    let mut zs: int[][] = own xss;
    zs[0][0] = 1665;
    return zs[0][0];
}

async fn run() -> int {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    let mut xs: int[][] = [];
    xs.push(v);
    let job: Task<int> = blocking { ret bump_nested(own xs); };
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

// Row (v): the same holder filled by assigning into an existing SLOT rather
// than by pushing. A different writer, the same buffer underneath.
const runtimeV2UnshareIntArrayViewInASlotSource = `
fn bump_nested(xss: own int[][]) -> int {
    let mut zs: int[][] = own xss;
    zs[0][0] = 1665;
    return zs[0][0];
}

async fn run() -> int {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    let filler: int[] = [9, 9];
    let mut xs: int[][] = [filler];
    xs[0] = v;
    let job: Task<int> = blocking { ret bump_nested(own xs); };
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

// Row (vi): a TUPLE member. The array is at member 0 of an inline composite, so
// the walk reaches it through the composite's own body rather than through a
// buffer the runtime steps.
const runtimeV2UnshareIntArrayViewInATupleSource = `
fn bump_pair(p: own (int[], int)) -> int {
    let mut q: (int[], int) = own p;
    q.0[0] = 555;
    return q.0[0];
}

async fn run() -> int {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    let filler: int[] = [9, 9];
    let mut pair: (int[], int) = (filler, 7);
    pair.0 = v;
    let job: Task<int> = blocking { ret bump_pair(own pair); };
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

// Row (vii): the far-channel SEND arm of a select, which is the sink whose read
// mode had to widen with the gate: a borrowing read hands back a COPY operand
// for every type, and a COPY at a boundary is the aliasing bug by another name.
const runtimeV2UnshareIntArrayViewThroughAFarChannelSource = `
async fn take(ch: far Channel<int[]>) -> int {
    let seen: TaskResult<int> = on ch {
        let got: Option<int[]> = ch.recv();
        ret compare got {
            Some(ys) => { let mut zs: int[] = own ys; zs[0] = 777; ret zs[0]; };
            nothing => 0;
        };
    };
    return compare seen { Success(x) => x; Cancelled() => 9; };
}

async fn run() -> int {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    let ch: far Channel<int[]> = channel_on::<int[]>(shard(1:ShardId), 4);
    let won: int = select {
        ch.send(own v) => 1;
    };
    if won != 1 { print("FAIL won="); print(won to string); return 1; }
    let r: int = compare take(ch.share()).await() { Success(x) => x; Cancelled() => 9; };
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

func TestRuntimeV2UnshareOfAnIntArrayViewPushedIntoAHolderIsRefused(t *testing.T) {
	assertUnshareArrayRefusal(t, runtimeV2UnshareIntArrayViewPushedSource, runtimeV2ArrayViewRefusalText)
}

func TestRuntimeV2UnshareOfAnIntArrayViewPushedIntoAHolderNegativeControl(t *testing.T) {
	assertUnshareArrayAliasesWithoutTheCheck(t, runtimeV2UnshareIntArrayViewPushedSource, 16651665)
}

func TestRuntimeV2UnshareOfAnIntArrayViewInAHolderSlotIsRefused(t *testing.T) {
	assertUnshareArrayRefusal(t, runtimeV2UnshareIntArrayViewInASlotSource, runtimeV2ArrayViewRefusalText)
}

func TestRuntimeV2UnshareOfAnIntArrayViewInAHolderSlotNegativeControl(t *testing.T) {
	assertUnshareArrayAliasesWithoutTheCheck(t, runtimeV2UnshareIntArrayViewInASlotSource, 16651665)
}

func TestRuntimeV2UnshareOfAnIntArrayViewInATupleIsRefused(t *testing.T) {
	assertUnshareArrayRefusal(t, runtimeV2UnshareIntArrayViewInATupleSource, runtimeV2ArrayViewRefusalText)
}

func TestRuntimeV2UnshareOfAnIntArrayViewInATupleNegativeControl(t *testing.T) {
	assertUnshareArrayAliasesWithoutTheCheck(t, runtimeV2UnshareIntArrayViewInATupleSource, 5550555)
}

func TestRuntimeV2UnshareOfAnIntArrayViewThroughAFarChannelIsRefused(t *testing.T) {
	assertUnshareArrayRefusal(t, runtimeV2UnshareIntArrayViewThroughAFarChannelSource, runtimeV2ArrayViewRefusalText)
}

func TestRuntimeV2UnshareOfAnIntArrayViewThroughAFarChannelNegativeControl(t *testing.T) {
	assertUnshareArrayAliasesWithoutTheCheck(t, runtimeV2UnshareIntArrayViewThroughAFarChannelSource, 7770777)
}
