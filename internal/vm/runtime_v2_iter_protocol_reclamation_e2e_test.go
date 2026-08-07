//go:build !golden

package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The generic for-loop iterator protocol (iter_init/iter_next) leaked on the
// LLVM backend: each iter_next boxes a fresh Option<T> on the heap, and the
// iterator itself (array cursor or range cursor) is a single heap struct —
// neither was ever reclaimed. `for x in arr` over a 3-element array inside a
// 2000-iteration outer loop lost 10,000 blocks on the committed baseline.
//
// Every probe below declares its iterable ONCE, outside the counting loop,
// and re-iterates it `n` times: that isolates the iterable's own (unrelated)
// heap cost as a per-call constant, so a d(n=2000) - d(n=1) differential
// cancels it and leaves exactly the iterator protocol's per-iteration cost —
// zero after this fix, scaling with n before it. This also sidesteps an
// adjacent, pre-existing gap this investigation surfaced (see the report):
// a boxed composite `let` re-declared inside a `while` body — completely
// unrelated to for-loops — is not reclaimed per iteration either; using the
// SAME iterable instance across all outer passes keeps that gap out of the
// measured window instead of silently laundering it into this gate.
//
// A discarded loop variable (`for _ in arr`) used to fail to compile with
// an "unknown type" MIR validation error: normalizeIterFor's element-type
// resolution (VarType, falling back to the bound symbol's type) never
// falls back further when the pattern binds no symbol at all, so the
// synthesized iterator/next locals got no type — found during this
// investigation, fixed separately: normalizeIterFor now falls back to
// the iterable's own element type via ArrayInfo/ArrayFixedInfo/a
// single-type-argument struct instance. Now reachable,
// array_for_discard_n_times below exercises the discarded-
// payload release branch (a full drop of the unconsumed Option box) this
// same investigation found correct but dead code until that fix landed.
const runtimeV2IterProtocolReclamationSource = `
type Point = { x: int64, y: int64 }

fn array_for_n_times(n: int) -> int {
    let arr: int[] = [1, 2, 3];
    let mut sum: int = 0;
    let mut i: int = 0;
    while i < n {
        for x in arr {
            sum = sum + x;
        }
        i = i + 1;
    }
    return sum;
}

fn array_for_break_n_times(n: int) -> int {
    let arr: int[] = [1, 2, 3, 4, 5];
    let mut hits: int = 0;
    let mut i: int = 0;
    while i < n {
        for x in arr {
            if x == 3 {
                hits = hits + 1;
                break;
            }
        }
        i = i + 1;
    }
    return hits;
}

fn array_for_return_once(arr: &int[], target: int) -> int {
    for x in arr {
        if x == target {
            return x;
        }
    }
    return -1;
}

fn array_for_return_n_times(n: int) -> int {
    let arr: int[] = [1, 2, 3, 4, 5];
    let mut total: int = 0;
    let mut i: int = 0;
    while i < n {
        total = total + array_for_return_once(&arr, 3);
        i = i + 1;
    }
    return total;
}

// The return here sits inside a value-producing expression (a compare
// arm assigned to a let), not as a bare loop-body statement: MIR's
// block-expression lowering intercepts an IMPLICIT return the same way
// (routing it to the compare's own exit, not the function's), so this
// pins that a genuine, explicit return still reaches the function exit
// from inside that nesting and still gets the cursor released.
fn array_for_expr_return_once(arr: &int[], target: int) -> int {
    for x in arr {
        let y: int = compare x == target {
            true => return x;
            false => 0;
        };
        if y == -999 { return -999; }
    }
    return -1;
}

fn array_for_expr_return_n_times(n: int) -> int {
    let arr: int[] = [1, 2, 3, 4, 5];
    let mut total: int = 0;
    let mut i: int = 0;
    while i < n {
        total = total + array_for_expr_return_once(&arr, 3);
        i = i + 1;
    }
    return total;
}

fn array_for_discard_n_times(n: int) -> int {
    let arr: int[] = [1, 2, 3];
    let mut count: int = 0;
    let mut i: int = 0;
    while i < n {
        for _ in arr {
            count = count + 1;
        }
        i = i + 1;
    }
    return count;
}

fn range_for_n_times(n: int) -> int {
    let r = 2..7;
    let mut sum: int = 0;
    let mut i: int = 0;
    while i < n {
        for x in r {
            sum = sum + x;
        }
        i = i + 1;
    }
    return sum;
}

fn build_str(base: string, suffix: int) -> string {
    return base + (suffix to string);
}

fn string_for_n_times(n: int) -> int {
    let arr: string[] = [build_str("s", 1), build_str("s", 22), build_str("s", 333)];
    let mut total: int = 0;
    let mut i: int = 0;
    while i < n {
        for s in arr {
            total = total + (len(s) to int);
        }
        i = i + 1;
    }
    return total;
}

// Every count here is fixed-width on purpose. This is the row held to strict
// zero, and arbitrary-precision arithmetic allocates a block per operation — so
// an int accumulator or an int loop counter would put the loop BODY's heap
// traffic in front of the iteration's and leave nothing to measure. With
// machine integers the only thing left that could touch the heap is the
// iteration protocol itself, which is the question being asked.
fn struct_for_n_times(n: int64) -> int {
    let arr: Point[] = [{ x: 1:int64, y: 2:int64 }, { x: 3:int64, y: 4:int64 }, { x: 5:int64, y: 6:int64 }];
    let mut total: int64 = 0:int64;
    let mut i: int64 = 0:int64;
    while i < n {
        for p in arr {
            total = total + p.x + p.y;
        }
        i = i + 1:int64;
    }
    return total to int;
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

// per_pass_check names the exact cost instead of checking that two unknown
// costs cancel: entering a for-loop N more times must allocate exactly N more
// blocks and free exactly N more.
//
// That one block is the loop's cursor, which the runtime owns for as long as
// the loop runs. What the count pins is everything it does NOT include: the
// elements. Three composites are stepped over on every pass and they contribute
// nothing, because each one is already in the array's buffer and stepping to it
// is an address, not a copy onto the heap.
//
// A differential cannot ask this. It compares the growth in allocations against
// the growth in frees, so a block per element per step passes as long as it is
// also freed — which is why a box per element could sit behind it unremarked.
fn per_pass_check(label: string, before1: &HeapStats, after1: &HeapStats, before2: &HeapStats, after2: &HeapStats, extra: uint) -> int {
    let allocs1: uint = after1.alloc_count - before1.alloc_count;
    let frees1: uint = after1.free_count - before1.free_count;
    let allocs2: uint = after2.alloc_count - before2.alloc_count;
    let frees2: uint = after2.free_count - before2.free_count;
    if allocs2 - allocs1 != extra || frees2 - frees1 != extra {
        print(label);
        print("per-pass heap cost is not one cursor and nothing else");
        print((allocs2 - allocs1) to string);
        print((frees2 - frees1) to string);
        print(extra to string);
        return 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    // array-for: bound loop variable (shallow step release + cursor release).
    let a1_before: HeapStats = rt_heap_stats();
    let a1: int = array_for_n_times(1);
    let a1_after: HeapStats = rt_heap_stats();
    let a2000_before: HeapStats = rt_heap_stats();
    let a2000: int = array_for_n_times(2000);
    let a2000_after: HeapStats = rt_heap_stats();
    if a1 != 6 || a2000 != 12000 {
        print("array-for value mismatch");
        print(a1 to string);
        print(a2000 to string);
        return 1;
    }
    let r1: int = diff_check("array-for", &a1_before, &a1_after, &a2000_before, &a2000_after);
    if r1 != 0 { return 10 + r1; }

    // array-for: early break still frees the cursor exactly once (the
    // break lands at the same point natural exhaustion would).
    let b1_before: HeapStats = rt_heap_stats();
    let b1: int = array_for_break_n_times(1);
    let b1_after: HeapStats = rt_heap_stats();
    let b2000_before: HeapStats = rt_heap_stats();
    let b2000: int = array_for_break_n_times(2000);
    let b2000_after: HeapStats = rt_heap_stats();
    if b1 != 1 || b2000 != 2000 {
        print("array-for-break value mismatch");
        print(b1 to string);
        print(b2000 to string);
        return 3;
    }
    let r3: int = diff_check("array-for-break", &b1_before, &b1_after, &b2000_before, &b2000_after);
    if r3 != 0 { return 30 + r3; }

    // array-for: return from inside the body frees the cursor on an exit
    // path that never reaches the after-the-loop release point.
    let e1_before: HeapStats = rt_heap_stats();
    let e1: int = array_for_return_n_times(1);
    let e1_after: HeapStats = rt_heap_stats();
    let e2000_before: HeapStats = rt_heap_stats();
    let e2000: int = array_for_return_n_times(2000);
    let e2000_after: HeapStats = rt_heap_stats();
    if e1 != 3 || e2000 != 6000 {
        print("array-for-return value mismatch");
        print(e1 to string);
        print(e2000 to string);
        return 4;
    }
    let r4: int = diff_check("array-for-return", &e1_before, &e1_after, &e2000_before, &e2000_after);
    if r4 != 0 { return 40 + r4; }

    // array-for: return nested inside an expression-embedded block (a
    // compare arm assigned to a let) still frees the cursor.
    let x1_before: HeapStats = rt_heap_stats();
    let x1: int = array_for_expr_return_n_times(1);
    let x1_after: HeapStats = rt_heap_stats();
    let x2000_before: HeapStats = rt_heap_stats();
    let x2000: int = array_for_expr_return_n_times(2000);
    let x2000_after: HeapStats = rt_heap_stats();
    if x1 != 3 || x2000 != 6000 {
        print("array-for-expr-return value mismatch");
        print(x1 to string);
        print(x2000 to string);
        return 8;
    }
    let r8: int = diff_check("array-for-expr-return", &x1_before, &x1_after, &x2000_before, &x2000_after);
    if r8 != 0 { return 80 + r8; }

    // array-for with a discarded loop variable: the discarded-payload
    // release branch (full drop of the unconsumed Option box, unlike the
    // shallow step release the bound-variable probes above exercise).
    let d1_before: HeapStats = rt_heap_stats();
    let d1: int = array_for_discard_n_times(1);
    let d1_after: HeapStats = rt_heap_stats();
    let d2000_before: HeapStats = rt_heap_stats();
    let d2000: int = array_for_discard_n_times(2000);
    let d2000_after: HeapStats = rt_heap_stats();
    if d1 != 3 || d2000 != 6000 {
        print("array-for-discard value mismatch");
        print(d1 to string);
        print(d2000 to string);
        return 9;
    }
    let r9: int = diff_check("array-for-discard", &d1_before, &d1_after, &d2000_before, &d2000_after);
    if r9 != 0 { return 90 + r9; }

    // range-for over a STORED Range<int> (the generic iterator protocol's
    // range-cursor path, distinct from the fast-pathed literal for-head).
    let g1_before: HeapStats = rt_heap_stats();
    let g1: int = range_for_n_times(1);
    let g1_after: HeapStats = rt_heap_stats();
    let g2000_before: HeapStats = rt_heap_stats();
    let g2000: int = range_for_n_times(2000);
    let g2000_after: HeapStats = rt_heap_stats();
    if g1 != 20 || g2000 != 40000 {
        print("range-for value mismatch");
        print(g1 to string);
        print(g2000 to string);
        return 5;
    }
    let r5: int = diff_check("range-for", &g1_before, &g1_after, &g2000_before, &g2000_after);
    if r5 != 0 { return 50 + r5; }

    // array-for over a heap-owning (string) element type: pins the class
    // the cursor-safety guard must never mistake for the fixed-width
    // Range<T> raw-pointer residual.
    let s1_before: HeapStats = rt_heap_stats();
    let s1: int = string_for_n_times(1);
    let s1_after: HeapStats = rt_heap_stats();
    let s2000_before: HeapStats = rt_heap_stats();
    let s2000: int = string_for_n_times(2000);
    let s2000_after: HeapStats = rt_heap_stats();
    if s1 != 9 || s2000 != 18000 {
        print("string-for value mismatch");
        print(s1 to string);
        print(s2000 to string);
        return 6;
    }
    let r6: int = diff_check("string-for", &s1_before, &s1_after, &s2000_before, &s2000_after);
    if r6 != 0 { return 60 + r6; }

    // array-for over a struct element type. This row is held to STRICT ZERO
    // rather than to a balance: a Point sits inline in the array's buffer, so
    // stepping over one allocates nothing at all, and iterating 2000 times must
    // cost exactly what iterating once does.
    //
    // The differential the other rows use cannot ask that. It compares the
    // growth from N=1 to N=2000 in allocations against the same growth in
    // frees, so a per-step allocation that is always freed passes — the right
    // question when an element genuinely owns something, and the wrong one
    // here, where the answer is that there is nothing to own. A box per element
    // per step would cancel out of it exactly.
    let p1_before: HeapStats = rt_heap_stats();
    let p1: int = struct_for_n_times(1:int64);
    let p1_after: HeapStats = rt_heap_stats();
    let p2000_before: HeapStats = rt_heap_stats();
    let p2000: int = struct_for_n_times(2000:int64);
    let p2000_after: HeapStats = rt_heap_stats();
    if p1 != 21 || p2000 != 42000 {
        print("struct-for value mismatch");
        print(p1 to string);
        print(p2000 to string);
        return 7;
    }
    let r7: int = diff_check("struct-for", &p1_before, &p1_after, &p2000_before, &p2000_after);
    if r7 != 0 { return 70 + r7; }
    let r10: int = per_pass_check("struct-for", &p1_before, &p1_after, &p2000_before, &p2000_after, 1999);
    if r10 != 0 { return 100 + r10; }

    print("iter-protocol-reclamation-ok");
    return 0;
}
`

func TestRuntimeV2IterProtocolReclamationCensusBalanced(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2IterProtocolReclamationSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, threads := range []string{"1", "2"} {
		t.Run(fmt.Sprintf("threads_%s", threads), func(t *testing.T) {
			env := overrideEnvVar(baseEnv, "SURGE_THREADS", threads)
			duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
			if result.exitCode != 0 {
				t.Fatalf(
					"iterator protocol reclamation census failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode,
					duration,
					result.stdout,
					result.stderr,
				)
			}
			if !strings.Contains(result.stdout, "iter-protocol-reclamation-ok") {
				t.Fatalf("iterator protocol reclamation census missing completion marker; stdout=%q", result.stdout)
			}
		})
	}
}
