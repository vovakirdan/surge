package sema

import (
	"testing"

	"surge/internal/diag"
)

// The code number is kicked LITERALLY, not through the constant. A test that
// reads the constant agrees with whatever the constant says, including a value
// a parallel lane took first — which is how two rules once shared a number.
const rangeCursorEscapesCode = 3199

func TestRangeCursorEscapesCodeNumber(t *testing.T) {
	if got := int(diag.SemaRangeCursorEscapes); got != rangeCursorEscapesCode {
		t.Fatalf("SemaRangeCursorEscapes is %d, want %d", got, rangeCursorEscapesCode)
	}
}

// THE BEHAVIOUR OF THIS RULE IS TESTED BY GOLDEN FIXTURES, NOT HERE, for the
// same reason spelled out beside the fixed-array view rule: `runSemaOnSnippet`
// has no stdlib, so `__range()` never resolves to a `Range<T>` and a snippet
// test asserting "no diagnostic" would be green for a program it never typed.
//
// The fixtures do have the stdlib and do reach the rule:
//
//	testdata/golden/sema/invalid/ownership/range_cursor_escapes_return.sg
//	    two SEM3199 rows — the cursor returned straight out of the expression,
//	    and the cursor returned through a binding that carries its provenance;
//	testdata/golden/sema/valid/range_cursor_stays_in_frame.sg
//	    an EMPTY .diag, and that is the half that matters. It holds the two
//	    shapes MEASURED CLEAN before this rule was written — a cursor consumed
//	    inside the frame that owns the array, and a cursor over a `&T[]`
//	    REFERENCE PARAMETER returned to the caller — so it is the file that
//	    fails if the predicate ever widens past what was measured.
