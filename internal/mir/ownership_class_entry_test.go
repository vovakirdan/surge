package mir

import (
	"testing"

	"surge/internal/types"
)

// sentinelScan is how far past a *Count sentinel the check below looks for a
// kind that slipped in behind it. Checking only the sentinel's own value cannot
// see one: appending `InstrFoo` after `instrKindCount` leaves the sentinel
// itself unnamed and every walk bounded by it still stops short.
const sentinelScan = 8

// TestKindCountSentinelsStayLast pins the one way the three walks above can go
// blind: a kind appended AFTER a sentinel is never reached by an iteration
// bounded by it. String() names every real kind and nothing else, so the kind
// below each sentinel must name itself and NOTHING at or above it may.
func TestKindCountSentinelsStayLast(t *testing.T) {
	if got := (instrKindCount - 1).String(); got == "Unknown" {
		t.Errorf("instrKindCount overshoots the last real InstrKind")
	}
	if got := OperandKind(operandKindCount - 1).String(); got == "Unknown" {
		t.Errorf("operandKindCount overshoots the last real OperandKind")
	}
	if got := RValueKind(rvalueKindCount - 1).String(); got == "Unknown" {
		t.Errorf("rvalueKindCount overshoots the last real RValueKind")
	}
	for i := range sentinelScan {
		if got := InstrKind(int(instrKindCount) + i).String(); got != "Unknown" {
			t.Errorf("InstrKind %s sits at or after instrKindCount; move the sentinel back to last and give the kind a classification", got)
		}
		if got := OperandKind(int(operandKindCount) + i).String(); got != "Unknown" {
			t.Errorf("OperandKind %s sits at or after operandKindCount; move the sentinel back to last and give the kind a classification", got)
		}
		if got := RValueKind(int(rvalueKindCount) + i).String(); got != "Unknown" {
			t.Errorf("RValueKind %s sits at or after rvalueKindCount; move the sentinel back to last and give the kind a classification", got)
		}
	}
}

// TestClassifyParamAtEntry pins the entry axiom against the predicate it
// mirrors. A parameter is a terminal ROOT, so the one answer it must never give
// is TRANSFERS — there is no earlier definition to inherit from.
func TestClassifyParamAtEntry(t *testing.T) {
	ot := newOwnershipTestTypes(t)

	cases := []struct {
		name string
		ty   types.TypeID
		want ownershipClass
	}{
		{"droppable_by_value", ot.str, ownershipOwnedAtEntry},
		{"value_composite", ot.strArray, ownershipOwnedAtEntry},
		{"owning_pointer", ot.strOwn, ownershipOwnedAtEntry},
		// Copy at the surface, so the caller keeps its binding AND its
		// reference for the whole call: a borrow, not owned at entry.
		{"reference_counted_scalar", ot.flt, ownershipAliases},
		{"reference", ot.strRef, ownershipNotApplicable},
		{"non_owning", ot.plain, ownershipNotApplicable},
		{"unknown", types.NoTypeID, ownershipNotApplicable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyParamAtEntry(tc.ty, ot.in, ot.sema, false)
			if got != tc.want {
				t.Errorf("classifyParamAtEntry = %s, want %s", got, tc.want)
			}
			// The same type read as a FRAME'S CAPTURE rather than a caller's
			// argument: the only class that moves is the reference-counted
			// one, and it moves to owned, because the state literal took the
			// reference before the body existed.
			wantAsCapture := tc.want
			if tc.want == ownershipAliases {
				wantAsCapture = ownershipOwnedAtEntry
			}
			if asCapture := classifyParamAtEntry(tc.ty, ot.in, ot.sema, true); asCapture != wantAsCapture {
				t.Errorf("classifyParamAtEntry(capturesArriveOwned) = %s, want %s", asCapture, wantAsCapture)
			}
			if got == ownershipTransfers {
				t.Errorf("a parameter classified TRANSFERS, which has nothing to inherit from")
			}
		})
	}

	// The caller's side of the same call has to agree about the same type, or
	// the callee thinks it owns what the caller never handed over. Asked of an
	// ordinary CALL, so `capturesArriveOwned` is false here: a frame's capture
	// has no caller-side argument contract to agree with, because the retain
	// that gave it its reference happened at the state literal rather than at
	// a call site.
	for _, ty := range []types.TypeID{ot.str, ot.strArray, ot.flt, ot.strRef, ot.plain} {
		entry := classifyParamAtEntry(ty, ot.in, ot.sema, false)
		contract := byValueArgContract(ot.in, ot.sema, ty, false)
		if (entry == ownershipOwnedAtEntry) != (contract == ArgContractTransferOwned) {
			t.Errorf("type %d: entry axiom %s disagrees with argument contract %s", ty, entry, contract)
		}
	}
}

// TestClassifyNilInputsAreAGap pins the fallthrough direction: an absent RValue
// or operand is a gap to report, never a permissive answer.
func TestClassifyNilInputsAreAGap(t *testing.T) {
	ot := newOwnershipTestTypes(t)
	if got := classifyRValue(nil, ot.str, ot.in, ot.sema); got != ownershipUnclassified {
		t.Errorf("classifyRValue(nil) = %s, want unclassified", got)
	}
	if got := classifyOperand(nil, ot.in, ot.sema); got != ownershipUnclassified {
		t.Errorf("classifyOperand(nil) = %s, want unclassified", got)
	}
	if _, ok := instrMintsDest(nil); ok {
		t.Errorf("instrMintsDest(nil) reported a destination")
	}
}
