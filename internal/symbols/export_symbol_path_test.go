package symbols

import (
	"testing"

	"surge/internal/diag"
)

// A free function, a const or a type imported under a spelling of its module
// that is not the module's own path takes the path of the module whose export
// container proves it owns them; a method keeps the requested path, and so
// does every export of a container that proves no owner.
func TestExportSymbolPathNamesAProvenOwner(t *testing.T) {
	fn := &ExportedSymbol{Name: "f", Kind: SymbolFunction, Signature: &FunctionSignature{Result: "int"}}
	method := &ExportedSymbol{Name: "m", Kind: SymbolFunction, Flags: SymbolFlagMethod, ReceiverKey: "T",
		Signature: &FunctionSignature{Params: []TypeKey{"&T"}, Result: "int", HasSelf: true}}
	constant := &ExportedSymbol{Name: "C", Kind: SymbolConst}
	typ := &ExportedSymbol{Name: "T", Kind: SymbolType}
	variable := &ExportedSymbol{Name: "v", Kind: SymbolLet}
	proven, unproven := NewModuleExports("dep"), NewModuleExports("")
	for _, tc := range []struct {
		name    string
		exports *ModuleExports
		exp     *ExportedSymbol
		want    string
	}{
		{"free_function_with_a_proven_owner", proven, fn, "dep"},
		{"const_with_a_proven_owner", proven, constant, "dep"},
		{"type_with_a_proven_owner", proven, typ, "dep"},
		{"method_keeps_the_requested_path", proven, method, "app/dep"},
		{"other_kind_keeps_the_requested_path", proven, variable, "app/dep"},
		{"free_function_without_a_proven_owner", unproven, fn, "app/dep"},
		{"const_without_a_proven_owner", unproven, constant, "app/dep"},
		{"type_without_a_proven_owner", unproven, typ, "app/dep"},
		{"no_container", nil, constant, "app/dep"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExportSymbolPath("app/dep", tc.exports, tc.exp); got != tc.want {
				t.Fatalf("ExportSymbolPath = %q, want %q", got, tc.want)
			}
		})
	}
}

// `import app/dep::C` through a spelling of the module that is not its own
// path: the copy of the const carries the owner's path when the container
// proves one, and the spelled path when it does not. (A type name used in an
// expression resolves through the type lookup and makes no copy here.)
func TestResolveImportedConstOwner(t *testing.T) {
	src := "import app/dep::C;\nfn run() -> int {\n    return C;\n}\n"
	for _, tc := range []struct {
		name, owner, want string
	}{
		{"proven_owner", "dep", "dep"},
		{"no_proven_owner", "", "app/dep"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			builder, fileID, parseBag := parseSnippet(t, src)
			if parseBag.Len() != 0 {
				t.Fatalf("PRECONDITION: parse diagnostics: %d", parseBag.Len())
			}
			exports := NewModuleExports(tc.owner)
			exports.Add(&ExportedSymbol{Name: "C", Kind: SymbolConst, Flags: SymbolFlagPublic})
			bag := diag.NewBag(8)
			res := ResolveFile(builder, fileID, &ResolveOptions{
				Reporter: &diag.BagReporter{Bag: bag}, Validate: true,
				ModuleExports: map[string]*ModuleExports{"app/dep": exports},
			})
			found := map[SymbolKind]string{}
			for i := 1; i <= res.Table.Symbols.Len(); i++ {
				sym := res.Table.Symbols.Get(SymbolID(i)) //nolint:gosec // bounded by the table length
				if sym != nil && sym.Flags&SymbolFlagImported != 0 && sym.Kind == SymbolConst {
					if prev, seen := found[sym.Kind]; seen && prev != sym.ModulePath {
						t.Fatalf("two copies of one %v disagree on their module: %q, %q", sym.Kind, prev, sym.ModulePath)
					}
					found[sym.Kind] = sym.ModulePath
				}
			}
			if len(found) != 1 {
				t.Fatalf("PRECONDITION: want an imported const copy, got %v (bag %d)", found, bag.Len())
			}
			for kind, path := range found {
				if path != tc.want {
					t.Fatalf("imported %v copy names module %q, want %q", kind, path, tc.want)
				}
			}
		})
	}
}
