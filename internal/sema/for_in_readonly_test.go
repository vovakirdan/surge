package sema

import (
	"testing"

	"surge/internal/diag"
)

// The code numbers are kicked LITERALLY, not through the constants. A test that
// reads the constant agrees with whatever the constant says, including a value
// a parallel lane took first — which is how two rules once shared a number.
const (
	writeToLoopBindingCode = 3201
	mutableIterableCode    = 3202
)

func TestForInReadOnlyCodeNumbers(t *testing.T) {
	if got := int(diag.SemaWriteToLoopBinding); got != writeToLoopBindingCode {
		t.Fatalf("SemaWriteToLoopBinding is %d, want %d", got, writeToLoopBindingCode)
	}
	if got := int(diag.SemaMutableIterable); got != mutableIterableCode {
		t.Fatalf("SemaMutableIterable is %d, want %d", got, mutableIterableCode)
	}
}

// THE BEHAVIOUR OF THESE TWO RULES IS TESTED BY GOLDEN FIXTURES, NOT HERE.
//
//	testdata/golden/sema/invalid/ownership/for_in_is_read_only.sg
//	    four rows: the whole-binding store, the place store through a field, the
//	    `&mut` iterable, and the Copy case `for i in 0..10 { i = 5 }` — which is
//	    in the fixture because the refusal is uniform rather than typed on
//	    whether the element owns heap.
//	testdata/golden/sema/valid/for_in_over_mut_ref_param.sg
//	    an EMPTY .diag, and the half that decides SEM3202's predicate is right:
//	    `fn total(xs: &mut Item[]) { for it in xs { ... } }` still compiles and
//	    returns 33 on both lanes. A predicate written as "the iterable has
//	    reference type" kills that program, which is exactly what this fixture
//	    is for.
//
// SEM3201's headline names a LOOP, and it may do that only because
// markNonOwningBinding has exactly one caller — the for-in walker. A second
// producer would make the message a lie about a binding that is not a loop's,
// so the invalid fixture above pins the shapes rather than only the rule.
