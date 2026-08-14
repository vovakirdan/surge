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

// SEM3202's QUICK FIX IS TESTED HERE because no golden fixture captures fixes -
// the corpus records `.diag`, and a fix that stopped being offered, or that
// deleted the wrong span, would not move a single golden byte.
//
// The snippet harness is enough for exactly this rule and for no other in this
// file: rejectMutableIterable runs BEFORE the iterable is typed, so it is a
// syntax question and needs no stdlib. The assertion is positive on both counts
// - the diagnostic is there, and its edit covers `&mut ` and nothing else.
func TestMutableIterableOffersToDropTheMut(t *testing.T) {
	src := `
fn f(hs: Holder[]) -> nothing {
    for h in &mut hs {
    }
}
`
	_, semaBag, _ := runSemaOnSnippetResult(t, src)
	var found *diag.Diagnostic
	for _, d := range semaBag.Items() {
		if d.Code == diag.SemaMutableIterable {
			found = d
			break
		}
	}
	if found == nil {
		t.Fatalf("SEM3202 was not reported: %s", diagnosticsSummary(semaBag))
	}
	if len(found.Fixes) != 1 || len(found.Fixes[0].Edits) != 1 {
		t.Fatalf("expected exactly one fix with one edit, got %+v", found.Fixes)
	}
	edit := found.Fixes[0].Edits[0]
	if edit.NewText != "" {
		t.Fatalf("the fix must DELETE, not rewrite; NewText=%q", edit.NewText)
	}
	// `&mut ` is five bytes. Deleting more would take the iterable with it, and
	// deleting less would leave a `&` behind.
	if got := edit.Span.End - edit.Span.Start; got != 5 {
		t.Fatalf("the fix deletes %d bytes, want the 5 of `&mut `", got)
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
