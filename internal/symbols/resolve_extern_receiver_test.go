package symbols

import (
	"testing"

	"surge/internal/diag"
)

func TestResolveExternConcreteByteReceiverHasNoSyntheticTypeParam(t *testing.T) {
	src := `
            extern<Array<byte>> {
                fn append_string(self: &mut Array<byte>, other: string) -> nothing;
            }
        `
	builder, fileID, parseBag := parseSnippet(t, src)
	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}

	bag := diag.NewBag(8)
	result := ResolveFile(builder, fileID, &ResolveOptions{
		Reporter: &diag.BagReporter{Bag: bag},
		Validate: true,
	})
	if bag.HasErrors() {
		t.Fatalf("unexpected symbol diagnostics: %+v", bag.Items())
	}
	name := builder.StringsInterner.Intern("append_string")
	for id, sym := range result.Table.Symbols.Data() {
		if sym.Kind == SymbolFunction && sym.Name == name {
			if len(sym.TypeParams) != 0 {
				t.Fatalf("append_string symbol %d has synthetic receiver parameters %v", id+1, sym.TypeParams)
			}
			return
		}
	}
	t.Fatal("append_string symbol not found")
}
