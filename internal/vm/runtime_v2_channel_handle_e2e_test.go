package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A local Channel<T> is reclaimed by its last release, and so is everything
// it still owns (RV2-DEBT-155).
//
// `Channel<T>` is a copyable handle whose object the runtime reference-counts:
// copying the handle retains, dropping a copy releases, the last release
// destroys the object and drops every payload still in its ring
// (docs/RUNTIME_V2.md section 7). Before this the channel was Copy and
// therefore, to every compiler leg, "nothing to reclaim": no obligation in
// sema, no drop in MIR, no release in the backend, and the block
// `rt_channel_new` made -- header, inline ring and park pool in one
// allocation -- was definitely lost once per channel, with the buffered
// payloads indirectly lost from it.
//
// Every row here is a whole program under valgrind with strict zero on BOTH
// figures, and every row prints a witness so that a program exiting before it
// built the channel cannot pass by having nothing to leak. The rows are the
// close condition of the debt row, one program each:
//
//   - an empty channel, so the object itself is what is measured;
//   - a channel holding two unreceived string payloads, so the ring drain is;
//   - a closed channel with a payload still in it, because `close` is not
//     `destroy` and the drain must still run;
//   - a channel referenced by a task that outlives the scope that made it, so
//     the reclaim is proven to wait for the last holder rather than to run at
//     the creator's scope exit.
//
// The fifth row is the scalar twin of the fourth. A reference-counted
// parameter of an `async fn` is borrowed at the call, so the task's initial
// frame takes a reference of its own -- and until the poll body owed that
// reference back (`paramIsRetainedIntoFrame`), a `float` handed to an
// `async fn` kept its block alive for the rest of the process. The channel
// rides the same leg, which is why the float row is pinned beside it.
func runChannelHandleValgrindRow(t *testing.T, source, marker string) {
	t.Helper()
	runChannelHandleValgrindRowWithResidue(t, source, marker, 0, 0)
}

