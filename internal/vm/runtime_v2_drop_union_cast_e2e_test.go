//go:build !golden

package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// RV2-DEBT-057: casting a union value between two structurally-identical
// but distinctly-interned union type IDs leaked the source box on the LLVM
// backend. `emitUnionCast` (internal/backend/llvm/emit_union_cast.go)
// always allocates a fresh box for the retagged value but never freed the
// one it replaced. The site fires on the ordinary tag-constructor-to-
// declared-union coercion (`let v: Outcome = Payload(...)`) whenever the
// constructor's own inferred type isn't already interned as the declared
// union — a structural-to-nominal upcast, not anything crossing-specific.
// It also fired inside every union-cast census window on the whole
// reclamation arc (any explicitly-typed union `let` triggers it), which is
// why other census probes on this arc go out of their way to avoid an
// explicit union-typed `let` for their scrutinee.
//
// The old box is always safe to free here: this cast only ever fires on a
// value already owned at this point (a fresh tag-constructor temporary, or
// a binding already consumed by the enclosing let/assignment's own move
// semantics) — a borrowed source (`&T`) takes a different path in the same
// function (a dereferenced read, guarded by the same ownership check) and
// is never freed. Every payload field needed by the new box is read into
// SSA temporaries before the old box is freed, so the free cannot race the
// read it depends on.
//
// single_cast_frees_source_box pins the exact free-count unit (one box,
// once). union_cast_n_times + the n=1/n=large differential (matching the
// iterator-protocol census convention) prove the fix holds under a
// cast-heavy loop, not just once: the per-iteration net alloc/free stays
// balanced regardless of loop count.
const runtimeV2DropUnionCastSource = `
tag Payload(string);
tag Empty();
type Outcome = Payload(string) | Empty();

fn check_frees(label: string, before: &HeapStats, after: &HeapStats, expected: uint) -> int {
    let frees: uint = after.free_count - before.free_count;
    if frees != expected {
        print(label);
        print(frees to string);
        print(expected to string);
        return 1;
    }
    return 0;
}

fn single_cast_frees_source_box() -> int {
    let before: HeapStats = rt_heap_stats();
    let v: Outcome = Payload("cast-me");
    let after: HeapStats = rt_heap_stats();
    let r: int = check_frees("single-cast window", &before, &after, 1:uint);
    @drop v;
    return r;
}

fn build_tag(base: string, suffix: int) -> string {
    return base + (suffix to string);
}

fn union_cast_n_times(n: int) -> int {
    let mut total: int = 0;
    let mut i: int = 0;
    while i < n {
        let v: Outcome = Payload(build_tag("cast", i));
        let out: string = compare v {
            Payload(s) => s;
            Empty() => "";
        };
        total = total + (len(out) to int);
        i = i + 1;
    }
    return total;
}

fn diff_check(label: string, before1: &HeapStats, after1: &HeapStats, before2: &HeapStats, after2: &HeapStats) -> int {
    let allocs1: uint = after1.alloc_count - before1.alloc_count;
    let frees1: uint = after1.free_count - before1.free_count;
    let allocs2: uint = after2.alloc_count - before2.alloc_count;
    let frees2: uint = after2.free_count - before2.free_count;
    let alloc_diff: uint = allocs2 - allocs1;
    let free_diff: uint = frees2 - frees1;
    if alloc_diff != free_diff {
        print(label);
        print("alloc/free differential mismatch");
        print(alloc_diff to string);
        print(free_diff to string);
        return 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let r1: int = single_cast_frees_source_box();
    if r1 != 0 { return 10 + r1; }

    let p1_before: HeapStats = rt_heap_stats();
    let d1: int = union_cast_n_times(1);
    let p1_after: HeapStats = rt_heap_stats();

    let p500_before: HeapStats = rt_heap_stats();
    let d500: int = union_cast_n_times(500);
    let p500_after: HeapStats = rt_heap_stats();

    if d1 <= 0 { return 20; }
    if d500 <= 0 { return 21; }

    let r2: int = diff_check("union-cast-loop", &p1_before, &p1_after, &p500_before, &p500_after);
    if r2 != 0 { return 30 + r2; }

    print("drop-union-cast-ok");
    return 0;
}
`

func TestRuntimeV2DropUnionCastReclamation(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2DropUnionCastSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, threads := range []string{"1", "2"} {
		t.Run(fmt.Sprintf("threads_%s", threads), func(t *testing.T) {
			env := overrideEnvVar(baseEnv, "SURGE_THREADS", threads)
			duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
			if result.exitCode != 0 {
				t.Fatalf(
					"union cast drop reclamation e2e failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode,
					duration,
					result.stdout,
					result.stderr,
				)
			}
			if !strings.Contains(result.stdout, "drop-union-cast-ok") {
				t.Fatalf("union cast drop reclamation e2e missing completion marker; stdout=%q", result.stdout)
			}
		})
	}
}
