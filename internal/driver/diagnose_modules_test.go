package driver

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	prevStd := os.Getenv("SURGE_STDLIB")
	_ = os.Setenv("SURGE_STDLIB", filepath.Join("..", ".."))
	t.Cleanup(func() {
		_ = os.Setenv("SURGE_STDLIB", prevStd)
	})

	fs := source.NewFileSet()
	fileID := fs.AddVirtual("play.sg", []byte("fn main() {}"))
	file := fs.Get(fileID)

	bag := diag.NewBag(8)
	lx := lexer.New(file, lexer.Options{})
	sharedStrings := source.NewInterner()
	builder := ast.NewBuilder(ast.Hints{}, sharedStrings)
	parseRes := parser.ParseFile(fs, lx, builder, parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: 16,
	})
	typeInterner := types.NewInterner()
	opts := DiagnoseOptions{
		Stage:          DiagnoseStageSema,
		MaxDiagnostics: 32,
	}
	exports, err := runModuleGraph(fs, file, builder, parseRes.File, bag, opts, NewModuleCache(4), typeInterner, sharedStrings)
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
	if meth := optionExports.Lookup("safe"); len(meth) == 0 || meth[0].ReceiverKey == "" {
		t.Fatalf("expected method safe on Option, got %+v (available: %v, scope=%s)", meth, exportKeys(optionExports.Symbols), dumpOptionScope(fs, exports))
	}

	intrinsics := exports[stdModuleCoreIntrinsics]
	if _, ok := intrinsics.Symbols["exit"]; !ok {
		t.Fatalf("expected exit in %s exports, have %v", stdModuleCoreIntrinsics, exportKeys(intrinsics.Symbols))
	}
	nextExports := intrinsics.Lookup("next")
	foundMethod := false
	for _, exp := range nextExports {
		if exp.ReceiverKey != "" {
			foundMethod = true
			break
		}
	}
	if !foundMethod {
		t.Fatalf("expected method next on Range in %s exports, have %+v", stdModuleCoreIntrinsics, nextExports)
	}

	intrTask := intrinsics.Lookup("Task")
	if len(intrTask) == 0 || len(intrTask[0].TypeParams) != 1 {
		t.Fatalf("expected generic Task export in %s, got %+v", stdModuleCoreIntrinsics, intrTask)
	}
	awaitExports := intrinsics.Lookup("await")
	foundAwait := false
	for _, exp := range awaitExports {
		if exp.ReceiverKey != "" {
			foundAwait = true
			break
		}
	}
	if !foundAwait {
		t.Fatalf("expected await method on Task in %s, got %+v (available: %v)", stdModuleCoreIntrinsics, awaitExports, exportKeys(intrinsics.Symbols))
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

func dumpOptionScope(fs *source.FileSet, exports map[string]*symbols.ModuleExports) string {
	optionPath := filepath.Join("..", "..", "core", "option.sg")
	fileID, err := fs.Load(optionPath)
	if err != nil {
		return fmt.Sprintf("load err=%v", err)
	}
	file := fs.Get(fileID)
	lx := lexer.New(file, lexer.Options{})
	builder := ast.NewBuilder(ast.Hints{}, nil)
	parseRes := parser.ParseFile(fs, lx, builder, parser.Options{MaxErrors: 8})
	externs := 0
	if file := builder.Files.Get(parseRes.File); file != nil {
		for _, itemID := range file.Items {
			item := builder.Items.Get(itemID)
			if item != nil && item.Kind == ast.ItemExtern {
				externs++
			}
		}
	}
	if parseRes.Bag != nil && parseRes.Bag.HasErrors() {
		msgs := parseRes.Bag.Items()
		texts := make([]string, 0, len(msgs))
		for _, d := range msgs {
			texts = append(texts, d.Message)
		}
		return fmt.Sprintf("externs=%d, parse errors=%s", externs, strings.Join(texts, "|"))
	}
	res := symbols.ResolveFile(builder, parseRes.File, &symbols.ResolveOptions{
		ModulePath:    "core/option",
		FilePath:      optionPath,
		BaseDir:       fs.BaseDir(),
		ModuleExports: exports,
	})
	scope := res.Table.Scopes.Get(res.FileScope)
	if scope == nil {
		return fmt.Sprintf("externs=%d, no scope", externs)
	}
	names := make([]string, 0, len(scope.Symbols))
	for _, id := range scope.Symbols {
		if sym := res.Table.Symbols.Get(id); sym != nil {
			names = append(names, res.Table.Strings.MustLookup(sym.Name))
		}
	}
	sort.Strings(names)
	return fmt.Sprintf("externs=%d, names=%s", externs, strings.Join(names, ","))
}
