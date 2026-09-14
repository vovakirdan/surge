package driver

import (
	"context"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/project"
	"surge/internal/source"
	"surge/internal/symbols"
)

func returnSourceRemapSignature(t *testing.T, marked int) *symbols.FunctionSignature {
	t.Helper()
	params := []string{"a: &string", "b: &string"}
	if marked >= 0 {
		params[marked] = "@return_source " + params[marked]
	}
	fs := source.NewFileSetWithBase("")
	id := fs.AddVirtual("return_source.sg", []byte("fn subject("+params[0]+", "+params[1]+") -> &string;"))
	builder := ast.NewBuilder(ast.Hints{}, nil)
	bag := diag.NewBag(16)
	parser.ParseFile(context.Background(), fs, lexer.New(fs.Get(id), lexer.Options{}), builder, parser.Options{Reporter: &diag.BagReporter{Bag: bag}})
	if bag.HasErrors() {
		t.Fatal(bag.Items())
	}
	return &symbols.FunctionSignature{Params: []symbols.TypeKey{"&string", "&string"}, Result: "&string", ReturnSourceSyntax: symbols.FunctionReturnSourceSyntax(builder, builder.Items.Fns.Get(1))}
}

func TestReturnSourceCoreRemapIdentity(t *testing.T) {
	for _, name := range []string{"distinct_sources", "same_sources", "legacy_all"} {
		t.Run(name, func(t *testing.T) {
			strs := source.NewInterner()
			rootTable := symbols.NewTable(symbols.Hints{}, strs)
			coreTable := symbols.NewTable(symbols.Hints{}, strs)
			marked := 0
			if name == "legacy_all" {
				marked = -1
			}
			sig := returnSourceRemapSignature(t, marked)
			add := func(table *symbols.Table, signature *symbols.FunctionSignature, imported bool) symbols.SymbolID {
				flags := symbols.SymbolFlagPublic
				if imported {
					flags |= symbols.SymbolFlagImported
				}
				return table.Symbols.New(&symbols.Symbol{Name: strs.Intern("subject"), Kind: symbols.SymbolFunction, ModulePath: "core", Flags: flags, Signature: signature})
			}
			rootA := add(rootTable, sig, true)
			coreA := add(coreTable, sig, false)
			var rootB, coreB symbols.SymbolID
			if name == "distinct_sources" {
				other := returnSourceRemapSignature(t, 1)
				rootB, coreB = add(rootTable, other, true), add(coreTable, other, false)
			}
			mapping := buildCoreSymbolRemap(&symbols.Result{Table: rootTable}, &moduleRecord{Table: coreTable, Meta: &project.ModuleMeta{Path: "core"}})
			if mapping[coreA] != rootA {
				t.Fatalf("core declaration mapped to %d, want %d", mapping[coreA], rootA)
			}
			if name == "distinct_sources" && (mapping[coreB] != rootB || mapping[coreA] == mapping[coreB]) {
				t.Fatal("core import remap collapsed distinct declared promises")
			}
			if name == "legacy_all" && signatureKey(sig) != "&string,&string,->&string" {
				t.Fatal("legacy AllInputs import key changed")
			}
		})
	}
}
