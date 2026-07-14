package sema

import (
	"testing"

	"surge/internal/diag"
)

func TestDropConsumesOwnedBinding(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
fn bad() -> nothing {
    let s: string = "gone";
    @drop s;
    @drop s;
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !hasCode(semaBag, diag.SemaUseAfterMove) {
		t.Fatalf("expected %v diagnostic, got %s", diag.SemaUseAfterMove, diagnosticsSummary(semaBag))
	}
}

func TestDropOfMovedBindingIsRejected(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
fn eat(s: string) -> nothing {
}

fn bad() -> nothing {
    let s: string = "gone";
    eat(s);
    @drop s;
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !hasCode(semaBag, diag.SemaUseAfterMove) {
		t.Fatalf("expected %v diagnostic, got %s", diag.SemaUseAfterMove, diagnosticsSummary(semaBag))
	}
}

func TestDropOfCopyBindingDoesNotConsume(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
fn ok() -> int {
    let n: int = 4;
    @drop n;
    return n;
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if hasCode(semaBag, diag.SemaUseAfterMove) {
		t.Fatalf("copy drop must not consume the binding, got %s", diagnosticsSummary(semaBag))
	}
}
