package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// A STRING SLICE MINTS A WHOLE STRING, and whoever receives it owns it.
//
// `rt_string_slice` does not point into its source the way an array view does -
// it builds a new string with `rt_string_from_bytes` - and nothing freed it.
// sema classified `let t = s[[1..3]]` as a projection read, marked the binding
// an alias, and both backends faithfully emitted nothing.
//
// This is the same shape RV2-DEBT-206 closed for arrays, in the same predicate,
// and it was never looked for in strings. The two differ in one way worth
// keeping: for an ARRAY the bound form and the temporary form DISAGREED, so only
// the bound one leaked; for a STRING both sites asked the same
// `isArrayViewExpr` and both were wrong, so a temporary nobody takes leaked too.
// One predicate - `mintsOwnedValue` - now answers for both.
//
// THE PROGRAM IS A CENSUS OF KINDS, not an example. Every entry below is a
// different thing that can happen to a value the binding now owns, and the ones
// that matter are the ones that could DOUBLE free rather than leak: moved into a
// callee, borrowed, sliced again, returned, and reassigned over. A leak shows up
// in the numbers; a double free shows up as a memcheck error, which is checked
// first.
const runtimeV2StringSliceReclaimSourceFmt = `
fn consume(t: string) -> int { return (t.__len() to int); }
fn borrowed(t: &string) -> int { return (rt_string_len(t) to int); }

fn returns_slice(s: &string) -> string {
    return s[[1..3]];
}

@entrypoint
fn main() -> int {
    let s: string = "abcdefghij";
    let mut n: int = 0;
    let mut i: int = 0;
    while i < %d {
        let a = s[[1..3]];
        n = n + (a.__len() to int);

        let b = s[[1..3]];
        n = n + consume(b);

        let c = s[[1..3]];
        n = n + borrowed(&c);

        // A temporary nobody takes: only the owned-temp machinery can free it,
        // and it asked the same wrong predicate the bound form did.
        n = n + (s[[3..5]].__len() to int);

        n = n + consume(s[[2..4]]);

        let d = s[[0..6]];
        let e = d[[1..3]];
        n = n + (e.__len() to int);

        let f = returns_slice(&s);
        n = n + (f.__len() to int);

        // Reassigned over: the store has to free the slice it displaces.
        let mut g = s[[0..2]];
        g = s[[2..4]];
        n = n + (g.__len() to int);

        // An element read mints nothing and must stay an alias; a drop recorded
        // here would free a character the string still owns.
        let h = s[0];
        if h != 0 { n = n + 1; }

        i = i + 1;
    }
    if n > 0 {
        print("string-slice-reclaim-witness");
        return 0;
    }
    return 1;
}
`

func TestRuntimeV2StringSliceReclaimed(t *testing.T) {
	measure := func(iterations int) (bytes int, blocks int) {
		t.Helper()
		src := fmt.Sprintf(runtimeV2StringSliceReclaimSourceFmt, iterations)
		outputPath := buildRuntimeV2CrossingSource(t, src, nil)
		env := envWithStdlib(repoRoot(t))
		stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
		if hasValgrindMemcheckError(stderr) {
			t.Fatalf(
				"a sliced string was freed twice or read after free at %d iterations - the "+
					"moved, borrowed, re-sliced and reassigned entries are the ones that can do "+
					"that\nstdout:\n%s\nstderr:\n%s",
				iterations, stdout, stderr)
		}
		if exitCode != 0 {
			t.Fatalf("string-slice program failed at %d iterations (exit=%d)\nstdout:\n%s\nstderr:\n%s",
				iterations, exitCode, stdout, stderr)
		}
		if !strings.Contains(stdout, "string-slice-reclaim-witness") {
			t.Fatalf("string-slice program missing completion marker; stdout=%q", stdout)
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
			"sliced strings leak: 4 iterations lost %d bytes in %d blocks, 8 lost %d in %d, "+
				"want zero. Per iteration that is %.2f blocks, and the count names which sites "+
				"stopped: the bound forms go through projectionReadAliasesItsSource, the "+
				"unclaimed temporary through temp_drops.go, and both ask mintsOwnedValue.",
			bytes4, blocks4, bytes8, blocks8, perIter,
		)
	}
}
