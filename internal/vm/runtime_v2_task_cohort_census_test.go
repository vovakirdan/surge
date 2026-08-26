package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A cohort of E handles on one task costs E-1 duplications and one move, not E
// duplications and a drop.
//
// Every asker still receives an independent value — that part was already true
// — but the LAST asker has nothing left to protect: no other handle exists, so
// nobody can come for the canonical value afterwards, and moving it is both
// cheaper and what §10 of the storage model asks for. What this measures is
// that the saving is real and exactly one per cohort.
//
// The two windows differ in ONE thing, the number of handles, so what separates
// them is the duplication and nothing else: both spawn the same body, both
// await every handle they hold, and both leave nothing behind.
const runtimeV2TaskCohortCensusSource = `
fn build(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 3 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

async fn produce() -> string {
    return build("cohort-");
}

async fn one_handle(n: int) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut i: int = 0;
    while i < n {
        let task: Task<string> = spawn produce();
        let only: string = compare task.await() { Success(v) => v; Cancelled() => ""; };
        if only != "cohort-xxx" { return 999999; }
        i = i + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    return c1.alloc_count - c0.alloc_count;
}

async fn two_handles(n: int) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut i: int = 0;
    while i < n {
        let task: Task<string> = spawn produce();
        let sibling: Task<string> = task.clone();
        let first: string = compare task.await() { Success(v) => v; Cancelled() => ""; };
        let second: string = compare sibling.await() { Success(v) => v; Cancelled() => ""; };
        if first != "cohort-xxx" { return 999998; }
        if second != "cohort-xxx" { return 999997; }
        i = i + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    return c1.alloc_count - c0.alloc_count;
}

fn report(label: string, one: uint, two: uint) -> int {
    print("FAIL task cohort census ");
    print(label);
    print(" one=");
    print(one to string);
    print(" two=");
    print(two to string);
    return 0 - 1;
}

async fn run() -> int {
    let a1: uint = compare one_handle(1).await() { Success(x) => x; Cancelled() => 999999; };
    let a8: uint = compare one_handle(8).await() { Success(x) => x; Cancelled() => 999999; };
    let b1: uint = compare two_handles(1).await() { Success(x) => x; Cancelled() => 999999; };
    let b8: uint = compare two_handles(8).await() { Success(x) => x; Cancelled() => 999999; };
    if a1 >= 999000 || a8 >= 999000 || b1 >= 999000 || b8 >= 999000 {
        print("FAIL task cohort value");
        return 1;
    }
    let one_cost: uint = (a8 - a1) / 7:uint;
    let two_cost: uint = (b8 - b1) / 7:uint;
    print("task cohort census one-handle per-iteration=");
    print(one_cost to string);
    print("task cohort census two-handle per-iteration=");
    print(two_cost to string);

    // The figures are pinned exactly rather than asserted flat, so the next
    // real reduction collapses this loudly instead of passing quietly.
    //
    // The two windows differ by TWO allocations per iteration, and only one of
    // them is the duplication: the second window also runs a second await,
    // with its own compare and its own result storage. What the entitlement
    // counts moved is the OTHER one -- before them, the two-handle window
    // cost 14, because the last asker duplicated as well and the canonical
    // value was then dropped behind it.
    if one_cost != 11:uint { return report("one-handle", one_cost, two_cost); }
    if two_cost != 13:uint { return report("two-handle", one_cost, two_cost); }

    print("task-cohort-census-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

func TestRuntimeV2TaskCohortCostsOneDuplicationPerExtraHandle(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2TaskCohortCensusSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	duration, result := runBinaryWithTimeout(t, outputPath, baseEnv, 30*time.Second)
	if result.exitCode != 0 {
		t.Fatalf("task cohort census failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode, duration, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "task-cohort-census-ok") {
		t.Fatalf("task cohort census missing completion marker; stdout=%q", result.stdout)
	}
}
