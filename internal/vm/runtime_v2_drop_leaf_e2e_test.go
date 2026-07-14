//go:build !golden

package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The leaf free floor end to end: the EXISTING explicit `@drop` statement
// now actually frees strings and dynamic arrays.
//
// Assertion currency is the FREE count only. Until scope-exit synthesis
// lands, every temporary (literal operands, converted strings, snapshot
// boxes) allocates and leaks by design, so alloc/free balance cannot hold
// inside any window — but nothing in the runtime frees spontaneously, so
// the free_count delta across a window containing only @drop statements
// is exact. Expected counts come from the reclamation layout:
//
//	string                          = 1 free (single allocation)
//	base array, no views            = 2 frees (data + header)
//	registered view, base alive     = 2 frees (registry link + view header)
//	base with live views            = 0 frees (deferred; debug counter +1)
//	last view over a deferred base  = 5 frees (link + orphan record +
//	                                  view header + base data + base header)
//	fixed-array slice (no link)     = 1 free (view header only)
const runtimeV2DropLeafSource = `
@intrinsic fn rt_array_debug_deferred_base_drops() -> uint;

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

fn string_drop_frees_once() -> int {
    let s: string = "drop-me-now";
    let before: HeapStats = rt_heap_stats();
    @drop s;
    let after: HeapStats = rt_heap_stats();
    return check_frees("string window", &before, &after, 1:uint);
}

fn owned_array_drop_frees_data_and_header() -> int {
    let mut a: int[] = [];
    a.push(1);
    a.push(2);
    a.push(3);
    let before: HeapStats = rt_heap_stats();
    @drop a;
    let after: HeapStats = rt_heap_stats();
    return check_frees("array window", &before, &after, 2:uint);
}

fn eat(arr: int[]) -> int {
    let n: int = arr.__len() to int;
    @drop arr;
    return n;
}

fn moved_param_dropped_by_callee_only() -> int {
    let mut a: int[] = [];
    a.push(7);
    let before: HeapStats = rt_heap_stats();
    let n: int = eat(a);
    let after: HeapStats = rt_heap_stats();
    if n != 1 { return 2; }
    return check_frees("param-move window", &before, &after, 2:uint);
}

fn read_len(arr: &int[]) -> int {
    return arr.__len() to int;
}

fn borrowed_param_never_dropped() -> int {
    let mut a: int[] = [];
    a.push(9);
    let before: HeapStats = rt_heap_stats();
    let n: int = read_len(&a);
    let mid: HeapStats = rt_heap_stats();
    let first: int = a[0];
    let w0: HeapStats = rt_heap_stats();
    @drop a;
    let w1: HeapStats = rt_heap_stats();
    if n != 1 { return 3; }
    if first != 9 { return 5; }
    let r1: int = check_frees("borrow window", &before, &mid, 0:uint);
    if r1 != 0 { return 4; }
    return check_frees("post-borrow drop window", &w0, &w1, 2:uint);
}

fn view_defers_base() -> int {
    let mut a: int[] = [];
    a.push(1);
    a.push(2);
    a.push(3);
    a.push(4);
    let v: int[] = a.slice(1..3);
    let picked: int = v[0];
    if picked != 2 { return 5; }
    let deferred_before: uint = rt_array_debug_deferred_base_drops();
    let before: HeapStats = rt_heap_stats();
    @drop a;
    let mid: HeapStats = rt_heap_stats();
    let deferred_mid: uint = rt_array_debug_deferred_base_drops();
    let still: int = v[1];
    let w0: HeapStats = rt_heap_stats();
    @drop v;
    let w1: HeapStats = rt_heap_stats();
    let r1: int = check_frees("base-defer window", &before, &mid, 0:uint);
    if r1 != 0 { return 6; }
    if deferred_mid != deferred_before + 1:uint { return 7; }
    if still != 3 { return 8; }
    return check_frees("last-view window", &w0, &w1, 5:uint);
}

fn nested_views_all_orders() -> int {
    let mut a: int[] = [];
    a.push(10);
    a.push(20);
    a.push(30);
    a.push(40);
    let mid_view: int[] = a.slice(0..3);
    let sub: int[] = mid_view.slice(1..2);
    let before: HeapStats = rt_heap_stats();
    @drop a;
    let s1: HeapStats = rt_heap_stats();
    let peek: int = sub[0];
    let w0: HeapStats = rt_heap_stats();
    @drop mid_view;
    let w1: HeapStats = rt_heap_stats();
    @drop sub;
    let w2: HeapStats = rt_heap_stats();
    let r1: int = check_frees("nested base-defer window", &before, &s1, 0:uint);
    if r1 != 0 { return 9; }
    if peek != 20 { return 10; }
    let r2: int = check_frees("nested first-view window", &w0, &w1, 2:uint);
    if r2 != 0 { return 11; }
    return check_frees("nested last-view window", &w1, &w2, 5:uint);
}

fn empty_view_still_drops() -> int {
    let mut a: int[] = [];
    a.push(1);
    let v: int[] = a.slice(0..0);
    let before: HeapStats = rt_heap_stats();
    @drop v;
    let s1: HeapStats = rt_heap_stats();
    @drop a;
    let s2: HeapStats = rt_heap_stats();
    let r1: int = check_frees("empty-view window", &before, &s1, 2:uint);
    if r1 != 0 { return 12; }
    return check_frees("post-empty-view base window", &s1, &s2, 2:uint);
}

fn eat_view(v: int[]) -> int {
    let n: int = v.__len() to int;
    @drop v;
    return n;
}

fn moved_view_dropped_by_callee_leaves_base() -> int {
    let mut a: int[] = [];
    a.push(3);
    a.push(6);
    let v: int[] = a.slice(0..2);
    let before: HeapStats = rt_heap_stats();
    let n: int = eat_view(v);
    let mid: HeapStats = rt_heap_stats();
    let kept: int = a[1];
    let w0: HeapStats = rt_heap_stats();
    @drop a;
    let w1: HeapStats = rt_heap_stats();
    if n != 2 { return 14; }
    if kept != 6 { return 15; }
    let r1: int = check_frees("view-param window", &before, &mid, 2:uint);
    if r1 != 0 { return 16; }
    return check_frees("post-view-param base window", &w0, &w1, 2:uint);
}

fn fixed_array_slice_drops_header_only() -> int {
    let data: int[4] = [1, 2, 3, 4];
    let v: int[] = data[[1..3]];
    let picked: int = v[0];
    if picked != 2 { return 13; }
    let before: HeapStats = rt_heap_stats();
    @drop v;
    let after: HeapStats = rt_heap_stats();
    return check_frees("fixed-slice window", &before, &after, 1:uint);
}

@entrypoint
fn main() -> int {
    let r1: int = string_drop_frees_once();
    if r1 != 0 { return 10 + r1; }
    let r2: int = owned_array_drop_frees_data_and_header();
    if r2 != 0 { return 20 + r2; }
    let r3: int = moved_param_dropped_by_callee_only();
    if r3 != 0 { return 30 + r3; }
    let r4: int = borrowed_param_never_dropped();
    if r4 != 0 { return 40 + r4; }
    let r5: int = view_defers_base();
    if r5 != 0 { return 50 + r5; }
    let r6: int = nested_views_all_orders();
    if r6 != 0 { return 60 + r6; }
    let r7: int = empty_view_still_drops();
    if r7 != 0 { return 70 + r7; }
    let r8: int = moved_view_dropped_by_callee_leaves_base();
    if r8 != 0 { return 80 + r8; }
    let r9: int = fixed_array_slice_drops_header_only();
    if r9 != 0 { return 100 + r9; }
    print("drop-leaf-ok");
    return 0;
}
`

func TestRuntimeV2DropLeafReclamation(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2DropLeafSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, threads := range []string{"1", "2"} {
		t.Run(fmt.Sprintf("threads_%s", threads), func(t *testing.T) {
			env := overrideEnvVar(baseEnv, "SURGE_THREADS", threads)
			duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
			if result.exitCode != 0 {
				t.Fatalf(
					"drop leaf e2e failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode,
					duration,
					result.stdout,
					result.stderr,
				)
			}
			if !strings.Contains(result.stdout, "drop-leaf-ok") {
				t.Fatalf("drop leaf e2e missing completion marker; stdout=%q", result.stdout)
			}
		})
	}
}
