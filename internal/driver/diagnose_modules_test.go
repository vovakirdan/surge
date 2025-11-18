package driver

import (
	"sort"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestStdlibExportsVisibleInModuleGraph(t *testing.T) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("play.sg", []byte("fn main() {}"))
	file := fs.Get(fileID)

	bag := diag.NewBag(8)
	lx := lexer.New(file, lexer.Options{})
	builder := ast.NewBuilder(ast.Hints{}, nil)
	parseRes := parser.ParseFile(fs, lx, builder, parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: 16,
	})
	typeInterner := types.NewInterner()
	opts := DiagnoseOptions{
		Stage:          DiagnoseStageSema,
		MaxDiagnostics: 32,
	}
	exports, err := runModuleGraph(fs, file, builder, parseRes.File, bag, opts, NewModuleCache(4), typeInterner)
	if err != nil {
		t.Fatalf("runModuleGraph failed: %v", err)
	}

	for _, module := range []string{stdModuleCoreIntrinsics, stdModuleCoreOption, stdModuleCoreResult} {
		if exports[module] == nil {
			t.Fatalf("expected exports for %s", module)
		}
	}

	optionExports := exports[stdModuleCoreOption]
	option := optionExports.Lookup("Option")
	if len(option) == 0 || len(option[0].TypeParams) == 0 {
		t.Fatalf("expected generic Option export, got %+v", option)
	}
	if info, ok := typeInterner.UnionInfo(option[0].Type); !ok || info == nil || info.Name != option[0].NameID {
		t.Fatalf("union info for Option missing or mismatched name: %+v", info)
	}

	intrinsics := exports[stdModuleCoreIntrinsics]
	if _, ok := intrinsics.Symbols["exit"]; !ok {
		t.Fatalf("expected exit in %s exports, have %v", stdModuleCoreIntrinsics, exportKeys(intrinsics.Symbols))
	}
}

func exportKeys(m map[string][]symbols.ExportedSymbol) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
