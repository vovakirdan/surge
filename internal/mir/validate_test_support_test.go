package mir_test

import (
	"context"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/hir"
	"surge/internal/lexer"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// parseAndLowerMIR parses source code and lowers to MIR for testing.
func parseAndLowerMIR(t *testing.T, src string) (*mir.Module, *types.Interner, error) {
	t.Helper()

	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte(src))
	file := fs.Get(fileID)

	sharedStrings := source.NewInterner()
	typeInterner := types.NewInterner()

	bag := diag.NewBag(100)
	lx := lexer.New(file, lexer.Options{})
	builder := ast.NewBuilder(ast.Hints{}, sharedStrings)

	opts := parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: 100,
	}

	result := parser.ParseFile(context.Background(), fs, lx, builder, opts)
	if bag.HasErrors() {
		for _, d := range bag.Items() {
			t.Logf("parse error: %v", d)
		}
		return nil, nil, nil
	}

	// Run symbols resolution
	symbolsRes := symbols.ResolveFile(builder, result.File, &symbols.ResolveOptions{
		Reporter:   &diag.BagReporter{Bag: bag},
		Validate:   true,
		ModulePath: "test",
		FilePath:   "test.sg",
	})

	// Create instantiation map for sema
	instMap := mono.NewInstantiationMap()

	// Run sema
	semaOpts := sema.Options{
		Reporter:       &diag.BagReporter{Bag: bag},
		Symbols:        &symbolsRes,
		Types:          typeInterner,
		Instantiations: mono.NewInstantiationMapRecorder(instMap),
	}
	semaRes := sema.Check(context.Background(), builder, result.File, semaOpts)

	if bag.HasErrors() {
		for _, d := range bag.Items() {
			t.Logf("sema error: %v", d)
		}
		return nil, nil, nil
	}
	finalizeTestInstantiationClosure(t, typeInterner, &symbolsRes, &semaRes)

	// Lower to HIR
	hirModule, err := hir.Lower(context.Background(), builder, result.File, &semaRes, &symbolsRes)
	if err != nil {
		return nil, nil, err
	}

	// Monomorphize
	monoMod, err := mono.MonomorphizeModule(hirModule, instMap, &semaRes, mono.Options{})
	if err != nil {
		return nil, nil, err
	}

	// Lower to MIR
	mirMod, err := mir.LowerModule(monoMod, &semaRes)
	if err != nil {
		return nil, nil, err
	}

	return mirMod, typeInterner, nil
}
