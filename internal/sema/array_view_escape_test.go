package sema

import (
	"testing"

	"surge/internal/diag"
)

// The code number is kicked LITERALLY, not through the constant. A test that
// reads the constant agrees with whatever the constant says, including a value
// a parallel lane took first — which is how two rules once shared a number.
const fixedArrayViewEscapesCode = 3198

func TestFixedArrayViewEscapesCodeNumber(t *testing.T) {
	if got := int(diag.SemaFixedArrayViewEscapes); got != fixedArrayViewEscapesCode {
		t.Fatalf("SemaFixedArrayViewEscapes is %d, want %d", got, fixedArrayViewEscapesCode)
	}
}

// THE BEHAVIOUR OF THIS RULE IS TESTED BY GOLDEN FIXTURES, NOT HERE, and the
// reason is worth writing down because the obvious unit test PASSES FOR THE
// WRONG REASON.
//
// `runSemaOnSnippet` types a bare snippet with no stdlib, so `xs[[1..3]]`
// cannot resolve `Range` and never becomes a slice at all. A refusal test
// written against it fails with "unknown type Range"; worse, an ACCEPTANCE test
// written against it passes while reporting nothing — it would stay green with
// the rule deleted, or with the rule refusing every program in the language.
//
// The fixtures do have the stdlib and do reach the rule:
//
//	testdata/golden/sema/invalid/ownership/fixed_array_view_escapes_return.sg
//	    two SEM3198 rows, one for the slice returned straight out of the
//	    expression and one for the slice returned through a binding that
//	    carries its provenance;
//	testdata/golden/sema/valid/fixed_array_view_stays_in_frame.sg
//	    an EMPTY .diag, which is the half that matters: a view over a `&T[N]`
//	    parameter returned, a dynamic array's view returned, and a fixed view
//	    used and dropped inside one frame all stay accepted. That file is what
//	    fails if the predicate ever widens.
