package sema

import (
	"context"
	"testing"

	"surge/internal/diag"
)

func TestExternFieldsEnableFieldAccess(t *testing.T) {
	src := `
type Foo = {}

extern<Foo> {
    field count: int;
}

fn demo(x: Foo) {
    let y: int = x.count;
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	syms := resolveSymbols(t, builder, fileID)
	bag := diag.NewBag(8)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  syms,
	})
	if bag.HasErrors() {
		t.Fatalf("unexpected semantic diagnostics: %s", diagnosticsSummary(bag))
	}
}

func TestExternFieldsSatisfyContract(t *testing.T) {
	src := `
contract HasCount {
    field count: int;
}

type Foo = {}

extern<Foo> {
    field count: int;
}

fn takes<T: HasCount>(value: T) -> int {
    return value.count;
}

fn demo() {
    let foo: Foo = { count: 0 };
    let _ = takes(foo);
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	syms := resolveSymbols(t, builder, fileID)
	bag := diag.NewBag(8)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  syms,
	})
	if bag.HasErrors() {
		t.Fatalf("unexpected semantic diagnostics: %s", diagnosticsSummary(bag))
	}
}

func TestExternFieldsDuplicateDetection(t *testing.T) {
	src := `
type Foo = {}

extern<Foo> {
    field a: int;
    field a: int;
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	syms := resolveSymbols(t, builder, fileID)
	bag := diag.NewBag(4)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  syms,
	})
	if !hasCodeContract(bag, diag.SemaExternDuplicateField) {
		t.Fatalf("expected extern duplicate field diagnostic, got %s", diagnosticsSummary(bag))
	}
}

func TestExternFieldsValidateAttributes(t *testing.T) {
	src := `
type Foo = {}

extern<Foo> {
    @override field a: int;
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	syms := resolveSymbols(t, builder, fileID)
	bag := diag.NewBag(4)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  syms,
	})
	if !hasCodeContract(bag, diag.SemaExternUnknownAttr) {
		t.Fatalf("expected extern attr diagnostic, got %s", diagnosticsSummary(bag))
	}
}

func TestExternFieldsRejectPositionalLiterals(t *testing.T) {
	src := `
type Foo = {}

extern<Foo> {
    field a: int;
    field b: string;
}

fn demo() {
    let _ = Foo { 1, "nope" };
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	syms := resolveSymbols(t, builder, fileID)
	bag := diag.NewBag(4)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  syms,
	})
	if !hasCodeContract(bag, diag.SemaTypeMismatch) {
		t.Fatalf("expected positional literal error, got %s", diagnosticsSummary(bag))
	}
}
