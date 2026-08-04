package sema

import (
	"context"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
)

func runLayoutSema(t *testing.T, sourceCode string) *diag.Bag {
	t.Helper()
	builder, fileID, parseBag := parseSource(t, sourceCode)
	if parseBag.HasErrors() {
		t.Fatalf("parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	symbolResult := resolveSymbols(t, builder, fileID)
	bag := diag.NewBag(64)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  symbolResult,
	})
	return bag
}

func findDiagnosticByCode(bag *diag.Bag, code diag.Code) *diag.Diagnostic {
	if bag == nil {
		return nil
	}
	for _, item := range bag.Items() {
		if item.Code == code {
			return item
		}
	}
	return nil
}

func TestTypeLayoutDiagnosticsArePreciseAndFriendly(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		code       diag.Code
		messageHas string
		noteHas    string
	}{
		{
			name: "recursive",
			source: `type Node = {
    next: Node,
};`,
			code:       diag.SemaRecursiveUnsized,
			messageHas: "Node has infinite size",
			noteHas:    "field next (Node)",
		},
		{
			name: "overaligned",
			source: `@align(8589934592)
type TooAligned = { value: bool };`,
			code:       diag.SemaLayoutUnsupportedAlignment,
			messageHas: "alignment 8589934592 for TooAligned",
			noteHas:    "no greater than 4294967296",
		},
		{
			name: "overflow",
			source: `@align(4294967296)
type Huge = { value: bool };
type TooLarge = {
    items: Huge[4294967295],
    tail: Huge,
};`,
			code:       diag.SemaLayoutOverflow,
			messageHas: "physical layout of TooLarge exceeds",
			noteHas:    "field tail (Huge)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bag := runContractSema(t, test.source)
			item := findDiagnosticByCode(bag, test.code)
			if item == nil {
				t.Fatalf("missing %s diagnostic: %s", test.code.ID(), diagnosticsSummary(bag))
			}
			if !strings.Contains(item.Message, test.messageHas) || strings.Contains(item.Message, "type#") {
				t.Fatalf("message = %q", item.Message)
			}
			notes := make([]string, 0, len(item.Notes))
			for _, note := range item.Notes {
				notes = append(notes, note.Msg)
			}
			joined := strings.Join(notes, "\n")
			if !strings.Contains(joined, test.noteHas) || strings.Contains(joined, "type#") {
				t.Fatalf("notes = %q", joined)
			}
			if item.Code == diag.SemaTypeNotClonable {
				t.Fatal("layout error reused SEM3116")
			}
		})
	}
}

func TestGenericDeferredLayoutIsQuietUntilConcrete(t *testing.T) {
	bag := runLayoutSema(t, `
type Box<T> = { value: T };

fn main() {
    let value: Box<int> = { value: 7 };
}
`)
	for _, item := range bag.Items() {
		switch item.Code {
		case diag.SemaRecursiveUnsized, diag.SemaLayoutOverflow,
			diag.SemaLayoutUnsupportedAlignment, diag.SemaLayoutDeferred:
			t.Fatalf("unexpected layout diagnostic: %s: %s", item.Code.ID(), item.Message)
		}
	}
}

func TestSignatureOnlyConcreteLayoutObligationIsValidated(t *testing.T) {
	bag := runLayoutSema(t, `
@align(4294967296)
type Huge = { value: bool };
type Storage<T> = {
    items: T[4294967295],
    tail: T,
};

fn consume(value: Storage<Huge>) { }
`)
	if item := findDiagnosticByCode(bag, diag.SemaLayoutOverflow); item == nil {
		t.Fatalf("missing signature-only layout error: %s", diagnosticsSummary(bag))
	}
}

func TestExplicitInstantiationOnlyLayoutObligationIsValidated(t *testing.T) {
	bag := runLayoutSema(t, `
@align(4294967296)
type Huge = { value: bool };
type Storage<T> = {
    items: T[4294967295],
    tail: T,
};

fn marker<T>() -> bool { return true; }
fn main() {
    let ok: bool = marker::<Storage<Huge>>();
}
`)
	if item := findDiagnosticByCode(bag, diag.SemaLayoutOverflow); item == nil {
		t.Fatalf("missing explicit-instantiation-only layout error: %s", diagnosticsSummary(bag))
	}
}

func TestFunctionBodyLayoutObligationIsValidatedAfterSubstitution(t *testing.T) {
	program := `
@align(4294967296)
type Huge = { value: bool };
type Storage<T> = {
    items: T[4294967295],
    tail: T,
};

fn marker<T>() -> bool {
    let value: Storage<T>;
    return true;
}
fn main() {
    let ok: bool = marker::<Huge>();
}
`
	bag := runLayoutSema(t, program)
	item := findDiagnosticByCode(bag, diag.SemaLayoutOverflow)
	if item == nil {
		t.Fatalf("missing substituted function-body layout error: %s", diagnosticsSummary(bag))
	}
	if !strings.Contains(item.Message, "Storage<Huge>") {
		t.Fatalf("message = %q, want concrete substituted type", item.Message)
	}
	wantStart := strings.Index(program, "marker::<Huge>()")
	if item.Primary == (source.Span{}) || int(item.Primary.Start) != wantStart {
		t.Fatalf("primary = %s, want instantiation start %d", item.Primary, wantStart)
	}
}

func TestUnusedGenericFunctionBodyLayoutObligationRemainsDeferred(t *testing.T) {
	bag := runLayoutSema(t, `
type Storage<T> = { value: T };

fn marker<T>() -> bool {
    let value: Storage<T>;
    return true;
}
fn main() { }
`)
	for _, item := range bag.Items() {
		switch item.Code {
		case diag.SemaRecursiveUnsized, diag.SemaLayoutOverflow,
			diag.SemaLayoutUnsupportedAlignment, diag.SemaLayoutDeferred:
			t.Fatalf("unused generic body reported %s: %s", item.Code.ID(), item.Message)
		}
	}
}

func TestAliasLayoutDiagnosticKeepsAliasPathPrefix(t *testing.T) {
	bag := runContractSema(t, `
@align(4294967296)
type Huge = { value: bool };
type Storage<T> = {
    items: T[4294967295],
    tail: T,
};
type Broken = Storage<Huge>;
`)
	item := findDiagnosticByCode(bag, diag.SemaLayoutOverflow)
	if item == nil {
		t.Fatalf("missing alias layout diagnostic: %s", diagnosticsSummary(bag))
	}
	notes := make([]string, 0, len(item.Notes))
	for _, note := range item.Notes {
		notes = append(notes, note.Msg)
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "Broken -> alias target Storage<Huge> -> field tail (Huge)") {
		t.Fatalf("alias path notes = %q", joined)
	}
}
