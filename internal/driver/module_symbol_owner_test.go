package driver

import (
	"testing"

	"surge/internal/project"
	"surge/internal/source"
	"surge/internal/symbols"
)

// A record's own declaration is keyed under the record's path whatever flags
// it carries; a prelude copy of another file keeps the empty path, and a
// symbol that names its own module keeps it.
func TestModuleSymbolOwnerKeysDeclarations(t *testing.T) {
	rec := &moduleRecord{Meta: &project.ModuleMeta{Path: "app/lib"}, Files: []*source.File{{ID: 3}}}
	owner := newModuleSymbolOwner(rec)
	inRec, elsewhere := source.Span{File: 3, Start: 4, End: 9}, source.Span{File: 7, Start: 4, End: 9}
	prelude := symbols.SymbolFlagImported | symbols.SymbolFlagBuiltin
	for _, tc := range []struct {
		name string
		sym  symbols.Symbol
		want string
	}{
		{"own_plain_declaration", symbols.Symbol{Span: inRec}, "app/lib"},
		{"own_intrinsic_extern_method", symbols.Symbol{Flags: prelude, Span: inRec}, "app/lib"},
		{"prelude_copy_of_core", symbols.Symbol{Flags: prelude, Span: elsewhere}, ""},
		{"symbol_naming_its_module", symbols.Symbol{Span: inRec, ModulePath: "app/x"}, "app/x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := owner.declarationPath(&tc.sym, rec); got != tc.want {
				t.Fatalf("declarationPath = %q, want %q", got, tc.want)
			}
		})
	}
	if (moduleSymbolOwner{}).declares(&symbols.Symbol{Span: inRec}) {
		t.Fatal("a record without a path declares nothing")
	}
}
