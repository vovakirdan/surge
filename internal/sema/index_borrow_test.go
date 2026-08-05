package sema

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

func TestCustomIndexAliasCarrierSharedReborrowAccepted(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Bag = { marker: int };
type IntRef = &int;

extern<Bag> {
    fn __index(self: &Bag, index: IntRef) -> IntRef {
        let _ = self;
        return index;
    }
}

fn read(bag: &Bag, index: IntRef) -> int {
    let value: &int = &bag[index];
    return *value;
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if semaBag.HasErrors() {
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(semaBag))
	}
}

func TestCustomIndexSharedCarrierRejectsMutableReborrowWithNote(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Bag = { value: int };

extern<Bag> {
    fn __index(self: &Bag, index: int) -> &int {
        let _ = index;
        return &self.value;
    }
}

fn bad(bag: &Bag) -> nothing {
    let value: &mut int = &mut bag[0];
    let _ = value;
    return nothing;
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}

	var found *diag.Diagnostic
	for _, item := range semaBag.Items() {
		if item != nil && item.Code == diag.SemaBorrowImmutable {
			found = item
			break
		}
	}
	if found == nil {
		t.Fatalf("expected %v, got %s", diag.SemaBorrowImmutable, diagnosticsSummary(semaBag))
	}
	if !strings.Contains(found.Message, "selected `__index` returns shared &int") {
		t.Fatalf("unexpected diagnostic message: %q", found.Message)
	}
	if len(found.Notes) != 1 || !strings.Contains(found.Notes[0].Msg, "returns `&mut T`") {
		t.Fatalf("expected mutable-accessor note, got %+v", found.Notes)
	}
}
