package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// One worker fixes the await order for exact narrow/wide allocation equality.
// With multiple workers, a JOIN waiter store can first grow inside either
// window even when both result types stay inline. That run checks values.
// Both modes execute the same producers, awaits and value guards; only the
// final allocation assertions differ. No absolute count or tolerance is used.
const runtimeV2TaskResultCensusSourceTemplate = `
@copy type Pair = { a: int64, b: int64 };

async fn make_narrow(k: int64) -> int64 {
    return k + 1:int64;
}

async fn make_wide(k: int64) -> Pair {
    return Pair { a = k, b = k + 1:int64 };
}

async fn narrow_window(n: int64) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut i: int64 = 0:int64;
    let mut acc: int64 = 0:int64;
    while i < n {
        let v: int64 = compare make_narrow(i).await() { Success(x) => x; Cancelled() => 0:int64 - 1:int64; };
        if v != i + 1:int64 { return 999999; }
        acc = acc + v;
        i = i + 1:int64;
    }
    let c1: HeapStats = rt_heap_stats();
    if acc < 0:int64 { return 999998; }
    return c1.alloc_count - c0.alloc_count;
}

async fn wide_window(n: int64) -> uint {
    let c0: HeapStats = rt_heap_stats();
    let mut i: int64 = 0:int64;
    let mut acc: int64 = 0:int64;
    while i < n {
        let p: Pair = compare make_wide(i).await() {
            Success(x) => x;
            Cancelled() => Pair { a = 0:int64 - 1:int64, b = 0:int64 - 1:int64 };
        };
        if p.a != i { return 999997; }
        if p.b != i + 1:int64 { return 999996; }
        acc = acc + p.b;
        i = i + 1:int64;
    }
    let c1: HeapStats = rt_heap_stats();
    if acc < 0:int64 { return 999995; }
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
    let n1: uint = compare narrow_window(1:int64).await() { Success(x) => x; Cancelled() => 999999; };
    let n8: uint = compare narrow_window(8:int64).await() { Success(x) => x; Cancelled() => 999999; };
    let w1: uint = compare wide_window(1:int64).await() { Success(x) => x; Cancelled() => 999999; };
    let w8: uint = compare wide_window(8:int64).await() { Success(x) => x; Cancelled() => 999999; };
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

    if %t {
        if n1 != w1 { return report("one-iteration", n1, w1); }
        if n8 != w8 { return report("eight-iterations", n8, w8); }
        print("task-result-census-ok");
    } else {
        print("task-result-fixed-values-ok");
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() { Success(code) => code; Cancelled() => 90; };
}
`

// Values beyond a machine word must survive both scalar and composite task
// results. Check each field and each accumulated sum for both signs.
const runtimeV2TaskResultCountedValuesSource = `
@copy type Pair = { a: int, b: int };

async fn make_narrow(k: int) -> int {
    return k + 1;
}

async fn make_wide(k: int) -> Pair {
    return Pair { a = k, b = k + 1 };
}

async fn window(start: int) -> int {
    let mut i: int64 = 0:int64;
    let mut narrow_acc: int = 0;
    let mut wide_acc: int = 0;
    while i < 8:int64 {
        let k: int = start + (i to int);
        let v: int = compare make_narrow(k).await() {
            Success(x) => x;
            Cancelled() => { return 91; };
        };
        let p: Pair = compare make_wide(k).await() {
            Success(x) => x;
            Cancelled() => { return 92; };
        };
        if v != k + 1 { return 1; }
        if p.a != k { return 2; }
        if p.b != k + 1 { return 3; }
        narrow_acc = narrow_acc + v;
        wide_acc = wide_acc + p.b;
        i = i + 1:int64;
    }
    let expected: int = 8 * start + 36;
    if narrow_acc != expected { return 4; }
    if wide_acc != expected { return 5; }
    return 0;
}

async fn run() -> int {
    let start: int = 1208925819614629174706176;
    let positive: int = compare window(start).await() { Success(x) => x; Cancelled() => 93; };
    if positive != 0 { return positive; }
    let negative: int = compare window(0 - start).await() { Success(x) => x; Cancelled() => 94; };
    if negative != 0 { return negative; }
    print("task-result-counted-values-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() { Success(code) => code; Cancelled() => 90; };
}
`

func TestRuntimeV2TaskResultCensusBalanced(t *testing.T) {
	for _, backend := range []string{backendVM, backendLLVM} {
		t.Run("counted_values/"+backend, func(t *testing.T) {
			t.Setenv(backendEnvVar, backend)
			res := runProgramFromSource(t, runtimeV2TaskResultCountedValuesSource, runOptions{})
			// VM stdout is not captured. Row failures use exit codes; runtime
			// errors may instead leave exit zero and report through stderr.
			if res.exitCode != 0 || res.stderr != "" {
				t.Fatalf("counted task result failed at row %d\nstdout:\n%s\nstderr:\n%s",
					res.exitCode, res.stdout, res.stderr)
			}
			if backend == backendLLVM && !strings.Contains(res.stdout, "task-result-counted-values-ok") {
				t.Fatalf("counted task result missing completion marker; stdout=%q", res.stdout)
			}
		})
	}
	for _, tc := range []struct {
		name        string
		workers     string
		checkCensus bool
		marker      string
	}{
		{"allocation_census/workers-1-shards-1", "1", true, "task-result-census-ok"},
		{"fixed_values/workers-8-shards-8", "8", false, "task-result-fixed-values-ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SURGE_THREADS", tc.workers)
			t.Setenv("SURGE_SHARDS", tc.workers)
			source := fmt.Sprintf(runtimeV2TaskResultCensusSourceTemplate, tc.checkCensus)
			outputPath := buildRuntimeV2CrossingSource(t, source, nil)
			baseEnv := envWithStdlib(repoRoot(t))
			duration, result := runBinaryWithTimeout(t, outputPath, baseEnv, 30*time.Second)
			if result.exitCode != 0 || result.stderr != "" {
				t.Fatalf("task result failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode, duration, result.stdout, result.stderr)
			}
			if strings.Contains(result.stdout, "FAIL") || strings.Count(result.stdout, tc.marker) != 1 {
				t.Fatalf("task result missing completion marker %q; stdout=%q", tc.marker, result.stdout)
			}
			t.Logf("task result output:\n%s", result.stdout)
		})
	}
}
