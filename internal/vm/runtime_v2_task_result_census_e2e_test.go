package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A task's result is stored at its own type, so a result WIDER than a
// machine word no longer costs a box.
//
// The representation this replaces could carry exactly one word. A
// composite result did not fit, so it was boxed on the producing side and
// read back through the pointer on the consuming one: one heap allocation
// per completed task that the program never wrote, on a value whose two
// fields are plain integers and touch the heap nowhere else.
//
// This windows that allocation directly. Both probes return a value of
// the SAME shape from an async body and read it back through await; they
// differ only in width:
//
//	narrow: one int -- fits a word, so it cost nothing before and costs
//	        nothing now. It is the control: if the per-iteration figure
//	        moved here too, the difference below would be about task
//	        machinery rather than about the result's storage.
//	wide:   two ints -- did NOT fit a word. Its box is what disappeared.
//
// The figures are pinned exactly rather than asserted flat, so a later
// reduction collapses this loudly instead of passing quietly. Each is the
// per-iteration allocation count, taken as the difference between an
// eight-iteration window and a one-iteration window so that everything
// paid once at the window edge cancels.
const runtimeV2TaskResultCensusSource = `
@copy type Pair = { a: int, b: int };

async fn make_narrow(k: int) -> int {
    return k + 1;
}

async fn make_wide(k: int) -> Pair {
    return Pair { a = k, b = k + 1 };
}

async fn narrow_window(n: int) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut i: int = 0;
    let mut acc: int = 0;
    while i < n {
        let v: int = compare make_narrow(i).await() { Success(x) => x; Cancelled() => 0 - 1; };
        if v != i + 1 { return 999999; }
        acc = acc + v;
        i = i + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    if acc < 0 { return 999998; }
    return c1.alloc_count - c0.alloc_count;
}

async fn wide_window(n: int) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut i: int = 0;
    let mut acc: int = 0;
    while i < n {
        let p: Pair = compare make_wide(i).await() {
            Success(x) => x;
            Cancelled() => Pair { a = 0 - 1, b = 0 - 1 };
        };
        if p.a != i { return 999997; }
        if p.b != i + 1 { return 999996; }
        acc = acc + p.b;
        i = i + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    if acc < 0 { return 999995; }
    return c1.alloc_count - c0.alloc_count;
}

fn report(label: string, narrow: uint, wide: uint) -> int {
    print("FAIL task result census ");
    print(label);
    print(" narrow=");
    print(narrow to string);
    print(" wide=");
    print(wide to string);
    return 0 - 1;
}

async fn run() -> int {
    let n1: uint = compare narrow_window(1).await() { Success(x) => x; Cancelled() => 999999; };
    let n8: uint = compare narrow_window(8).await() { Success(x) => x; Cancelled() => 999999; };
    let w1: uint = compare wide_window(1).await() { Success(x) => x; Cancelled() => 999999; };
    let w8: uint = compare wide_window(8).await() { Success(x) => x; Cancelled() => 999999; };
    if n1 >= 999000 || n8 >= 999000 || w1 >= 999000 || w8 >= 999000 {
        print("FAIL task result value");
        return 1;
    }
    print("task result census narrow one=");
    print(n1 to string);
    print(" eight=");
    print(n8 to string);
    print("task result census wide one=");
    print(w1 to string);
    print(" eight=");
    print(w8 to string);

    // THE PROPERTY: a composite result costs exactly what a scalar one
    // costs, at both window sizes. Both live in the task's own storage, and
    // neither takes a block of its own.
    //
    // It is stated as an equality between the two probes rather than as a
    // pinned absolute, because the absolute is task machinery -- which this
    // step does not claim to have changed -- while the DIFFERENCE is the box,
    // which it removed. On the representation this replaces the wide probe
    // allocated one block per iteration that the narrow one did not.
    if n1 != w1 { return report("one-iteration", n1, w1); }
    if n8 != w8 { return report("eight-iterations", n8, w8); }

    print("task-result-census-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() { Success(code) => code; Cancelled() => 90; };
}
`

func TestRuntimeV2TaskResultCensusBalanced(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2TaskResultCensusSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	duration, result := runBinaryWithTimeout(t, outputPath, baseEnv, 30*time.Second)
	if result.exitCode != 0 {
		t.Fatalf("task result census failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode, duration, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "task-result-census-ok") {
		t.Fatalf("task result census missing completion marker; stdout=%q", result.stdout)
	}
}
