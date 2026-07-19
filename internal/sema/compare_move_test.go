package sema

import (
	"testing"

	"surge/internal/diag"
)

func TestCompareOwnedScrutineeMarksBindingMoved(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
fn bad() -> int? {
    let next: int? = Some(1);
    compare next {
        nothing => {};
        _ => {};
    };
    return next;
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !hasCode(semaBag, diag.SemaUseAfterMove) {
		t.Fatalf("expected %v diagnostic, got %s", diag.SemaUseAfterMove, diagnosticsSummary(semaBag))
	}
}

// TestCompareCopyScrutineeStaysUsable pins the other side of the
// RV2-DEBT-052 gate: a Copy scrutinee (a plain int, matched by literal
// equality) is never move-tracked by observeMove, so the original
// binding stays valid after the compare — normalizeCompareExpr's
// release synthesis must never free a Copy scrutinee's storage, since
// there is none to transfer here in the first place.
func TestCompareCopyScrutineeStaysUsable(t *testing.T) {
	_, semaBag := runSemaOnSnippet(t, `
fn ok() -> int {
    let n: int = 1;
    compare n {
        1 => {};
        _ => {};
    };
    return n;
}
`)
	if hasCode(semaBag, diag.SemaUseAfterMove) {
		t.Fatalf("did not expect %v diagnostic for a Copy scrutinee, got %s", diag.SemaUseAfterMove, diagnosticsSummary(semaBag))
	}
}

// TestCompareGuardCannotMoveOwnBinding is the golden-invalid case for
// the guard-fallthrough UAF found in adversarial review of RV2-DEBT-052:
// extraction runs before the guard, so `consume(x)` (taking `x` by
// value) frees the payload inside the callee; on a failed guard the
// fallthrough re-tests the SAME scrutinee, and this fix's own
// deep-drop release on a later no-payload/wildcard arm would then
// double-free it. Rejected at sema instead: a guard may only borrow a
// pattern binding it introduces.
func TestCompareGuardCannotMoveOwnBinding(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
tag Payload(string);
tag Empty();
type Outcome = Payload(string) | Empty;

fn consume(s: string) -> bool { return s == "x"; }

fn bad(v: Outcome) -> int {
    return compare v {
        Payload(x) if consume(x) => 1;
        Empty() => 0;
        _ => 0 - 1;
    };
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !hasCode(semaBag, diag.SemaCompareGuardMovesBinding) {
		t.Fatalf("expected %v diagnostic, got %s", diag.SemaCompareGuardMovesBinding, diagnosticsSummary(semaBag))
	}
}

// TestCompareGuardCanBorrowOwnBinding is the golden-valid counterpart:
// borrowing (`check(&x)`) never calls observeMove, so it stays outside
// the new restriction — a failed guard leaves the payload untouched for
// whatever runs next, exactly as required.
func TestCompareGuardCanBorrowOwnBinding(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
tag Payload(string);
tag Empty();
type Outcome = Payload(string) | Empty;

fn check(s: &string) -> bool { return s == "x"; }

fn ok(v: Outcome) -> int {
    return compare v {
        Payload(x) if check(&x) => 1;
        Empty() => 0;
        _ => 0 - 1;
    };
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if hasCode(semaBag, diag.SemaCompareGuardMovesBinding) {
		t.Fatalf("did not expect %v diagnostic for a borrowing guard, got %s", diag.SemaCompareGuardMovesBinding, diagnosticsSummary(semaBag))
	}
	if semaBag.HasErrors() {
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(semaBag))
	}
}
