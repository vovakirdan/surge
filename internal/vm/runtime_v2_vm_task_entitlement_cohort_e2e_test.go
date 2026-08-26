package vm_test

import "testing"

// What the cohort must keep true while the last asker learns to MOVE.
//
// Both rows are CONTROLS. They pass on either side of the reserved final move
// and they are here for that reason: the move changes which asker duplicates,
// and the two properties most easily broken by getting that wrong are the ones
// a program can actually see. The rule itself is asked where it is a decision,
// in task_entitlement_internal_test.go, because a duplication and a move are
// indistinguishable to the program that reads the result.

// C3: two entitlements, one result, two values that are not each other.
// The VM twin of TestLLVMNativeClonedTaskHandleServesEachAwaiterItsOwnResult,
// which had no VM parity: every cloned-handle fixture on this lane carries an
// `int`, where an alias and an independent value read the same.
const runtimeV2VMCohortIndependenceSource = `
type Box = { note: string, count: int };

extern<Box> {
    pub fn __clone(self: &Box) -> Box {
        return Box { note = self.note.__clone(), count = self.count };
    }
}

async fn produce(k: int) -> Box {
    let b: Box = { note = "shared-by-nobody", count = k };
    return b;
}

async fn run() -> int {
    let mut i: int = 0;
    while i < 4 {
        let t: Task<Box> = spawn produce(i);
        let sib: Task<Box> = t.clone();
        let mut a: Box = compare t.await() { Success(x) => x; Cancelled() => { return 91; }; };
        let b: Box = compare sib.await() { Success(x) => x; Cancelled() => { return 92; }; };
        if a.count != i { return 2; }
        if b.count != i { return 3; }
        if a.note != "shared-by-nobody" { return 4; }
        if b.note != "shared-by-nobody" { return 5; }
        a.count = a.count + 100;
        if b.count != i { return 6; }
        if a.count != i + 100 { return 7; }
        i = i + 1;
    }
    print("vm-cohort-independence-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() { Success(c) => c; Cancelled() => 90; };
}
`

// D1: cancel is TASK-GLOBAL, not entitlement-local. Requesting it through one
// sibling is observed by every entitlement, which is what stops the move from
// being read as "the last asker owns the task".
const runtimeV2VMCohortCancelThroughSiblingSource = `
async fn slow(k: int) -> int {
    sleep(50).await();
    return k;
}

async fn run() -> int {
    let t: Task<int> = spawn slow(5);
    let sib: Task<int> = t.clone();
    sib.cancel();
    let r1: int = compare t.await() { Success(x) => x; Cancelled() => 0 - 1; };
    let r2: int = compare sib.await() { Success(x) => x; Cancelled() => 0 - 1; };
    if r1 != 0 - 1 { return 2; }
    if r2 != 0 - 1 { return 3; }
    print("vm-cancel-through-sibling-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() { Success(c) => c; Cancelled() => 90; };
}
`

func TestRuntimeV2VMClonedHandlesEachGetTheirOwnResult(t *testing.T) {
	requireVMLane(t)
	res := runProgramFromSource(t, runtimeV2VMCohortIndependenceSource, runOptions{})
	if res.stderr != "" {
		t.Fatalf("cloned handles did not each get their own result: the program reported\n%s", res.stderr)
	}
	if res.exitCode != 0 {
		t.Fatalf("cloned handles did not each get their own result: row %d", res.exitCode)
	}
}

func TestRuntimeV2VMCancelThroughASiblingIsTaskGlobal(t *testing.T) {
	requireVMLane(t)
	res := runProgramFromSource(t, runtimeV2VMCohortCancelThroughSiblingSource, runOptions{})
	if res.stderr != "" {
		t.Fatalf("cancel through a sibling: the program reported\n%s", res.stderr)
	}
	if res.exitCode != 0 {
		t.Fatalf("cancel through a sibling was not task-global: row %d", res.exitCode)
	}
}
