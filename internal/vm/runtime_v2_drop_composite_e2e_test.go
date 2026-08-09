//go:build !golden

package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Recursive composite drop glue: dropping a struct / tuple / union /
// fixed array / array-of-droppables frees its owning fields and elements,
// not just the outermost one. Assertions ride exact free_count deltas
// around drop-only windows (nothing frees spontaneously) minus the cost of
// the measurement itself: each `let x: HeapStats = rt_heap_stats()`
// allocates a snapshot box, copies it into the local and reclaims the box
// inside the window its own probe opened.
//
// A composite value itself costs the heap nothing — it lives in ordinary
// storage, so only its droppable leaves (strings, dynamic-array buffers
// and headers) are blocks. value_composite_costs_no_block pins that,
// because every count below is stated in leaves alone.
const runtimeV2DropCompositeSource = `
type Inner = { label: string }
type User = { name: string, note: string }
type Outer = { inner: Inner, items: string[] }
type FixedItem = { name: string, id: int }
type FixedStrideItem = { left: int, right: int }
type Pair = { left: int, right: int }

fn probe_window_cost() -> uint {
    let a: HeapStats = rt_heap_stats();
    let b: HeapStats = rt_heap_stats();
    return b.free_count - a.free_count;
}

fn check_frees(win: string, before: &HeapStats, after: &HeapStats, want: uint) -> int {
    let raw: uint = after.free_count - before.free_count;
    let cost: uint = probe_window_cost();
    if raw < cost {
        print(win);
        print("window freed less than its own probe");
        return 2;
    }
    let f: uint = raw - cost;
    if f != want {
        print(win);
        print(f to string);
        print(want to string);
        return 1;
    }
    return 0;
}

fn value_composite_costs_no_block() -> int {
    let before: HeapStats = rt_heap_stats();
    let p: Pair = Pair { left: 1, right: 2 };
    @drop p;
    let after: HeapStats = rt_heap_stats();
    let cost: uint = probe_window_cost();
    let allocs: uint = after.alloc_count - before.alloc_count;
    let frees: uint = after.free_count - before.free_count;
    if allocs != frees {
        print("value-composite window unbalanced");
        print(allocs to string);
        print(frees to string);
        return 1;
    }
    if frees != cost {
        print("value-composite reached the heap");
        print(frees to string);
        print(cost to string);
        return 2;
    }
    return 0;
}

fn struct_drop() -> int {
    let u: User = User { name: "alice", note: "friend" };
    let before: HeapStats = rt_heap_stats();
    @drop u;                       // name + note
    let after: HeapStats = rt_heap_stats();
    return check_frees("struct", &before, &after, 2:uint);
}

fn tuple_drop() -> int {
    let t: (string, string) = ("first", "second");
    let before: HeapStats = rt_heap_stats();
    @drop t;                       // two strings
    let after: HeapStats = rt_heap_stats();
    return check_frees("tuple", &before, &after, 2:uint);
}

fn option_some_drop() -> int {
    let o: string? = Some("payload");
    let before: HeapStats = rt_heap_stats();
    @drop o;                       // payload string
    let after: HeapStats = rt_heap_stats();
    return check_frees("option-some", &before, &after, 1:uint);
}

fn option_none_drop() -> int {
    let o: string? = nothing;
    let before: HeapStats = rt_heap_stats();
    @drop o;                       // no payload, nothing to reclaim
    let after: HeapStats = rt_heap_stats();
    return check_frees("option-none", &before, &after, 0:uint);
}

fn fixed_array_drop() -> int {
    let a: string[3] = ["u", "v", "w"];
    let before: HeapStats = rt_heap_stats();
    @drop a;                       // three element strings
    let after: HeapStats = rt_heap_stats();
    return check_frees("fixed-array", &before, &after, 3:uint);
}

fn fixed_composite_array_drop() -> int {
    let a: FixedItem[2] = [
        FixedItem { name: "left", id: 1 },
        FixedItem { name: "right", id: 2 },
    ];
    let before: HeapStats = rt_heap_stats();
    @drop a;                       // one string per element
    let after: HeapStats = rt_heap_stats();
    return check_frees("fixed-composite-array", &before, &after, 2:uint);
}

fn fixed_composite_array_default_drop() -> int {
    let a: FixedItem[2] = default::<FixedItem[2]>();
    let before: HeapStats = rt_heap_stats();
    @drop a;                       // one empty string per element
    let after: HeapStats = rt_heap_stats();
    return check_frees("fixed-composite-array-default", &before, &after, 2:uint);
}

fn fixed_composite_array_index_iter() -> int {
    let a: FixedStrideItem[2] = [
        FixedStrideItem { left: 1, right: 2 },
        FixedStrideItem { left: 3, right: 4 },
    ];
    if a[1].right != 4 { return 1; }
    let mut sum: int = 0;
    for item in a {
        sum = sum + item.left;
    }
    if sum != 4 { return 2; }
    return 0;
}

fn string_array_drop() -> int {
    let mut arr: string[] = [];
    arr.push("one");
    arr.push("two");
    arr.push("three");
    let before: HeapStats = rt_heap_stats();
    @drop arr;                     // 3 elements + data + header
    let after: HeapStats = rt_heap_stats();
    return check_frees("string-array", &before, &after, 5:uint);
}

fn array_of_structs_drop() -> int {
    let mut arr: Inner[] = [];
    arr.push(Inner { label: "a" });
    arr.push(Inner { label: "b" });
    let before: HeapStats = rt_heap_stats();
    @drop arr;                     // 2 labels + data + header
    let after: HeapStats = rt_heap_stats();
    return check_frees("array-of-structs", &before, &after, 4:uint);
}

fn nested_drop() -> int {
    let mut items: string[] = [];
    items.push("x");
    items.push("y");
    let o: Outer = Outer { inner: Inner { label: "deep" }, items: items };
    let before: HeapStats = rt_heap_stats();
    @drop o;                       // inner label + items(x, y, data, header)
    let after: HeapStats = rt_heap_stats();
    return check_frees("nested", &before, &after, 5:uint);
}

fn array_with_view_drop() -> int {
    let mut arr: string[] = [];
    arr.push("p");
    arr.push("q");
    arr.push("r");
    let v: string[] = arr.slice(0..2);
    let d0: HeapStats = rt_heap_stats();
    @drop arr;                     // deferred: base has a live view
    let d1: HeapStats = rt_heap_stats();
    let r1: int = check_frees("view-defer", &d0, &d1, 0:uint);
    if r1 != 0 { return r1; }
    let still: int = v[1].__len() to int;
    if still != 1 { return 9; }
    let d2: HeapStats = rt_heap_stats();
    @drop v;                       // last view: 3 elems + data + base hdr + link + orphan + view hdr
    let d3: HeapStats = rt_heap_stats();
    return check_frees("view-last", &d2, &d3, 8:uint);
}

@entrypoint
fn main() -> int {
    let r0: int = value_composite_costs_no_block();
    if r0 != 0 { return 5 + r0; }
    let r1: int = struct_drop();
    if r1 != 0 { return 10 + r1; }
    let r2: int = tuple_drop();
    if r2 != 0 { return 20 + r2; }
    let r3: int = option_some_drop();
    if r3 != 0 { return 30 + r3; }
    let r4: int = option_none_drop();
    if r4 != 0 { return 40 + r4; }
    let r5: int = fixed_array_drop();
    if r5 != 0 { return 50 + r5; }
    let r10: int = fixed_composite_array_drop();
    if r10 != 0 { return 55 + r10; }
    let r11: int = fixed_composite_array_default_drop();
    if r11 != 0 { return 57 + r11; }
    let r12: int = fixed_composite_array_index_iter();
    if r12 != 0 { return 58 + r12; }
    let r6: int = string_array_drop();
    if r6 != 0 { return 60 + r6; }
    let r7: int = array_of_structs_drop();
    if r7 != 0 { return 70 + r7; }
    let r8: int = nested_drop();
    if r8 != 0 { return 80 + r8; }
    let r9: int = array_with_view_drop();
    if r9 != 0 { return 90 + r9; }
    print("composite-drop-ok");
    return 0;
}
`

func TestRuntimeV2DropComposite(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2DropCompositeSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, threads := range []string{"1", "2"} {
		t.Run(fmt.Sprintf("threads_%s", threads), func(t *testing.T) {
			env := overrideEnvVar(baseEnv, "SURGE_THREADS", threads)
			_, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
			if result.exitCode != 0 {
				t.Fatalf("composite-drop e2e failed (exit=%d)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode, result.stdout, result.stderr)
			}
			if !strings.Contains(result.stdout, "composite-drop-ok") {
				t.Fatalf("composite-drop e2e missing marker; stdout=%q", result.stdout)
			}
		})
	}
}
