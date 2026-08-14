package sema

import (
	"testing"

	"surge/internal/diag"
)

// The code number is kicked LITERALLY, not through the constant. A test that
// reads the constant agrees with whatever the constant says, including a value
// a parallel lane took first — which is how two rules once shared a number.
const armReferenceIntoFreedPayloadCode = 3200

func TestArmReferenceIntoFreedPayloadCodeNumber(t *testing.T) {
	if got := int(diag.SemaArmReferenceIntoFreedPayload); got != armReferenceIntoFreedPayloadCode {
		t.Fatalf("SemaArmReferenceIntoFreedPayload is %d, want %d", got, armReferenceIntoFreedPayloadCode)
	}
}

// THE BEHAVIOUR OF THIS RULE IS TESTED BY GOLDEN FIXTURES, NOT HERE: the
// snippet harness has no stdlib, so `Option`, `pop()` and the payload's release
// obligation never come into existence and a snippet test would agree with a
// program it never typed.
//
//	testdata/golden/sema/invalid/ownership/arm_reference_into_freed_payload.sg
//	    one SEM3200 row — an OWNED scrutinee, so the arm frees the payload the
//	    reference points into;
//	testdata/golden/sema/valid/arm_reference_into_borrowed_payload.sg
//	    an EMPTY .diag, and it is the half that decides the predicate is right:
//	    the same `&inner[0]` over a BORROWED subject stays legal, because the
//	    owner keeps the payload alive past the compare. It runs and returns 8 on
//	    both lanes. A predicate written as "any reference out of an arm" fails
//	    here, which is exactly what it is for.
//
// RV2-DEBT-205's fixture `testdata/golden/vm_compare/compare_arm_element_read.sg`
// is the third leg: the VALUE-typed spelling of the same program must keep
// compiling, or this rule has swallowed the shape 205 fixed.
