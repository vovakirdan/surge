package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// A COMPOUND ASSIGNMENT FREES WHAT IT DISPLACES, on every target shape.
//
// It used to free nothing, at any target. `s += "x"` leaked the old string
// exactly as `s = s + "x"` once did, and so did `it.name += "x"` - identical
// numbers for the two spellings, which is what said this was not place-shaped
// and not a tail of RV2-DEBT-208.
//
// It was a THIRD lowering path and the record was thrown away twice on the way
// down. Sema already recorded the obligation for a compound whole-binding store
// (`handleAssignment` has no operator test), HIR discarded it because
// `lowerCompoundAssignExpr` had nowhere to discharge it, and the PLACE form was
// gated out in sema for the same stated reason. Removing either gate alone does
// nothing; the missing half was always the discharge.
//
// WHERE the drop goes is the whole of it. The compound form READS its target
// before it writes it, and for a string the backend hands the callee the ADDRESS
// OF THE SLOT rather than a loaded handle - so a drop before the operation is a
// use-after-free the runtime performs itself, not an ordering nit. After the
// operation the old value is dead and the result lives in a fresh temp.
//
// THE SELF-COMPOUND SHAPE IS IN HERE ON PURPOSE. `u += u` is the compound
// analogue of `x = x`, the program that forced `materializeBeforeOverwrite` onto
// the plain `=` path. It needs nothing here, because the store's source is
// always a temp and never the destination - and a fixture is cheaper than
// trusting that sentence.
const runtimeV2CompoundAssignSourceFmt = `
type Item = { name: string, id: int };

fn mk(n: int) -> string {
    return "displaced-value-long-enough-to-reach-the-heap-" + (n to string);
}

fn through_ref(target: &mut string) -> nothing {
    *target += "r";
    return nothing;
}

@entrypoint
fn main() -> int {
    let mut acc: int = 0;
    let mut i: int = 0;
    while i < %d {
        let mut s: string = mk(0);
        s += "x";
        acc = acc + (s.__len() to int);

        let mut u: string = mk(0);
        u += u;
        acc = acc + (u.__len() to int);

        let mut it: Item = Item { name = mk(0), id = 0 };
        it.name += "y";
        acc = acc + (it.name.__len() to int);

        let mut t: (string, int) = (mk(0), 0);
        t.0 += "z";
        acc = acc + (t.0.__len() to int);

        let mut w: string = mk(0);
        through_ref(&mut w);
        acc = acc + (w.__len() to int);

        // Owns nothing: a drop recorded for this would be an obligation on a
        // value that never touched the heap.
        let mut n: int = 1;
        n += 2;
        acc = acc + n;

        let mut xs: int[] = [];
        xs.push(1);
        let mut ys: int[] = [];
        ys.push(2);
        xs += ys;
        acc = acc + (xs.__len() to int);

        i = i + 1;
    }
    if acc > 0 {
        print("compound-assign-witness");
        return 0;
    }
    return 1;
}
`

func TestRuntimeV2CompoundAssignReleasesDisplacedValue(t *testing.T) {
	measure := func(iterations int) (bytes int, blocks int) {
		t.Helper()
		src := fmt.Sprintf(runtimeV2CompoundAssignSourceFmt, iterations)
		outputPath := buildRuntimeV2CrossingSource(t, src, nil)
		env := envWithStdlib(repoRoot(t))
		stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
		// Asked before the leak numbers, and it is the assertion that can only
		// fail LOUDLY: a drop placed before the operation that still reads the
		// target frees storage the runtime is about to dereference, and that
		// shows up here rather than as a leak.
		if hasValgrindMemcheckError(stderr) {
			t.Fatalf(
				"compound assignment hit a memcheck error at %d iterations - a drop emitted "+
					"before the binary operation looks exactly like this\nstdout:\n%s\nstderr:\n%s",
				iterations, stdout, stderr)
		}
		if exitCode != 0 {
			t.Fatalf("compound-assign program failed at %d iterations (exit=%d)\nstdout:\n%s\nstderr:\n%s",
				iterations, exitCode, stdout, stderr)
		}
		if !strings.Contains(stdout, "compound-assign-witness") {
			t.Fatalf("compound-assign program missing completion marker; stdout=%q", stdout)
		}
		lostBytes, lostBlocks, err := parseValgrindDefinitelyLost(stderr)
		if err != nil {
			t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
		}
		return lostBytes, lostBlocks
	}

	bytes4, blocks4 := measure(4)
	bytes8, blocks8 := measure(8)

	if bytes4 != 0 || blocks4 != 0 || bytes8 != 0 || blocks8 != 0 {
		perIter := float64(blocks8-blocks4) / 4.0
		t.Fatalf(
			"compound assignment leaks: 4 iterations lost %d bytes in %d blocks, 8 lost %d in %d, "+
				"want zero. Per iteration that is %.2f blocks; SIX of the seven targets own heap, "+
				"so 6 blocks/iteration means none of them releases and a smaller number names how "+
				"many shapes the fix reaches.",
			bytes4, blocks4, bytes8, blocks8, perIter,
		)
	}
}
