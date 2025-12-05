package sema

import (
	"context"
	"testing"

	"surge/internal/diag"
)

func TestOverloadPrefersMonomorphicBeforeGeneric(t *testing.T) {
	src := `
fn foo(n: int) -> int { return 0; }
@overload fn foo<T>(n: T) -> string { return ""; }

fn main() {
    let a: int = foo(0);
    let b: string = foo("bar");
}
`
	bag := runOverloadSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
}

func TestOverloadAmbiguityWithinGenericSet(t *testing.T) {
	src := `
@overload fn qux<T>(x: T, y: T) -> int { return 0; }
@overload fn qux<U>(x: U, y: U) -> int { return 1; }

fn main() {
    qux(1, 2);
}
`
	bag := runOverloadSource(t, src)
	if !hasCode(bag, diag.SemaAmbiguousOverload) {
		t.Fatalf("expected ambiguous overload diagnostic, got %s", diagnosticsSummary(bag))
	}
}

func TestOverloadMonomorphicMismatchThenGenericAmbiguity(t *testing.T) {
	src := `
fn spam(x: int) -> int { return x; }

@overload fn spam<T>(x: T, y: T) -> int { return 0; }
@overload fn spam<U, V>(x: U, y: V) -> int { return 0; }

fn main() {
    spam("a", "b");
}
`
	bag := runOverloadSource(t, src)
	if !hasCode(bag, diag.SemaAmbiguousOverload) {
		t.Fatalf("expected ambiguous overload diagnostic, got %s", diagnosticsSummary(bag))
	}
}

func TestOverloadNoMatchAcrossMonomorphicAndGeneric(t *testing.T) {
	src := `
fn zoo(x: int) {}
@overload fn zoo<T>(x: T) {}

fn main() {
    zoo();
}
`
	bag := runOverloadSource(t, src)
	if !hasCode(bag, diag.SemaNoOverload) {
		t.Fatalf("expected no overload diagnostic, got %s", diagnosticsSummary(bag))
	}
}

func runOverloadSource(t *testing.T, src string) *diag.Bag {
	t.Helper()
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	syms := resolveSymbols(t, builder, fileID)
	bag := diag.NewBag(16)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  syms,
	})
	return bag
}
