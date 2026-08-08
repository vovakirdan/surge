package sema

import (
	"context"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/symbols"
)

func aliasMagicDiagnostics(t *testing.T, src string) *diag.Bag {
	t.Helper()
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	resolveBag := diag.NewBag(16)
	syms := symbols.ResolveFile(builder, fileID, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: resolveBag},
	})
	if resolveBag.HasErrors() {
		t.Fatalf("unexpected resolve diagnostics: %s", diagnosticsSummary(resolveBag))
	}
	bag := diag.NewBag(16)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  &syms,
	})
	return bag
}

func aliasMagicDiagnostic(t *testing.T, bag *diag.Bag) *diag.Diagnostic {
	t.Helper()
	for _, item := range bag.Items() {
		if item.Code == diag.SemaAliasMagicRedeclared {
			return item
		}
	}
	t.Fatalf("expected an alias magic method refusal, got: %s", diagnosticsSummary(bag))
	return nil
}

func TestAliasRedeclaringTargetMagicMethodIsRefused(t *testing.T) {
	src := `
type Leaf = { text: string };
type Handle = Leaf;

extern<Leaf> {
    pub fn __eq(self: &Leaf, other: &Leaf) -> bool { return true; }
}

extern<Handle> {
    pub fn __eq(self: &Handle, other: &Handle) -> bool { return false; }
}
`
	bag := aliasMagicDiagnostics(t, src)
	d := aliasMagicDiagnostic(t, bag)
	if !strings.Contains(d.Message, "delete `__eq` from `extern<Handle>`") {
		t.Fatalf("headline does not name the edit: %q", d.Message)
	}
	if len(d.Notes) < 4 {
		t.Fatalf("expected the rival location and the reasons as notes, got %d", len(d.Notes))
	}
	if !strings.Contains(d.Notes[0].Msg, "is declared here") {
		t.Fatalf("first note should name the other declaration, got %q", d.Notes[0].Msg)
	}
	if len(d.Fixes) != 1 {
		t.Fatalf("expected one quick fix, got %d", len(d.Fixes))
	}
	if d.Fixes[0].Applicability != diag.FixApplicabilityManualReview {
		t.Fatalf("removing a body that may differ must not be applied blind: %s", d.Fixes[0].Applicability)
	}
	if d.Fixes[0].IsPreferred {
		t.Fatalf("a fix the author has to weigh must not be marked preferred")
	}
}

func TestAliasMagicMethodStaysLegalWhenTargetDeclaresNone(t *testing.T) {
	src := `
type Leaf = { text: string };
type Handle = Leaf;

extern<Handle> {
    pub fn __eq(self: &Handle, other: &Handle) -> bool { return true; }
}
`
	bag := aliasMagicDiagnostics(t, src)
	if hasCodeContract(bag, diag.SemaAliasMagicRedeclared) {
		t.Fatalf("an alias adding a hook its target lacks stays legal: %s", diagnosticsSummary(bag))
	}
}

func TestTargetMagicMethodStaysLegalWhenAliasDeclaresNone(t *testing.T) {
	src := `
type Leaf = { text: string };
type Handle = Leaf;

extern<Leaf> {
    pub fn __eq(self: &Leaf, other: &Leaf) -> bool { return true; }
}
`
	bag := aliasMagicDiagnostics(t, src)
	if hasCodeContract(bag, diag.SemaAliasMagicRedeclared) {
		t.Fatalf("a hook on the target alone stays legal: %s", diagnosticsSummary(bag))
	}
}

func TestAliasOrdinaryMethodOverrideStaysLegal(t *testing.T) {
	// The call site writes `Leaf::label` or `Handle::label`, so the author picks
	// the body. Only hooks the compiler reaches for are refused.
	src := `
type Leaf = { text: string };
type Handle = Leaf;

extern<Leaf> {
    pub fn label(self: &Leaf) -> string { return "leaf"; }
}

extern<Handle> {
    pub fn label(self: &Handle) -> string { return "handle"; }
}
`
	bag := aliasMagicDiagnostics(t, src)
	if hasCodeContract(bag, diag.SemaAliasMagicRedeclared) {
		t.Fatalf("an ordinary method override on an alias stays legal: %s", diagnosticsSummary(bag))
	}
}

func TestAliasMagicMethodWithDifferentOperandsStaysLegal(t *testing.T) {
	// Two `__to` declarations that convert to different targets answer different
	// questions, so neither shadows the other.
	src := `
type Leaf = { text: string };
type Handle = Leaf;
type Tag = { id: int };
type Marker = { id: int };

extern<Leaf> {
    pub fn __to(self: Leaf, target: Tag) -> Tag { return Tag { id = 1 }; }
}

extern<Handle> {
    pub fn __to(self: Handle, target: Marker) -> Marker { return Marker { id = 2 }; }
}
`
	bag := aliasMagicDiagnostics(t, src)
	if hasCodeContract(bag, diag.SemaAliasMagicRedeclared) {
		t.Fatalf("hooks over different operands stay legal: %s", diagnosticsSummary(bag))
	}
}

func TestAliasMagicMethodRefusedThroughAliasChain(t *testing.T) {
	src := `
type Leaf = { text: string };
type Middle = Leaf;
type Handle = Middle;

extern<Leaf> {
    pub fn __eq(self: &Leaf, other: &Leaf) -> bool { return true; }
}

extern<Handle> {
    pub fn __eq(self: &Handle, other: &Handle) -> bool { return false; }
}
`
	bag := aliasMagicDiagnostics(t, src)
	if !hasCodeContract(bag, diag.SemaAliasMagicRedeclared) {
		t.Fatalf("an alias of an alias is transparent through both steps: %s", diagnosticsSummary(bag))
	}
}

func TestAliasRedeclaringCompilerProvidedMagicMethodIsRefused(t *testing.T) {
	src := `
type Score = int;

extern<Score> {
    pub fn __add(self: Score, other: Score) -> Score { return self; }
}
`
	bag := aliasMagicDiagnostics(t, src)
	d := aliasMagicDiagnostic(t, bag)
	if !strings.Contains(d.Notes[0].Msg, "the compiler supplies") {
		t.Fatalf("a builtin rival has no source line to point at, so the note has to say so: %q", d.Notes[0].Msg)
	}
}
