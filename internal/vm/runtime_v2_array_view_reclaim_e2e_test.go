package vm_test

import (
	"fmt"
	"testing"
	"time"
)

// A slice MINTS a view header, and whoever holds it owns it.
//
// `array_alloc_view` allocates a fresh 24-byte header whose data pointer aims
// into the source. sema classified `let v = xs[[1..3]]` as a projection read
// and marked the binding an alias, so no drop was recorded and the header
// leaked — on BOTH lanes and for BOTH array kinds. `temp_drops.go` already said
// the opposite about the same expression, which is why the temporary form
// `consume(xs[[1..3]])` reclaimed its header while the bound form did not.
//
// THIS IS A DIFFERENTIAL, not an absolute. One block per slice still leaks and
// is a different defect: the `Range` the index path materialises has no
// `rt_range_free` in the runtime at all, so nothing can release it yet. An
// absolute strict-zero assertion here would be red for a reason this test does
// not own. What it pins is the SLOPE: one leaked block per slice, not two.
const runtimeV2ArrayViewReclaimSourceFmt = `
@entrypoint
fn main() -> int {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut i: int = 0;
    let mut acc: int = 0;
    while i < %d {
        let v = xs[[1..3]];
        acc = acc + (v.__len() to int);
        i = i + 1;
    }
    if acc == %d {
        print("array-view-reclaim-witness");
        return 0;
    }
    return 1;
}
`

func TestRuntimeV2ArrayViewHeaderReclaimedPerSlice(t *testing.T) {
	measure := func(slices int) (blocks int) {
		t.Helper()
		src := fmt.Sprintf(runtimeV2ArrayViewReclaimSourceFmt, slices, slices*2)
		outputPath := buildRuntimeV2CrossingSource(t, src, nil)
		env := envWithStdlib(repoRoot(t))
		stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
		if hasValgrindMemcheckError(stderr) {
			t.Fatalf("slicing a fixed array hit a memcheck error at %d slices\nstdout:\n%s\nstderr:\n%s", slices, stdout, stderr)
		}
		if exitCode != 0 {
			t.Fatalf("array-view program failed at %d slices (exit=%d)\nstdout:\n%s\nstderr:\n%s", slices, exitCode, stdout, stderr)
		}
		_, blocksLost, err := parseValgrindDefinitelyLost(stderr)
		if err != nil {
			t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
		}
		return blocksLost
	}

	// Two runs that differ ONLY in how many slices are taken. Whatever leaks
	// once per program cancels; what is left is the per-slice cost.
	at8 := measure(8)
	at16 := measure(16)
	perSlice := float64(at16-at8) / 8.0

	// One block per slice is the Range temporary and is RV2-DEBT-215/216. Two
	// was that plus the view header, which is what this pins.
	if at16-at8 != 8 {
		t.Fatalf(
			"a slice leaks %.2f blocks, want 1 (the Range temporary alone)\n"+
				"8 slices lost %d blocks, 16 slices lost %d.\n"+
				"Two per slice means the view header stopped being reclaimed: a slice MINTS "+
				"that header and the binding holding it owns it.",
			perSlice, at8, at16,
		)
	}
}
