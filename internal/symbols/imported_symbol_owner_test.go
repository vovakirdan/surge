package symbols

import (
	"testing"

	"surge/internal/diag"
)

func TestResolveImportedSymbolOwner(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    SymbolKind
		builtin bool
		want    string
	}{
		{"function", SymbolFunction, false, "dep"},
		{"builtin_function", SymbolFunction, true, "app/dep"},
		{"const", SymbolConst, false, "dep"},
		{"builtin_const", SymbolConst, true, "dep"},
		{"type", SymbolType, false, "dep"},
		{"builtin_type", SymbolType, true, "dep"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "import app/dep::{value as imported};\nfn run() { return imported; }"
			builder, fileID, parseBag := parseSnippet(t, src)
			if parseBag.HasErrors() {
				t.Fatalf("PRECONDITION: parse: %v", parseBag.Items())
			}
			exported := &ExportedSymbol{Name: "value", Kind: tc.kind, Flags: SymbolFlagPublic}
			if tc.builtin {
				exported.Flags |= SymbolFlagBuiltin
			}
			if tc.kind == SymbolFunction {
				exported.Signature = &FunctionSignature{Result: "int"}
			}
			exports := NewModuleExports("dep")
			exports.Add(exported)
			bag := diag.NewBag(8)
			res := ResolveFile(builder, fileID, &ResolveOptions{
				Reporter: &diag.BagReporter{Bag: bag}, Validate: true,
				ModuleExports: map[string]*ModuleExports{"app/dep": exports},
			})
			if bag.HasErrors() {
				t.Fatalf("resolve: %v", bag.Items())
			}
			found := false
			for _, id := range res.ExprSymbols {
				sym := res.Table.Symbols.Get(id)
				if sym == nil || sym.Kind != tc.kind || sym.Flags&SymbolFlagImported == 0 {
					continue
				}
				found = true
				if sym.ModulePath != tc.want {
					t.Fatalf("selected imported %v path=%q, want %q", tc.kind, sym.ModulePath, tc.want)
				}
			}
			if !found {
				t.Fatal("no selected imported value")
			}
		})
	}
}
