package driver

import (
	"testing"

	"surge/internal/hir"
	"surge/internal/project"
	"surge/internal/source"
	"surge/internal/symbols"
)

func TestRemapHIRModuleSharedExprRemappedOnce(t *testing.T) {
	expr := &hir.Expr{
		Kind: hir.ExprVarRef,
		Data: hir.VarRefData{
			Name:     "x",
			SymbolID: symbols.SymbolID(1),
		},
	}
	stmt := hir.Stmt{
		Kind: hir.StmtAssign,
		Data: hir.AssignData{
			Target: expr,
			Value:  expr,
		},
	}
	mod := &hir.Module{
		Funcs: []*hir.Func{
			{
				Name:     "f",
				SymbolID: symbols.SymbolID(10),
				Body: &hir.Block{
					Stmts: []hir.Stmt{stmt},
				},
			},
		},
	}
	mapping := map[symbols.SymbolID]symbols.SymbolID{
		symbols.SymbolID(1): symbols.SymbolID(2),
		symbols.SymbolID(2): symbols.SymbolID(3),
	}
	remapHIRModule(mod, mapping)
	data, ok := expr.Data.(hir.VarRefData)
	if !ok {
		t.Fatalf("expected VarRefData, got %T", expr.Data)
	}
	if data.SymbolID != symbols.SymbolID(2) {
		t.Fatalf("expected symbol to remap once to 2, got %d", data.SymbolID)
	}
}

func TestBuildCoreSymbolRemapIncludesLocals(t *testing.T) {
	strings := source.NewInterner()
	rootTable := symbols.NewTable(symbols.Hints{}, strings)
	coreTable := symbols.NewTable(symbols.Hints{}, strings)

	name := strings.Intern("foo")
	rootTable.Symbols.New(&symbols.Symbol{
		Name:       name,
		Kind:       symbols.SymbolFunction,
		ModulePath: "core",
		Flags:      symbols.SymbolFlagImported | symbols.SymbolFlagBuiltin,
	})
	coreTable.Symbols.New(&symbols.Symbol{
		Name:       name,
		Kind:       symbols.SymbolFunction,
		ModulePath: "core",
		Flags:      symbols.SymbolFlagPublic,
	})

	localID := coreTable.Symbols.New(&symbols.Symbol{
		Name: strings.Intern("local"),
		Kind: symbols.SymbolParam,
	})

	mapping := buildCoreSymbolRemap(&symbols.Result{Table: rootTable}, &moduleRecord{
		Table: coreTable,
		Meta:  &project.ModuleMeta{Path: "core"},
	})

	if _, ok := mapping[localID]; !ok {
		t.Fatalf("expected local symbol to be remapped")
	}
}

func TestBuildCoreSymbolRemapKeepsPreludeBuiltinDistinctFromCoreAlias(t *testing.T) {
	strs := source.NewInterner()
	rootTable := symbols.NewTable(symbols.Hints{}, strs)
	coreTable := symbols.NewTable(symbols.Hints{}, strs)
	name := strs.Intern("Array")
	rootBuiltin := newDriverBuiltinTypeSymbol(rootTable)
	rootTable.Symbols.New(&symbols.Symbol{
		Name:       name,
		Kind:       symbols.SymbolType,
		ModulePath: "core",
		Flags:      symbols.SymbolFlagImported | symbols.SymbolFlagBuiltin,
	})
	coreBuiltin := newDriverBuiltinTypeSymbol(coreTable)

	mapping := buildCoreSymbolRemap(&symbols.Result{Table: rootTable}, &moduleRecord{
		Table: coreTable,
		Meta:  &project.ModuleMeta{Path: "core"},
	})
	if got := mapping[coreBuiltin]; got != rootBuiltin {
		t.Fatalf("core prelude Array mapped to %d, want root prelude Array %d", got, rootBuiltin)
	}
}

func TestBuildModuleSymbolRemapMapsDefaultPreludeBuiltin(t *testing.T) {
	strs := source.NewInterner()
	rootTable := symbols.NewTable(symbols.Hints{}, strs)
	moduleTable := symbols.NewTable(symbols.Hints{}, strs)
	rootArray := newDriverBuiltinTypeSymbol(rootTable)
	moduleArray := newDriverBuiltinTypeSymbol(moduleTable)

	mapping := buildModuleSymbolRemap(&symbols.Result{Table: rootTable}, &moduleRecord{
		Table: moduleTable,
		Meta:  &project.ModuleMeta{Path: "remote"},
	})
	if got := mapping[moduleArray]; got != rootArray {
		t.Fatalf("module prelude Array mapped to %d, want root prelude Array %d", got, rootArray)
	}
}

func TestInstantiationSymbolRemapCacheSurvivesSourceMetadataMutation(t *testing.T) {
	strs := source.NewInterner()
	rootTable := symbols.NewTable(symbols.Hints{}, strs)
	moduleTable := symbols.NewTable(symbols.Hints{}, strs)
	name := strs.Intern("print")
	rootPrint := rootTable.Symbols.New(&symbols.Symbol{
		Name:       name,
		Kind:       symbols.SymbolFunction,
		ModulePath: "core",
		Flags:      symbols.SymbolFlagImported,
	})
	modulePrint := moduleTable.Symbols.New(&symbols.Symbol{
		Name:       name,
		Kind:       symbols.SymbolFunction,
		ModulePath: "core",
		Flags:      symbols.SymbolFlagPublic,
	})
	rec := &moduleRecord{Table: moduleTable, Meta: &project.ModuleMeta{Path: "core"}}
	mapping := buildCoreSymbolRemap(&symbols.Result{Table: rootTable}, rec)
	if got := mapping[modulePrint]; got != rootPrint {
		t.Fatalf("initial print mapping = %d, want %d", got, rootPrint)
	}
	cacheInstantiationSymbolRemap(rec, rootTable, mapping)

	// Once closure finalization selects a mapping, later consumers must reuse
	// it even if another compiler phase enriches source symbol metadata.
	moduleTable.Symbols.Get(modulePrint).TypeParams = []source.StringID{strs.Intern("T")}
	if recomputed := buildCoreSymbolRemap(&symbols.Result{Table: rootTable}, rec)[modulePrint]; recomputed == rootPrint {
		t.Fatal("test requires post-recheck metadata to drift under recomputation")
	}
	cached, ok := cachedInstantiationSymbolRemap(rec, rootTable)
	if !ok {
		t.Fatal("missing authoritative cached mapping")
	}
	if got := cached[modulePrint]; got != rootPrint {
		t.Fatalf("cached print mapping drifted to %d, want %d", got, rootPrint)
	}
}

func newDriverBuiltinTypeSymbol(table *symbols.Table) symbols.SymbolID {
	return table.Symbols.New(&symbols.Symbol{
		Name:  table.Strings.Intern("Array"),
		Kind:  symbols.SymbolType,
		Flags: symbols.SymbolFlagBuiltin,
	})
}
