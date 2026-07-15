//go:build !golden

package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Scope-exit synthesis end to end: owned strings and arrays free WITHOUT
// an explicit @drop — at block ends, early returns, break/continue, and
// reassignment — while moved values never double-free.
//
// Measurement keeps Task 2's currency (exact free_count deltas around
// tight windows). Two windows ride an int-storage cost: `a.push(1)` and
// `a.slice(..)` move an int into typed array storage, and the free count
// of that path is pinned into the scope-end and view-order rows. Those
// two constants were re-pinned when small ints became inline (fixnum):
// the value crossing into typed storage is no longer a heap bignum, so
// the path frees a different number of blocks than it did while every
// int was boxed. The view-order row's real teeth are the deferral counter:
// reverse-declaration order drops the view BEFORE its base, so nothing
// defers. Loop rows compare a
// scenario against a twin with the droppable removed: the loop
// machinery's own alloc/free noise (bignum scratch in conditions and
// counters) cancels, and the difference is exactly the synthesized
// per-iteration reclamation.
const runtimeV2DropScopeExitSource = `
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

fn scope_end_leaf_drops() -> nothing {
    let s: string = "implicit";
    let mut a: int[] = [];
    a.push(1);
}

fn early_return_drops() -> nothing {
    let s: string = "returned-over";
    return;
}

fn eat(s: string) -> nothing {
}

fn wrap(s: string) -> string {
    return s;
}

fn moved_param_drops_in_callee_only() -> nothing {
    let s: string = "moved";
    eat(s);
}

fn shadow_keeps_both() -> nothing {
    let s: string = "first";
    {
        let s: string = "second";
    }
}

fn shadow_through_move() -> nothing {
    let s: string = "only";
    {
        let s: string = wrap(s);
    }
}

fn reassign_drops_old() -> nothing {
    let mut a: string = "old";
    a = "new";
}

fn reassign_suppressed_on_self_move() -> nothing {
    let mut b: string = "old";
    b = wrap(b);
}

fn view_then_base_scope_order() -> nothing {
    let mut a: int[] = [];
    a.push(1);
    let v: int[] = a.slice(0..1);
}

fn noop_probe() -> nothing {
}

fn check_balance(label: string, before: &HeapStats, after: &HeapStats, noise: uint, expected: uint) -> int {
    let allocs: uint = after.alloc_count - before.alloc_count;
    let frees: uint = after.free_count - before.free_count;
    if allocs - noise != frees {
        print(label);
        print((allocs - noise) to string);
        print(frees to string);
        return 1;
    }
    if frees != expected {
        print(label);
        print(frees to string);
        print(expected to string);
        return 2;
    }
    return 0;
}

fn concat_operands_reclaimed() -> nothing {
    let s: string = "left-" + "right";
}

fn make_owned() -> string {
    return "made";
}

fn discarded_result_reclaimed() -> nothing {
    make_owned();
}

fn read_str(s: &string) -> nothing {
}

fn borrowed_arg_temp_reclaimed() -> nothing {
    read_str("bor" + "rowed");
}

fn cond_temp_loop(n: int) -> nothing {
    let mut i: int = 0;
    while "ab" + "cd" != "zz" {
        i = i + 1;
        if i >= n { break; }
    }
}

fn cond_temp_loop_twin(n: int) -> nothing {
    let mut i: int = 0;
    while "abcd" != "zz" {
        i = i + 1;
        if i >= n { break; }
    }
}

fn build_string_array() -> string[] {
    let mut arr: string[] = [];
    arr.push("alpha");
    arr.push("beta");
    return arr;
}

fn generic_push_no_double_drop() -> int {
    let arr: string[] = build_string_array();
    let first: string = arr[0].__clone();
    if first != "alpha" { return 1; }
    return 0;
}

fn loop_with_body_local(n: int) -> nothing {
    let mut i: int = n;
    while i > 0 {
        let s: string = "per-iteration";
        i = i - 1;
    }
}

fn loop_twin(n: int) -> nothing {
    let mut i: int = n;
    while i > 0 {
        i = i - 1;
    }
}

fn loop_break_drops(n: int) -> nothing {
    let mut i: int = n;
    while i > 0 {
        let s: string = "break-path";
        break;
    }
}

fn loop_break_twin(n: int) -> nothing {
    let mut i: int = n;
    while i > 0 {
        break;
    }
}

fn loop_continue_drops(n: int) -> nothing {
    let mut i: int = n;
    while i > 0 {
        let s: string = "continue-path";
        i = i - 1;
        continue;
    }
}

fn loop_continue_twin(n: int) -> nothing {
    let mut i: int = n;
    while i > 0 {
        i = i - 1;
        continue;
    }
}

@entrypoint
fn main() -> int {
    let w0: HeapStats = rt_heap_stats();
    scope_end_leaf_drops();
    let w1: HeapStats = rt_heap_stats();
    early_return_drops();
    let w2: HeapStats = rt_heap_stats();
    moved_param_drops_in_callee_only();
    let w3: HeapStats = rt_heap_stats();
    shadow_keeps_both();
    let w4: HeapStats = rt_heap_stats();
    shadow_through_move();
    let w5: HeapStats = rt_heap_stats();
    reassign_drops_old();
    let w6: HeapStats = rt_heap_stats();
    reassign_suppressed_on_self_move();
    let w7: HeapStats = rt_heap_stats();
    let deferred_before: uint = rt_array_debug_deferred_base_drops();
    view_then_base_scope_order();
    let w8: HeapStats = rt_heap_stats();
    let deferred_after: uint = rt_array_debug_deferred_base_drops();

    let r1: int = check_frees("scope-end window", &w0, &w1, 6:uint);
    if r1 != 0 { return 11; }
    let r2: int = check_frees("early-return window", &w1, &w2, 1:uint);
    if r2 != 0 { return 12; }
    let r3: int = check_frees("param-move window", &w2, &w3, 1:uint);
    if r3 != 0 { return 13; }
    let r4: int = check_frees("shadow-two window", &w3, &w4, 2:uint);
    if r4 != 0 { return 14; }
    let r5: int = check_frees("shadow-move window", &w4, &w5, 1:uint);
    if r5 != 0 { return 15; }
    let r6: int = check_frees("reassign window", &w5, &w6, 2:uint);
    if r6 != 0 { return 16; }
    let r7: int = check_frees("reassign-suppressed window", &w6, &w7, 1:uint);
    if r7 != 0 { return 17; }
    let r8: int = check_frees("view-order window", &w7, &w8, 9:uint);
    if r8 != 0 { return 18; }
    if deferred_after != deferred_before {
        print("view dropped after its base: deferral counter moved");
        return 23;
    }

    let p0: HeapStats = rt_heap_stats();
    loop_with_body_local(5);
    let p1: HeapStats = rt_heap_stats();
    loop_twin(5);
    let p2: HeapStats = rt_heap_stats();
    let with_frees: uint = p1.free_count - p0.free_count;
    let twin_frees: uint = p2.free_count - p1.free_count;
    if with_frees - twin_frees != 5:uint {
        print("loop per-iteration diff");
        print((with_frees - twin_frees) to string);
        return 19;
    }
    let with_allocs: uint = p1.alloc_count - p0.alloc_count;
    let twin_allocs: uint = p2.alloc_count - p1.alloc_count;
    if with_allocs - twin_allocs != 5:uint {
        print("loop per-iteration alloc diff");
        print((with_allocs - twin_allocs) to string);
        return 20;
    }

    let b0: HeapStats = rt_heap_stats();
    loop_break_drops(3);
    let b1: HeapStats = rt_heap_stats();
    loop_break_twin(3);
    let b2: HeapStats = rt_heap_stats();
    let break_diff: uint = (b1.free_count - b0.free_count) - (b2.free_count - b1.free_count);
    if break_diff != 1:uint {
        print("break window diff");
        print(break_diff to string);
        return 21;
    }

    let c0: HeapStats = rt_heap_stats();
    loop_continue_drops(4);
    let c1: HeapStats = rt_heap_stats();
    loop_continue_twin(4);
    let c2: HeapStats = rt_heap_stats();
    let continue_diff: uint = (c1.free_count - c0.free_count) - (c2.free_count - c1.free_count);
    if continue_diff != 4:uint {
        print("continue window diff");
        print(continue_diff to string);
        return 22;
    }

    let g: int = generic_push_no_double_drop();
    if g != 0 { return 24; }

    let n0: HeapStats = rt_heap_stats();
    noop_probe();
    let n1: HeapStats = rt_heap_stats();
    let noise: uint = (n1.alloc_count - n0.alloc_count) - (n1.free_count - n0.free_count);

    let t0: HeapStats = rt_heap_stats();
    concat_operands_reclaimed();
    let t1: HeapStats = rt_heap_stats();
    discarded_result_reclaimed();
    let t2: HeapStats = rt_heap_stats();
    borrowed_arg_temp_reclaimed();
    let t3: HeapStats = rt_heap_stats();

    let rb1: int = check_balance("concat-balance window", &t0, &t1, noise, 3:uint);
    if rb1 != 0 { return 30 + rb1; }
    let rb2: int = check_balance("discard-balance window", &t1, &t2, noise, 1:uint);
    if rb2 != 0 { return 40 + rb2; }
    let rb3: int = check_balance("borrowed-temp window", &t2, &t3, noise, 3:uint);
    if rb3 != 0 { return 50 + rb3; }

    let l0: HeapStats = rt_heap_stats();
    cond_temp_loop(4);
    let l1: HeapStats = rt_heap_stats();
    cond_temp_loop_twin(4);
    let l2: HeapStats = rt_heap_stats();
    let cond_diff: uint = (l1.free_count - l0.free_count) - (l2.free_count - l1.free_count);
    if cond_diff != 8:uint {
        print("cond-temp per-iteration diff");
        print(cond_diff to string);
        return 60;
    }

    print("drop-scope-exit-ok");
    return 0;
}
`

func TestRuntimeV2DropScopeExit(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2DropScopeExitSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, threads := range []string{"1", "2"} {
		t.Run(fmt.Sprintf("threads_%s", threads), func(t *testing.T) {
			env := overrideEnvVar(baseEnv, "SURGE_THREADS", threads)
			duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
			if result.exitCode != 0 {
				t.Fatalf(
					"drop scope-exit e2e failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode,
					duration,
					result.stdout,
					result.stderr,
				)
			}
			if !strings.Contains(result.stdout, "drop-scope-exit-ok") {
				t.Fatalf("drop scope-exit e2e missing completion marker; stdout=%q", result.stdout)
			}
		})
	}
}
