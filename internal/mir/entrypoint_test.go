package mir_test

import (
	"testing"

	"surge/internal/hir"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/symbols"
	"surge/internal/types"
)

// TestBuildSurgeStart_NoEntrypoint tests that nil is returned when no entrypoint exists.
func TestBuildSurgeStart_NoEntrypoint(t *testing.T) {
	typeInterner := types.NewInterner()

	mm := &mono.MonoModule{
		Source: &hir.Module{
			TypeInterner: typeInterner,
		},
		Funcs: make(map[mono.MonoKey]*mono.MonoFunc),
	}

	// Add a regular function (not entrypoint)
	key := mono.MonoKey{Sym: symbols.SymbolID(1)}
	mm.Funcs[key] = &mono.MonoFunc{
		Key:  key,
		Func: &hir.Func{Name: "regular", SymbolID: symbols.SymbolID(1)},
	}

	f, err := mir.BuildSurgeStart(mm, nil, typeInterner, 1, nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if f != nil {
		t.Errorf("expected nil for no entrypoint, got %v", f)
	}
}

// TestBuildSurgeStart_ReturnsNothing tests __surge_start generation for entrypoint returning nothing.
func TestBuildSurgeStart_ReturnsNothing(t *testing.T) {
	typeInterner := types.NewInterner()
	nothingType := typeInterner.Builtins().Nothing

	mm := &mono.MonoModule{
		Source: &hir.Module{
			TypeInterner: typeInterner,
			Symbols: &symbols.Result{
				Table: symbols.NewTable(symbols.Hints{}, nil),
			},
		},
		Funcs: make(map[mono.MonoKey]*mono.MonoFunc),
	}

	// Add entrypoint function returning nothing
	key := mono.MonoKey{Sym: symbols.SymbolID(1)}
	mm.Funcs[key] = &mono.MonoFunc{
		Key: key,
		Func: &hir.Func{
			Name:     "main",
			SymbolID: symbols.SymbolID(1),
			Result:   nothingType,
			Flags:    hir.FuncEntrypoint,
		},
	}

	f, err := mir.BuildSurgeStart(mm, nil, typeInterner, 1, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil {
		t.Fatal("expected __surge_start function, got nil")
	}
	if f.Name != "__surge_start" {
		t.Errorf("expected name __surge_start, got %s", f.Name)
	}
	if f.ID != 1 {
		t.Errorf("expected ID 1, got %d", f.ID)
	}
	if len(f.Blocks) == 0 {
		t.Error("expected at least one block")
	}
}

// TestBuildSurgeStart_ReturnsInt tests __surge_start generation for entrypoint returning int.
func TestBuildSurgeStart_ReturnsInt(t *testing.T) {
	typeInterner := types.NewInterner()
	intType := typeInterner.Builtins().Int

	mm := &mono.MonoModule{
		Source: &hir.Module{
			TypeInterner: typeInterner,
			Symbols: &symbols.Result{
				Table: symbols.NewTable(symbols.Hints{}, nil),
			},
		},
		Funcs: make(map[mono.MonoKey]*mono.MonoFunc),
	}

	// Add entrypoint function returning int
	key := mono.MonoKey{Sym: symbols.SymbolID(1)}
	mm.Funcs[key] = &mono.MonoFunc{
		Key: key,
		Func: &hir.Func{
			Name:     "main",
			SymbolID: symbols.SymbolID(1),
			Result:   intType,
			Flags:    hir.FuncEntrypoint,
		},
	}

	f, err := mir.BuildSurgeStart(mm, nil, typeInterner, 1, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil {
		t.Fatal("expected __surge_start function, got nil")
	}
	if f.Name != "__surge_start" {
		t.Errorf("expected name __surge_start, got %s", f.Name)
	}

	// Should have locals for entry_ret and code
	if len(f.Locals) < 2 {
		t.Errorf("expected at least 2 locals (entry_ret, code), got %d", len(f.Locals))
	}
}

// TestBuildSurgeStart_NilModule tests that nil module returns nil function.
func TestBuildSurgeStart_NilModule(t *testing.T) {
	typeInterner := types.NewInterner()

	f, err := mir.BuildSurgeStart(nil, nil, typeInterner, 1, nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if f != nil {
		t.Errorf("expected nil for nil module, got %v", f)
	}
}
