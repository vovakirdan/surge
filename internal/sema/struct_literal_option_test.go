package sema

import (
	"context"
	"testing"

	"surge/internal/diag"
)

func TestStructLiteralAllowsNothingForOptionField(t *testing.T) {
	src := `
tag Some<T>(T);
type Option<T> = Some(T) | nothing;

type Foo = { a: Option<int> }

fn main() {
    let _ = Foo { a = nothing };
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
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(bag))
	}
}
