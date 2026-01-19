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
