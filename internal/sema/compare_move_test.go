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
