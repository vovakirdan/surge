package vm_test

import (
	"strings"
	"testing"
	"time"
)

// An in-range integer/uint literal folds to a tagged fixnum word at compile
// time (internal/backend/llvm/emit_term.go), so evaluating it allocates
// nothing — where it previously called rt_big*_from_literal to re-parse the
// decimal string on every use, at two heap allocations per digit
// (RV2-DEBT-036). The witness is a hot loop whose condition and body both
// use multi-digit literals: allocation traffic across the loop must be flat,
// not proportional to the iteration count.
//
// rt_heap_stats() is the in-language witness; the loop count is a runtime
// value so the two measured runs compile to the same code and differ only
// in how many times the literal-bearing instructions execute.
const runtimeV2LiteralFoldSource = `
fn alloc_delta(before: &HeapStats, after: &HeapStats) -> uint {
    return after.alloc_count - before.alloc_count;
}

fn run_loop(n: int) -> uint {
    let a: HeapStats = rt_heap_stats();
    let mut i = 0;
    while i < 100000 {
        i = i + 1;
        if i > n { i = i; }
    }
    let b: HeapStats = rt_heap_stats();
    return alloc_delta(&a, &b);
}

@entrypoint
fn main() -> int {
    // Two loop bodies, same literals, different trip counts. If the literals
    // re-materialized per iteration the deltas would differ by ~14 per extra
    // iteration; folded, both are flat.
    let short: uint = run_loop(10);
    let long: uint = run_loop(100000);
    print("short=");
    print(short to string);
    print(" long=");
    print(long to string);
    print("\n");
    // Allow a tiny constant slack for stats bookkeeping, but the delta must
    // NOT scale: a per-iteration re-parse would put ~1.4M allocations here.
    if long > 1000 {
        print("FAIL literal churn: long-loop allocations scale with iterations\n");
        return 1;
    }
    print("literal-fold-ok\n");
    return 0;
}
`

func TestRuntimeV2InRangeLiteralsFoldToFixnum(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2LiteralFoldSource, nil)
	env := envWithStdlib(repoRoot(t))
	env = overrideEnvVar(env, "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")
	_, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
	if result.exitCode != 0 {
		t.Fatalf("literal-fold witness failed (exit=%d)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode, result.stdout, result.stderr)
	}
	if strings.Contains(result.stdout, "FAIL") {
		t.Fatalf("literal churn detected; stdout=%q", result.stdout)
	}
	if !strings.Contains(result.stdout, "literal-fold-ok") {
		t.Fatalf("missing completion marker; stdout=%q", result.stdout)
	}
}
