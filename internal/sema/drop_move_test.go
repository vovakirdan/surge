package sema

import (
	"fmt"
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
fn ok() -> int64 {
    let n: int64 = 4;
    @drop n;
    return n;
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
}

func TestDropOfCountedCopyBindingConsumes(t *testing.T) {
	for _, scalar := range []string{"int", "uint"} {
		t.Run(scalar, func(t *testing.T) {
			parseBag, semaBag := runSemaOnSnippet(t, fmt.Sprintf(`
fn bad() -> %[1]s {
    let n: %[1]s = 4;
    @drop n;
    return n;
}`, scalar))
			if parseBag.HasErrors() {
				t.Fatalf("parse diagnostics: %s", diagnosticsSummary(parseBag))
			}
			requireSemaCodeCount(t, semaBag, diag.SemaUseAfterMove, 1)
		})
	}
}
