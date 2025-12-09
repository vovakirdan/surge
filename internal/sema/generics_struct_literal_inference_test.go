package sema

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

func TestStructLiteralInfersGenericFromNamedFields(t *testing.T) {
	src := `
type Box<T> = { value: T };

fn main() {
    let _ = Box { value: 42 };
}
`
	bag := runGenericsSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
}

func TestStructLiteralPositionalInference(t *testing.T) {
	src := `
type Box<T> = { value: T };

fn main() {
    let _ = Box { 1 };
}
`
	bag := runGenericsSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
}

func TestStructLiteralInferenceFailureReportsHelpfulError(t *testing.T) {
	src := `
type Phantom<T> = {};

fn main() {
    let _ = Phantom {};
}
`
	bag := runGenericsSource(t, src)
	if !bag.HasErrors() {
		t.Fatalf("expected inference diagnostics, got none")
	}
	found := false
	for _, item := range bag.Items() {
		if item.Code == diag.SemaTypeMismatch && strings.Contains(item.Message, "cannot infer type parameter") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected cannot-infer diagnostic, got: %s", diagnosticsSummary(bag))
	}
}
