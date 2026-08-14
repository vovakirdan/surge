package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// A `for-in` OVER A TEMPORARY MUST KEEP THAT TEMPORARY ALIVE, and it did not.
//
// `for v in build()` evaluates something nobody names. The statement-end
// machinery freed it at the end of the `let __iter = iter_init(...)` statement
// the for-in normalization builds — which is BEFORE the loop that reads through
// the cursor it just made. MIR showed it in three lines: `L2 = call build()`,
// `L4 = iter_init copy L3`, `drop L3`, all in the entry block.
//
// NO SLICE, VIEW OR RANGE IS INVOLVED. An ordinary array returned by an ordinary
// function does it, which is why this outranked the escaping-view row it was
// found under (RV2-DEBT-218 was re-attributed to this).
//
// It was invisible because `for x in <call>` appears nowhere in the corpus, and
// silent because the count is right and only the values are wrong: with five
// elements the loop reported 5 and returned the last three correctly while the
// first two came back as garbage — the allocator had already handed the head of
// the block to somebody else.
//
// THE PROGRAM IS A CENSUS OF ITERABLE KINDS, not an example, because the fix
// binds the iterable to a synthetic name and a binding that frees something it
// does not own is worse than the leak. Iterating a PLAIN BINDING twice is in
// here for exactly that reason: if the loop ever took ownership of `xs`, the
// second pass would read freed storage.
const runtimeV2ForInTemporarySourceFmt = `
type Item = { n: int };

fn owned_array() -> int[] {
    let mut p: int[] = [];
    p.push(1); p.push(2);
    return p;
}

fn cursor_over_caller(xs: &int[]) -> Range<int> {
    return xs.__range();
}

fn sum_ref(xs: &mut Item[]) -> int {
    let mut s: int = 0;
    for it in xs {
        s = s + it.n;
    }
    return s;
}

fn early_return() -> int {
    for v in owned_array() {
        if v == 2 {
            return v;
        }
    }
    return 0;
}

fn with_break() -> int {
    let mut s: int = 0;
    for v in owned_array() {
        s = s + v;
        if v == 1 {
            break;
        }
    }
    return s;
}

fn nested(rounds: int) -> int {
    let mut s: int = 0;
    let mut i: int = 0;
    while i < rounds {
        for v in owned_array() {
            s = s + v;
        }
        i = i + 1;
    }
    return s;
}

fn inner_array(k: int) -> int[] {
    let mut p: int[] = [];
    p.push(k * 10);
    return p;
}

// Two hoists at once, and a return that escapes BOTH loops - each has its own
// binding to release, innermost first.
fn nested_temporaries() -> int {
    let mut s: int = 0;
    for a in owned_array() {
        for b in inner_array(a) {
            s = s + b;
        }
    }
    return s;
}

fn return_through_two_loops() -> int {
    for a in owned_array() {
        for b in inner_array(a) {
            if b >= 20 {
                return b;
            }
        }
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let mut total: int = 0;

    // The defect itself: a call that returns an array it owns.
    for v in owned_array() { total = total + v; }

    // A cursor over the CALLER's array: correct before and after, and it must
    // stay that way - the temporary here owns nothing.
    let mut xs: int[] = [];
    xs.push(10); xs.push(20);
    for v in cursor_over_caller(&xs) { total = total + v; }

    // A plain binding, iterated TWICE. If the loop ever took ownership of it,
    // the second pass reads freed storage.
    for v in xs { total = total + v; }
    for v in xs { total = total + v; }

    let mut items: Item[] = [];
    items.push(Item { n = 5 });
    total = total + sum_ref(&mut items);

    for i in 0..3 { total = total + i; }
    for v in xs.__range() { total = total + v; }

    // A return out of the loop body, a break, and a call in an outer loop:
    // three exits that do not pass through the point after the loop.
    total = total + early_return();
    total = total + with_break();
    total = total + nested_temporaries();
    total = total + return_through_two_loops();
    total = total + nested(%d);

    print("for-in-temporary-witness");
    print(total to string);
    return 0;
}
`

func TestRuntimeV2ForInOverTemporaryKeepsItAlive(t *testing.T) {
	// 3 from the owned array, 30 from the caller's cursor, 30 + 30 from the two
	// plain-binding passes, 5 through the &mut parameter, 3 from the numeric
	// range, 30 from the local cursor, 2 from the early return, 1 from the
	// break, 10 + 20 from the two nested temporaries, 20 from the return that
	// escapes both of them, and 3 per nested round.
	const fixedTotal = 3 + 30 + 30 + 30 + 5 + 3 + 30 + 2 + 1 + 30 + 20

	run := func(rounds int) (stdout string, bytes, blocks int) {
		t.Helper()
		src := fmt.Sprintf(runtimeV2ForInTemporarySourceFmt, rounds)
		outputPath := buildRuntimeV2CrossingSource(t, src, nil)
		env := envWithStdlib(repoRoot(t))
		out, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
		// Asked first, and it is the half that used to fail: the loop read
		// storage the entry block had already freed.
		if hasValgrindMemcheckError(stderr) {
			t.Fatalf(
				"for-in over a temporary hit a memcheck error at %d nested rounds - reading a "+
					"freed iterable, or freeing one the program still owns, both look like "+
					"this\nstdout:\n%s\nstderr:\n%s",
				rounds, out, stderr)
		}
		if exitCode != 0 {
			t.Fatalf("for-in program failed at %d rounds (exit=%d)\nstdout:\n%s\nstderr:\n%s",
				rounds, exitCode, out, stderr)
		}
		if !strings.Contains(out, "for-in-temporary-witness") {
			t.Fatalf("for-in program missing completion marker; stdout=%q", out)
		}
		lostBytes, lostBlocks, err := parseValgrindDefinitelyLost(stderr)
		if err != nil {
			t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
		}
		return out, lostBytes, lostBlocks
	}

	// THE ANSWER IS CHECKED, not just the absence of a crash. This defect's
	// worst form was a wrong number with a zero exit: the count came back right
	// and only the values were garbage, so a test that asserted "it ran" would
	// have passed throughout.
	for _, rounds := range []int{1, 4} {
		out, bytes, blocks := run(rounds)
		want := fmt.Sprintf("%d", fixedTotal+3*rounds)
		if !strings.Contains(out, want) {
			t.Fatalf(
				"for-in over a temporary produced the wrong total at %d rounds: wanted %s in\n%s\n"+
					"A wrong number here means the loop read the iterable after it was freed; the "+
					"count stays right because the cursor's length was read before the free.",
				rounds, want, out)
		}
		if bytes != 0 || blocks != 0 {
			t.Fatalf(
				"for-in over a temporary leaks at %d rounds: %d bytes in %d blocks, want zero. "+
					"The iterable is bound to a synthetic name now, so a leak here means its drop "+
					"stopped being emitted after the loop.",
				rounds, bytes, blocks)
		}
	}
}
