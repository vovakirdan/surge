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
// then the box — not just the leaves. Assertions ride exact free_count
// deltas around drop-only windows (nothing frees spontaneously).
const runtimeV2DropCompositeSource = `
type Inner = { label: string }
type User = { name: string, note: string }
type Outer = { inner: Inner, items: string[] }

fn check_frees(win: string, before: &HeapStats, after: &HeapStats, want: uint) -> int {
    let f: uint = after.free_count - before.free_count;
    if f != want {
        print(win);
        print(f to string);
        print(want to string);
        return 1;
    }
    return 0;
}

fn struct_drop() -> int {
    let u: User = User { name: "alice", note: "friend" };
    let before: HeapStats = rt_heap_stats();
    @drop u;                       // name + note + box
    let after: HeapStats = rt_heap_stats();
    return check_frees("struct", &before, &after, 3:uint);
}

fn tuple_drop() -> int {
    let t: (string, string) = ("first", "second");
    let before: HeapStats = rt_heap_stats();
    @drop t;                       // two strings + box
    let after: HeapStats = rt_heap_stats();
    return check_frees("tuple", &before, &after, 3:uint);
}

fn option_some_drop() -> int {
    let o: string? = Some("payload");
    let before: HeapStats = rt_heap_stats();
    @drop o;                       // payload string + box
    let after: HeapStats = rt_heap_stats();
    return check_frees("option-some", &before, &after, 2:uint);
}

fn option_none_drop() -> int {
    let o: string? = nothing;
    let before: HeapStats = rt_heap_stats();
    @drop o;                       // box only, no payload
    let after: HeapStats = rt_heap_stats();
    return check_frees("option-none", &before, &after, 1:uint);
}

fn fixed_array_drop() -> int {
    let a: string[3] = ["u", "v", "w"];
    let before: HeapStats = rt_heap_stats();
    @drop a;                       // three element strings + box
    let after: HeapStats = rt_heap_stats();
    return check_frees("fixed-array", &before, &after, 4:uint);
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
    @drop arr;                     // 2*(label + box) + data + header
    let after: HeapStats = rt_heap_stats();
    return check_frees("array-of-structs", &before, &after, 6:uint);
}

fn nested_drop() -> int {
    let mut items: string[] = [];
    items.push("x");
    items.push("y");
    let o: Outer = Outer { inner: Inner { label: "deep" }, items: items };
    let before: HeapStats = rt_heap_stats();
    @drop o;                       // inner(label+box) + items(x,y,data,header) + outer box
    let after: HeapStats = rt_heap_stats();
    return check_frees("nested", &before, &after, 7:uint);
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
