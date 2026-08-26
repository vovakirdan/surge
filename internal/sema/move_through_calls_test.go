package sema

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

// The two tightenings that unblock recursive drop glue: moves must be
// observed through GENERIC calls and into STRUCT LITERAL fields —
// otherwise the callee/aggregate and the caller both think they own the
// value and recursive drops double-free. Every rejection carries the
// move-site note and a way-out hint (the author's friendliness mandate).

func TestMoveThroughGenericCallIsTracked(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
fn sink<T>(v: T) -> nothing {
}

fn bad() -> nothing {
    let s: string = "gone";
    sink(s);
    sink(s);
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !hasCode(semaBag, diag.SemaUseAfterMove) {
		t.Fatalf("expected %v through a generic call, got %s", diag.SemaUseAfterMove, diagnosticsSummary(semaBag))
	}
}

func TestMoveIntoStructLiteralFieldIsTracked(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Holder = { value: string }

fn eat(s: string) -> nothing {
}

fn bad() -> nothing {
    let s: string = "owned-by-holder";
    let h: Holder = Holder { value: s };
    eat(s);
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !hasCode(semaBag, diag.SemaUseAfterMove) {
		t.Fatalf("expected %v through a struct literal field, got %s", diag.SemaUseAfterMove, diagnosticsSummary(semaBag))
	}
}

func TestUseAfterMoveCarriesWayOutHint(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
fn sink<T>(v: T) -> nothing {
}

fn bad() -> nothing {
    let s: string = "gone";
    sink(s);
    sink(s);
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	// The way out is ADVICE, so it lives in the Help channel. Which way out it
	// names depends on what the compiler can prove about the type: this snippet
	// is checked without the standard library, so `string` has no visible
	// `__clone` and the sentence offers the borrow alone. That is the whole
	// point of the type-directed table — it never names a clone it cannot
	// prove is callable.
	found := false
	for _, item := range semaBag.Items() {
		if item.Code != diag.SemaUseAfterMove {
			continue
		}
		for _, entry := range item.Help {
			if strings.Contains(entry.Msg, "borrow") || strings.Contains(entry.Msg, "clone(") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("use-after-move must carry the way-out help, got %s", diagnosticsSummary(semaBag))
	}
}

func TestBorrowingThroughGenericCallStaysAccepted(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
fn peek<T>(v: &T) -> nothing {
}

fn ok() -> nothing {
    let s: string = "kept";
    peek(&s);
    peek(&s);
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
}