// runChannelHandleValgrindRowWithResidue is the same row with a NAMED residue
// on the definitely-lost figure. Indirect loss stays strictly zero whatever the
// residue: a channel destroyed without draining its ring reports the payloads
// there, and nothing this file measures is allowed to hide behind that.
//
// A residue is an exact equality, never an upper bound. The point of writing a
// number down is that it changes loudly: a channel that outlives its last
// handle adds its own block (hundreds of bytes) and would move the figure in
// either direction, so a row that pins the residue still falsifies the question
// it is here for. Every residue this file allows must name what allocates it
// and what would make it zero.
func runChannelHandleValgrindRowWithResidue(t *testing.T, source, marker string, residueBytes, residueBlocks int) {
	t.Helper()
	outputPath := buildRuntimeV2CrossingSource(t, source, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 180*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a real memcheck error (invalid free / use-after-free / uninitialized read) -- a channel released once per copy without a retain per copy looks exactly like this\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("channel handle e2e failed (program exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, marker) {
		t.Fatalf("missing witness %q; stdout=%q", marker, stdout)
	}
	definiteBytes, definiteBlocks, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	indirectBytes, indirectBlocks := parseValgrindLeakMatch(valgrindIndirectLeakRE, stderr)
	if definiteBytes != residueBytes || definiteBlocks != residueBlocks || indirectBytes != 0 || indirectBlocks != 0 {
		t.Fatalf(
			"a channel outlived its last handle: %d bytes in %d blocks definitely lost (want %d bytes in %d blocks), %d bytes in %d blocks indirectly lost (want none)\nstderr:\n%s",
			definiteBytes, definiteBlocks, residueBytes, residueBlocks, indirectBytes, indirectBlocks, stderr,
		)
	}
}

// wideStringSource builds a string long enough to be its own heap block, so a
// payload the drain forgets shows up as an indirectly lost block rather than
// as nothing.
const channelHandleWideStringSource = `
fn wide(prefix: string) -> string {
    let mut s: string = prefix;
    let mut i: int = 0;
    while i < 8 {
        s = s + "0123456789";
        i = i + 1;
    }
    return s;
}
`

// The object alone: one channel of capacity four, one value through it and
// out again, nothing left inside when the binding goes out of scope.
const runtimeV2ChannelHandleEmptySource = `
@entrypoint
fn main() -> int {
    let ch = Channel::<int>::new(4:uint);
    ch.try_send(7);
    let got: Option<int> = ch.try_recv();
    let n: int = compare got { Some(v) => v; nothing => 0; };
    if n != 7 {
        print("FAIL channel round trip");
        return 1;
    }
    print("channel-empty-witness");
    return 0;
}
`

// The ring drain: two string payloads nobody receives. Each is a block of its
// own, reachable only from the channel's ring, so a destroy that frees the
// channel without draining reports them as indirectly lost.
const runtimeV2ChannelHandleBufferedSource = channelHandleWideStringSource + `
@entrypoint
fn main() -> int {
    let ch = Channel::<string>::new(4:uint);
    let a: string = wide("a");
    let b: string = wide("b");
    ch.try_send(own a);
    ch.try_send(own b);
    print("channel-buffered-witness");
    return 0;
}
`

// Closed with a payload still published into it. RUNTIME_V2 section 7: close
// forbids sends and does NOT discard the buffer, so the value is still the
// channel's to drop at destruction.
const runtimeV2ChannelHandleClosedSource = channelHandleWideStringSource + `
@entrypoint
fn main() -> int {
    let ch = Channel::<string>::new(4:uint);
    let a: string = wide("a");
    ch.try_send(own a);
    ch.close();
    print("channel-closed-witness");
    return 0;
}
`

// The channel is made in an inner scope, handed to a task by value, and the
// scope ends before the task runs. The task's frame holds the last handle: it
// takes one of the two payloads and leaves the other for the destroy that its
// own exit performs. A reclaim at the creator's scope exit would have freed
// the channel under the task; a missing release in the task leaks it.
const runtimeV2ChannelHandleOutlivesScopeSource = channelHandleWideStringSource + `
async fn take_one(ch: Channel<string>) -> int {
    checkpoint().await();
    let got: Option<string> = ch.try_recv();
    return compare got { Some(_) => 1; nothing => 0; };
}

async fn run() -> int {
    let mut tasks: Task<int>[] = [];
    {
        let ch = Channel::<string>::new(4:uint);
        let a: string = wide("a");
        let b: string = wide("b");
        ch.try_send(own a);
        ch.try_send(own b);
        tasks.push(spawn take_one(ch));
    }
    checkpoint().await();
    let mut taken: int = 0;
    while tasks.__len() > 0:uint {
        let t = tasks.pop().safe();
        taken = taken + compare t.await() { Success(v) => v; Cancelled() => 0 - 100; };
    }
    return taken;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    let taken: int = compare t.await() { Success(v) => v; Cancelled() => 0 - 100; };
    if taken != 1 {
        print("FAIL task did not take one payload");
        return 1;
    }
    print("channel-outlives-witness");
    return 0;
}
`

// The scalar twin: one float, three tasks that each borrow it into their
// frame across a suspension. The block is shared and counted, so a frame that
// never gives its reference back keeps the one block alive -- one block
// definitely lost, however many tasks.
const runtimeV2AsyncFloatParamSource = `
async fn hold(x: float) -> int {
    checkpoint().await();
    return 1;
}

async fn run() -> int {
    let f: float = 1.5;
    let mut tasks: Task<int>[] = [];
    let mut i: int = 0;
    while i < 3 {
        tasks.push(spawn hold(f));
        i = i + 1;
    }
    let mut total: int = 0;
    while tasks.__len() > 0:uint {
        let t = tasks.pop().safe();
        total = total + compare t.await() { Success(v) => v; Cancelled() => 0 - 100; };
    }
    return total;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    let total: int = compare t.await() { Success(v) => v; Cancelled() => 0 - 100; };
    if total != 3 {
        print("FAIL float param tasks");
        return 1;
    }
    print("float-param-witness");
    return 0;
}
`

func TestRuntimeV2ChannelHandleValgrindZero(t *testing.T) {
	rows := []struct {
		name, source, marker string
	}{
		{"empty", runtimeV2ChannelHandleEmptySource, "channel-empty-witness"},
		{"buffered", runtimeV2ChannelHandleBufferedSource, "channel-buffered-witness"},
		{"closed", runtimeV2ChannelHandleClosedSource, "channel-closed-witness"},
		{"outlives_scope", runtimeV2ChannelHandleOutlivesScopeSource, "channel-outlives-witness"},
		{"async_float_param", runtimeV2AsyncFloatParamSource, "float-param-witness"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			runChannelHandleValgrindRow(t, row.source, row.marker)
		})
	}
}

