package vm_test

import "testing"

// `timeout(t, ms)` is an ASKER that owns no handle.
//
// Every other consumer of a task's result reaches it through a `Task<T>` word,
// and RV2-DEBT-167's cohort counts those words: one `OKResource` object is one
// entitlement, and when the last one dies the task releases its result. The
// timeout operation does not fit that shape twice over. It builds a runtime
// task of its own -- `SpawnTimeout` -- that no source name ever holds, so that
// task has no cohort and its result is nobody's to release; and it consumes the
// TARGET's result from inside a poll, at a moment when the target's own cohort
// may already have emptied.
//
// Registering a handle for the runtime task is the obvious move and it is a
// recorded dead end: RV2-DEBT-167's lane tried it and it detonated at once
// (`VM1003 expected nothing, got bigint`), because the delivered value shares
// the storage the release would take. The model's answer is not another handle
// but the other state the model already has -- a CLAIM, held by an operation
// that has committed to consuming a result and not yet done so.
//
// The row measures the timeout operation the way RV2-DEBT-167 measures the
// others: two windows, n=1 and n=9, differenced so that everything paid once
// at a window edge cancels, and the exit code IS the count.
const runtimeV2TaskTimeoutClaimStrictZeroSource = `
async fn child(k: int) -> int {
    return k;
}

async fn window(n: int) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut i: int = 0;
    let mut acc: int = 0;
    while i < n {
        let worker: Task<int> = spawn child(i);
        let v: int = compare timeout(worker, 1000) { Success(x) => x; Cancelled() => 0 - 1; };
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

func TestRuntimeV2TaskResultStrictZeroTimeout(t *testing.T) {
	runTaskResultStrictZeroRow(t, runtimeV2TaskTimeoutClaimStrictZeroSource)
}

// The control the claim exists for, stated as an ANSWER rather than a census.
//
// Here the target's own entitlement dies before the timeout operation comes for
// its result: the handle is a temporary, so nothing in source outlives the
// statement, and the target completes later -- inside the timeout's park. If a
// release ran on the count of live handles alone, the value would be gone by
// the time the operation asked for it, and the row would read the arm it never
// took. It checks the VALUE, so a zeroed slot cannot pass as a cancellation.
const runtimeV2TaskTimeoutTemporaryHandleSource = `
async fn slow(k: int) -> int {
    sleep(2).await();
    return k;
}

async fn run() -> int {
    let mut i: int = 0;
    while i < 4 {
        let v: int = compare timeout(spawn slow(i + 7), 1000) {
            Success(x) => x;
            Cancelled() => 0 - 1;
        };
        if v != i + 7 { return 2; }
        i = i + 1;
    }
    print("vm-timeout-temporary-handle-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() { Success(c) => c; Cancelled() => 90; };
}
`

func TestRuntimeV2VMTimeoutOverATemporaryHandle(t *testing.T) {
	requireVMLane(t)
	res := runProgramFromSource(t, runtimeV2TaskTimeoutTemporaryHandleSource, runOptions{})
	if res.stderr != "" {
		t.Fatalf("timeout over a temporary handle: the program reported\n%s", res.stderr)
	}
	if res.exitCode != 0 {
		t.Fatalf("timeout over a temporary handle returned %d", res.exitCode)
	}
}
