package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A map's value is stored at its own type, so a value WIDER than a machine
// word no longer costs a block of its own.
//
// The representation this replaces held two words per entry. A composite value
// did not fit in one, so every insert copied it into a separate transport
// allocation and stored that allocation's address; every removal read it back
// out and freed the block. One heap allocation per insert, on a value whose
// fields are plain integers and which touches the heap nowhere else.
//
// This windows that allocation directly. Both probes replace an existing entry
// in a map whose storage is already large enough, so neither grows and neither
// adds an entry; they differ only in the WIDTH of the value:
//
//	narrow: one int -- fits a word, so it cost nothing before and costs
//	        nothing now. It is the control: if the per-iteration figure moved
//	        here too, the difference below would be about map machinery rather
//	        than about where a value lives.
//	wide:   two ints -- did NOT fit a word. Its block is what disappeared.
//
// Each figure is the allocation count over a window, and the property is
// asserted as an EQUALITY between the two probes at two window sizes rather
// than as a pinned absolute. The absolute is map machinery, which this step
// does not claim to have changed; the difference is the box, which it removed.
const runtimeV2MapEntryCensusSource = `
type Pair = { a: int, b: int };

fn narrow_window(n: int) -> uint {
    let mut m: Map<int, int> = Map::<int, int>.new();
    // Fill first, so the window measures replacement and not growth: a map
    // that reallocated inside the window would report the move of every live
    // entry as well as the entry under test.
    let mut warm: int = 0;
    while warm < 8 {
        let _ = m.insert(warm, warm);
        warm = warm + 1;
    }
    let c0: HeapStats = rt_heap_stats();
    let mut i: int = 0;
    let mut acc: int = 0;
    while i < n {
        acc = acc + compare m.insert(i, i + 100) {
            Some(previous) => previous;
            nothing => 0 - 1000;
        };
        i = i + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    if acc < 0 { return 999998; }
    return c1.alloc_count - c0.alloc_count;
}

fn wide_window(n: int) -> uint {
    let mut m: Map<int, Pair> = Map::<int, Pair>.new();
    let mut warm: int = 0;
    while warm < 8 {
        let _ = m.insert(warm, Pair { a = warm, b = warm });
        warm = warm + 1;
    }
    let c0: HeapStats = rt_heap_stats();
    let mut i: int = 0;
    let mut acc: int = 0;
    while i < n {
        acc = acc + compare m.insert(i, Pair { a = i, b = i + 100 }) {
            Some(previous) => previous.b;
            nothing => 0 - 1000;
        };
        i = i + 1;
    }
    let c1: HeapStats = rt_heap_stats();
    if acc < 0 { return 999997; }
    return c1.alloc_count - c0.alloc_count;
}

fn report(label: string, narrow: uint, wide: uint) -> int {
    print("FAIL map entry census ");
    print(label);
    print(" narrow=");
    print(narrow to string);
    print(" wide=");
    print(wide to string);
    return 0 - 1;
}

@entrypoint
fn main() -> int {
    let n1: uint = narrow_window(1);
    let n8: uint = narrow_window(8);
    let w1: uint = wide_window(1);
    let w8: uint = wide_window(8);
    if n1 >= 999000 || n8 >= 999000 || w1 >= 999000 || w8 >= 999000 {
        print("FAIL map entry value");
        return 1;
    }
    print("map entry census narrow one=");
    print(n1 to string);
    print(" eight=");
    print(n8 to string);
    print("map entry census wide one=");
    print(w1 to string);
    print(" eight=");
    print(w8 to string);

    // THE PROPERTY: a composite value costs exactly what a scalar one costs,
    // at both window sizes. Both live in the map's own value run, and neither
    // takes a block of its own.
    if n1 != w1 { return report("one-iteration", n1, w1); }
    if n8 != w8 { return report("eight-iterations", n8, w8); }

    print("map-entry-census-ok");
    return 0;
}
`

// Expected on the tree without the storage flip: the wide probe allocates one
// transport block per replaced entry that the narrow one does not, so the
// eight-iteration comparison reports narrow=0 wide=8 and the test fails at
// `eight-iterations`. To be measured by the lead.
func TestRuntimeV2MapEntryCensusBalanced(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2MapEntryCensusSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	duration, result := runBinaryWithTimeout(t, outputPath, baseEnv, 30*time.Second)
	if result.exitCode != 0 {
		t.Fatalf("map entry census failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode, duration, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "map-entry-census-ok") {
		t.Fatalf("map entry census missing completion marker; stdout=%q", result.stdout)
	}
}
