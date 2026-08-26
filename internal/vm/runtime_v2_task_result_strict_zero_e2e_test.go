package vm_test

import "testing"

// RV2-DEBT-167: a task owns its result, and releases it when nothing can claim
// it any more.
//
// Before this, every delivery CLONED the value the task held (`taskResultValue`
// -> `cloneForShare`) and nothing ever released the task's own copy, so one
// heap object survived per completed task with a heap-bearing result. It was
// invisible at process exit because the shutdown drain swept it
// (`dropAsyncTasks`), which is precisely why an end-to-end program could not
// assert that it had left nothing behind -- the async machinery leaked
// underneath whatever the test was actually measuring, and the Wave C VM
// transport round-trip family had to be written at unit level against refcounts
// instead.
//
// Each row below measures the shape it names, not the runtime around it. Two
// windows run the same body at n=1 and n=9 and report objects allocated inside
// the window and never freed; everything paid once at a window edge -- the
// HeapStats structs themselves included -- is identical in both, so the
// DIFFERENCE divided by the eight extra iterations is what one operation
// retains. Zero is the whole claim.
//
// The verdict travels as the exit code because the VM lane runs the program
// in-process and does not capture its stdout. The codes are the COUNT, not a
// pass/fail flag, so a regression says how much it lost: on the unfixed tree at
// 0d00d9fa these rows report 11, 12 and 12 -- one object per await, and two per
// channel round trip, exactly the two figures the debt row recorded.
//
// VM-only, and the other lane cannot witness it: on the native backend an `int`
// result is an inline fixnum that never takes a block at all. The native twin
// of this same ownership gap is RV2-DEBT-242, where the result is a `string`
// and the loss is 19 bytes per delivery.

// requireVMLane skips the native lane WITHOUT consulting
// SURGE_SKIP_TIMEOUT_TESTS. These rows finish in well under a second and are
// meant to run inside `make check`, where that variable defaults to 1 -- going
// through requireVMBackend would make them vacuously green there, which is the
// failure mode Global Rule 13 names.
func requireVMLane(t *testing.T) {
	t.Helper()
	if backend := testBackend(t); backend != backendVM {
		t.Skipf("VM-only census row for %s=%s", backendEnvVar, backend)
	}
}

func runTaskResultStrictZeroRow(t *testing.T, source string) {
	t.Helper()
	requireVMLane(t)
	result := runProgramFromSource(t, source, runOptions{})
	if result.exitCode == 0 {
		return
	}
	t.Fatalf("strict-zero census failed: %s (exit=%d)\nstderr:\n%s",
		taskResultStrictZeroVerdict(result.exitCode), result.exitCode, result.stderr)
}

// taskResultStrictZeroVerdict turns the program's exit code back into the
// sentence it stands for, so a red row reads as a measurement.
func taskResultStrictZeroVerdict(code int) string {
	switch code {
	case 11:
		return "the operation retained ONE object per iteration"
	case 12:
		return "the operation retained TWO objects per iteration"
	case 19:
		return "the operation retained more than two objects per iteration"
	case 93, 94:
		return "a census window failed its own correctness check, so the census never ran"
	case 95:
		return "the nine-iteration window retained less than the one-iteration window"
	case 90:
		return "the driver task was cancelled"
	default:
		return "the program failed before reporting a census"
	}
}

// The barest shape the debt row names: spawn, await, no channel, no crossing,
// no composite payload. The awaited `int` is the only heap-bearing thing in
// sight, so a per-iteration residue can be nothing but the task result.
const runtimeV2TaskResultStrictZeroSpawnAwaitSource = `
async fn child(k: int) -> int {
    return k;
}

async fn window(n: int) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut i: int = 0;
    let mut acc: int = 0;
    while i < n {
        let t: Task<int> = spawn child(i);
        let v: int = compare t.await() { Success(x) => x; Cancelled() => 0 - 1; };
        if v != i { return 900001:uint; }
        acc = acc + v;
        i = i + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    if acc != (n * (n - 1)) / 2 { return 900002:uint; }
    return (c1.alloc_count - c0.alloc_count) - (c1.free_count - c0.free_count);
}

async fn run() -> int {
    let g1: uint = compare window(1).await() { Success(x) => x; Cancelled() => 900003:uint; };
    let g9: uint = compare window(9).await() { Success(x) => x; Cancelled() => 900004:uint; };
    if g1 >= 900000:uint { return 93; }
    if g9 >= 900000:uint { return 94; }
    if g9 < g1 { return 95; }
    let retained: uint = (g9 - g1) / 8:uint;
    if retained == 0:uint { return 0; }
    if retained == 1:uint { return 11; }
    if retained == 2:uint { return 12; }
    return 19;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() { Success(c) => c; Cancelled() => 90; };
}
`

