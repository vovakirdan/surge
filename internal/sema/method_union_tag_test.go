package sema

import (
	"context"
	"testing"

	"surge/internal/diag"
)

func TestMethodCallAcceptsTagUnionArgument(t *testing.T) {
	src := `
tag Short(uint32);
tag Long(string);

type Token = Short(uint32) | Long(string);
type Bag = { dummy: int }

extern<Bag> {
    pub fn accept(self: &mut Bag, value: Token) -> nothing {
        let _ = value;
    }
}

fn main() {
    let mut b: Bag = { dummy: 0 };
    b.accept(Short(1 to uint32));
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
		t.Fatalf("unexpected semantics errors: %s", diagnosticsSummary(bag))
	}
}
