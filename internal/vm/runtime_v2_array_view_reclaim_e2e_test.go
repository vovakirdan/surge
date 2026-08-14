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
// IT IS NOW AN ABSOLUTE, and it was a differential before for a reason that no
// longer holds. One block per slice used to leak on top of the header: the
// `Range` the index path materialises reached no release at all, so an absolute
// assertion would have been red for a defect this test does not own. That was
// RV2-DEBT-215/216 and it is closed - a range moved into a slice sink is freed
// at the call site, and a range a binding owns is freed by `emitInstrDrop` -
// so this test asserts strict zero and keeps the slope assertion underneath it.
// The slope is what tells the two defects apart if either comes back: a header
// regression moves the slope to 1, a Range regression moves it to 1 as well but
// with the block allocated in `rt_range.c` rather than `array_alloc_view`.
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

	// The absolute. Slicing a fixed array in a loop owes nothing at exit: the
	// view header belongs to the binding and the Range to whoever the index
	// path handed it to, and both are released now.
	if at8 != 0 || at16 != 0 {
		t.Fatalf(
			"slicing leaks: 8 slices lost %d blocks, 16 slices lost %d, want 0 for both.\n"+
				"Per-slice slope is %.2f. A slope of 1 says one of the two owners stopped "+
				"releasing - check whether the block was allocated by array_alloc_view (the "+
				"header, RV2-DEBT-206) or by alloc_range in rt_range.c (the Range, RV2-DEBT-215/216).",
			at8, at16, perSlice,
		)
	}

	// The slope, kept underneath the absolute because it survives a change in
	// whatever leaks once per program and would name the defect on its own.
	if at16-at8 != 0 {
		t.Fatalf(
			"a slice leaks %.2f blocks, want 0\n8 slices lost %d blocks, 16 slices lost %d.",
			perSlice, at8, at16,
		)
	}
}
