package sema

import (
	"testing"

	"surge/internal/diag"
)

func TestGenericFunctionTypeParams(t *testing.T) {
	src := `
fn id<T>(x: T) -> T { return x; }

fn main() {
    let a = id(42);
    let b = id("hello");
}
`
	bag := runGenericsSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
}

func TestGenericTypeShadowsNamedType(t *testing.T) {
	src := `
type T = { value: int };

fn f(x: T) { }

fn g<T>(x: T) -> T { return x; }

fn main() {
    let v: T = { value: 1 };
    f(v);
    let _ = g(42);
}
`
	bag := runGenericsSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
}

func TestGenericTypeDeclarationUsage(t *testing.T) {
	src := `
type Box<T> = { value: T };

fn main() {
    let i: Box<int> = { value: 1 };
    let s: Box<string> = { value: "hi" };
}
`
	bag := runGenericsSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
}

func TestGenericContractScope(t *testing.T) {
	src := `
contract FooLike<T> {
    field v: T;
    fn get(self: T) -> T;
}
`
	bag := runGenericsSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
}

func TestGenericTagScope(t *testing.T) {
	src := `
tag Some<T>(T);
type Option<T> = Some(T) | nothing;
`
	bag := runGenericsSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
}

func TestImplicitTypeParamInFunctionIsError(t *testing.T) {
	src := `
fn bad(x: T) {}
`
	bag := runGenericsSource(t, src)
	if !hasCode(bag, diag.SemaUnresolvedSymbol) {
		t.Fatalf("expected unresolved symbol, got %s", diagnosticsSummary(bag))
	}
}

func TestImplicitTypeParamInStructIsError(t *testing.T) {
	src := `
type S = { value: T };
`
	bag := runGenericsSource(t, src)
	if !hasCode(bag, diag.SemaUnresolvedSymbol) {
		t.Fatalf("expected unresolved symbol, got %s", diagnosticsSummary(bag))
	}
}

func TestImplicitTypeParamInContractIsError(t *testing.T) {
	src := `
contract C {
    field v: T;
}
`
	bag := runGenericsSource(t, src)
	if !hasCode(bag, diag.SemaUnresolvedSymbol) {
		t.Fatalf("expected unresolved symbol, got %s", diagnosticsSummary(bag))
	}
}

func TestGenericTypeArityMismatchIsError(t *testing.T) {
	src := `
type Box<T> = { value: T };

fn main() {
    let b: Box<int, int> = { value: 1 };
}
`
	bag := runGenericsSource(t, src)
	if !hasCode(bag, diag.SemaTypeMismatch) {
		t.Fatalf("expected type mismatch, got %s", diagnosticsSummary(bag))
	}
}

func TestUnknownTypeArgumentIsError(t *testing.T) {
	src := `
type Box<T> = { value: T };

fn main() {
    let b: Box<U> = { value: 1 };
}
`
	bag := runGenericsSource(t, src)
	if !hasCode(bag, diag.SemaUnresolvedSymbol) {
		t.Fatalf("expected unresolved symbol, got %s", diagnosticsSummary(bag))
	}
}

func runGenericsSource(t *testing.T, src string) *diag.Bag {
	t.Helper()
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	syms := resolveSymbols(t, builder, fileID)
	bag := diag.NewBag(32)
	Check(builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  syms,
	})
	return bag
}
