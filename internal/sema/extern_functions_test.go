package sema

import (
	"testing"

	"surge/internal/diag"
	"surge/internal/symbols"
)

func TestExternFunctionReportsUnknownTypes(t *testing.T) {
	src := `
extern<string> {
    @overload fn __add(self: string, other: MyType) -> string { return self; }
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}

	resolveBag := diag.NewBag(8)
	syms := symbols.ResolveFile(builder, fileID, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: resolveBag},
	})
	if resolveBag.HasErrors() {
		t.Fatalf("unexpected resolve diagnostics: %s", diagnosticsSummary(resolveBag))
	}

	bag := diag.NewBag(8)
	Check(builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  &syms,
	})

	if !hasCodeContract(bag, diag.SemaUnresolvedSymbol) {
		t.Fatalf("expected unresolved symbol diagnostics, got %+v", bag.Items())
	}
}

func TestExternFunctionSeesParamFieldTypes(t *testing.T) {
	src := `
type Point = { x: int, y: int }

extern<Point> {
    fn __add(self: Point, other: Point) -> Point {
        return { x: self.x + other.x, y: self.y + other.y };
    }
}
`

	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}

	resolveBag := diag.NewBag(8)
	syms := symbols.ResolveFile(builder, fileID, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: resolveBag},
	})
	if resolveBag.HasErrors() {
		t.Fatalf("unexpected resolve diagnostics: %s", diagnosticsSummary(resolveBag))
	}

	bag := diag.NewBag(8)
	Check(builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  &syms,
	})

	if bag.HasErrors() {
		t.Fatalf("unexpected semantics errors: %s", diagnosticsSummary(bag))
	}
}