// The composite that holds a channel: `@copy type Mutex = { gate:
// Channel<nothing> }` from core/sync.sg, plus a Semaphore. Copying a Mutex
// clones the composite, and the clone RETAINS the channel (emitLeafCloneAt),
// so `m` and `m2` each hold a reference and each drop gives back exactly one;
// with the clone sharing the bare word the second drop freed the channel
// under the first copy. Eight lock/unlock rounds through both copies, and the
// runtime's own channel behind each primitive is reclaimed once at the end.
//
// The waiting task borrows its primitive (`mutex_lock_task(mtx: &Mutex)`), so
// a `lock()` copies nothing and touches no shared count -- RUNTIME_V2
// section 11's rule for request-path code, and the reason the lock cycle here
// is a valgrind row rather than a benchmark.
const runtimeV2MutexLockUnlockSource = `
async fn cycle(rounds: int) -> int {
    let m: Mutex = Mutex::new();
    let s: Semaphore = Semaphore::new(2:uint);
    let m2: Mutex = m;
    let mut i: int = 0;
    while i < rounds {
        let lock_task = m.lock();
        lock_task.await();
        m.unlock();
        let lock_again = m2.lock();
        lock_again.await();
        m2.unlock();
        s.acquire().await();
        s.release();
        i = i + 1;
    }
    return i;
}

async fn run() -> int {
    return compare cycle(8).await() { Success(v) => v; Cancelled() => 0 - 100; };
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    let done: int = compare t.await() { Success(v) => v; Cancelled() => 0 - 100; };
    if done != 8 {
        print("FAIL mutex rounds");
        return 1;
    }
    print("mutex-cycle-witness");
    return 0;
}
`

// The residue this row pins is NOT the channel's and does not belong to the
// handle axis at all. The LLVM backend materialises a shared `&T` parameter of
// an `async fn` into a heap box (`emitAsyncRefParamBox`), because the caller's
// stack frame may die before the task runs; the box is packed into the task
// frame and nothing ever frees it. Three such calls per round -- `m.lock()`,
// `m2.lock()`, `s.acquire()` -- times eight rounds is 24 blocks of one pointer
// each, and the same figure appears for a program with no channel in it at all
// (`async fn read_ref(x: &int)` called eight times leaks 64 bytes in 8 blocks
// on a tree with none of this change on it). It is pinned exactly rather than
// bounded so that a channel outliving its last handle -- hundreds of bytes in
// a block of its own -- still fails this row, and so that freeing the box makes
// this row fail until it is rewritten to strict zero.
func TestRuntimeV2MutexLockUnlockValgrindBounded(t *testing.T) {
	const asyncRefParamBoxBytes = 192
	const asyncRefParamBoxBlocks = 24
	runChannelHandleValgrindRowWithResidue(
		t, runtimeV2MutexLockUnlockSource, "mutex-cycle-witness",
		asyncRefParamBoxBytes, asyncRefParamBoxBlocks,
	)
}

// The VM lane's half of the same row. The VM already balanced the handle
// count on every copy and drop, but its executor kept the channel object --
// and the payloads in it -- until process exit, where `dropAsyncTasks` swept
// them up; a channel that went out of scope mid-program held its payloads for
// the rest of the run. The last handle release now destroys the channel
// (`channelHandleReleased` -> `ChanDestroy`) and drops what it held.
//
// A leak check at exit cannot see that, because the exit sweep hides it. The
// census can: the window makes one channel per iteration with two string
// payloads nobody receives, and the retained-per-iteration figure is what the
// exit sweep would otherwise have been reclaiming.
const runtimeV2VMChannelStrictZeroSource = channelHandleWideStringSource + `
async fn window(n: int) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut i: int = 0;
    while i < n {
        let ch = Channel::<string>::new(4:uint);
        let a: string = wide("a");
        let b: string = wide("b");
        ch.try_send(own a);
        ch.try_send(own b);
        i = i + 1;
    }
    let c1: HeapStats = rt_heap_stats();
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

func TestRuntimeV2VMChannelStrictZero(t *testing.T) {
	runTaskResultStrictZeroRow(t, runtimeV2VMChannelStrictZeroSource)
}