// The debt row's second shape: a scalar channel round trip. The channel is
// built ONCE outside the window, so what is measured is the round trip and not
// the channel object; the two awaited tasks are why this one lost two.
const runtimeV2TaskResultStrictZeroChannelSource = `
async fn sender(ch: own Channel<int>, v: int) -> int {
    ch.send(v);
    return 0;
}

async fn receiver(ch: own Channel<int>) -> int {
    let got: Option<int> = ch.recv();
    return compare got { Some(x) => x; nothing => 0 - 1; };
}

async fn window(n: int) -> uint {
    let ch = Channel::<int>::new(1:uint);
    let c0: HeapStats = rt_heap_stats();
    let mut i: int = 0;
    let mut acc: int = 0;
    while i < n {
        let s_ch = ch;
        let r_ch = ch;
        let st: Task<int> = spawn sender(s_ch, 7);
        let rt: Task<int> = spawn receiver(r_ch);
        let a: int = compare st.await() { Success(x) => x; Cancelled() => 0 - 1; };
        let b: int = compare rt.await() { Success(x) => x; Cancelled() => 0 - 2; };
        if a != 0 { return 900001:uint; }
        if b != 7 { return 900002:uint; }
        acc = acc + b;
        i = i + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    if acc != 7 * n { return 900003:uint; }
    ch.close();
    return (c1.alloc_count - c0.alloc_count) - (c1.free_count - c0.free_count);
}

async fn run() -> int {
    let g1: uint = compare window(1).await() { Success(x) => x; Cancelled() => 900004:uint; };
    let g9: uint = compare window(9).await() { Success(x) => x; Cancelled() => 900005:uint; };
    if g1 >= 900000:uint { return 93; }
    if g9 >= 900000:uint { return 94; }
    if g9 < g1 { return 95; }
    let retained: uint = (g9 - g1) / 8:uint;
    if retained == 0:uint { return 0; }
    if retained == 1:uint { return 11; }
    if retained == 2:uint { return 12; }
    return 19;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() { Success(c) => c; Cancelled() => 90; };
}
`

// The transport round trip that had to be a unit test.
//
// TestTransportRoundTripKeepsOneOwner asserts the same property by reading
// refcounts out of the heap, because at the time no end-to-end program could
// say "and nothing was left over". This is that proof as a real program: a
// struct carrying a RUNTIME-BUILT string (an interned literal would have no
// refcount to lose) crosses from a producer task to a consumer task, the
// consumer checks the string ARRIVED INTACT rather than merely arrived, and
// the census says the crossing kept nothing. The unit test stays: it bounds
// exactly-once from both sides, which a census cannot do.
const runtimeV2TaskResultStrictZeroTransportSource = `
type Node = { id: int, label: string };

fn build_label(seed: string) -> string {
    let mut s = seed;
    let mut j: int = 0;
    while j < 3 {
        s = s + "x";
        j = j + 1;
    }
    return s;
}

async fn producer(ch: own Channel<Node>, k: int) -> int {
    let item: Node = Node { id = k, label = build_label("n") };
    ch.send(own item);
    return 0;
}

async fn consumer(ch: own Channel<Node>) -> int {
    let got: Option<Node> = ch.recv();
    let node: Node = compare got { Some(x) => x; nothing => Node { id = 0 - 1, label = "" }; };
    if node.label != "nxxx" { return 0 - 2; }
    return node.id;
}

async fn window(n: int) -> uint {
    let ch = Channel::<Node>::new(1:uint);
    let c0: HeapStats = rt_heap_stats();
    let mut i: int = 0;
    let mut acc: int = 0;
    while i < n {
        let p_ch = ch;
        let c_ch = ch;
        let pt: Task<int> = spawn producer(p_ch, i);
        let ct: Task<int> = spawn consumer(c_ch);
        let a: int = compare pt.await() { Success(x) => x; Cancelled() => 0 - 1; };
        let b: int = compare ct.await() { Success(x) => x; Cancelled() => 0 - 3; };
        if a != 0 { return 900001:uint; }
        if b != i { return 900002:uint; }
        acc = acc + b;
        i = i + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    if acc != (n * (n - 1)) / 2 { return 900003:uint; }
    ch.close();
    return (c1.alloc_count - c0.alloc_count) - (c1.free_count - c0.free_count);
}

async fn run() -> int {
    let g1: uint = compare window(1).await() { Success(x) => x; Cancelled() => 900004:uint; };
    let g9: uint = compare window(9).await() { Success(x) => x; Cancelled() => 900005:uint; };
    if g1 >= 900000:uint { return 93; }
    if g9 >= 900000:uint { return 94; }
    if g9 < g1 { return 95; }
    let retained: uint = (g9 - g1) / 8:uint;
    if retained == 0:uint { return 0; }
    if retained == 1:uint { return 11; }
    if retained == 2:uint { return 12; }
    return 19;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() { Success(c) => c; Cancelled() => 90; };
}
`

func TestRuntimeV2TaskResultStrictZeroSpawnAwait(t *testing.T) {
	runTaskResultStrictZeroRow(t, runtimeV2TaskResultStrictZeroSpawnAwaitSource)
}

func TestRuntimeV2TaskResultStrictZeroChannelRoundTrip(t *testing.T) {
	runTaskResultStrictZeroRow(t, runtimeV2TaskResultStrictZeroChannelSource)
}

func TestRuntimeV2TaskResultStrictZeroTransportRoundTrip(t *testing.T) {
	runTaskResultStrictZeroRow(t, runtimeV2TaskResultStrictZeroTransportSource)
}
