package mono

import (
	"context"
	"fmt"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/hir"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func compileAndMonomorphize(t *testing.T, src string) (*MonoModule, *types.Interner, error) {
	t.Helper()
	return compileAndMonomorphizeWithLowerOptions(t, src, hir.LowerOptions{})
}

func compileAndMonomorphizeWithLowerOptions(t *testing.T, src string, lowerOpts hir.LowerOptions) (*MonoModule, *types.Interner, error) {
	t.Helper()
	return compileAndMonomorphizeWithRecorder(t, src, lowerOpts, true)
}

func compileAndMonomorphizeWithRecorder(t *testing.T, src string, lowerOpts hir.LowerOptions, withRecorder bool) (*MonoModule, *types.Interner, error) {
	t.Helper()
	return compileAndMonomorphizeOptions(t, src, lowerOpts, withRecorder, true)
}

func compileAndMonomorphizeOptions(t *testing.T, src string, lowerOpts hir.LowerOptions, withRecorder, finalize bool) (*MonoModule, *types.Interner, error) {
	t.Helper()
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte(src))
	file := fs.Get(fileID)

	sharedStrings := source.NewInterner()
	typeInterner := types.NewInterner()
	instMap := NewInstantiationMap()

	bag := diag.NewBag(100)
	lx := lexer.New(file, lexer.Options{})
	builder := ast.NewBuilder(ast.Hints{}, sharedStrings)

	opts := parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: 100,
	}

	result := parser.ParseFile(context.Background(), fs, lx, builder, opts)
	if bag.HasErrors() {
		return nil, nil, fmt.Errorf("parse errors: %s", monoDiagSummary(bag))
	}

	symbolsRes := symbols.ResolveFile(builder, result.File, &symbols.ResolveOptions{
		Reporter:   &diag.BagReporter{Bag: bag},
		Validate:   true,
		ModulePath: "core",
		FilePath:   "test.sg",
	})
	if bag.HasErrors() {
		return nil, nil, fmt.Errorf("symbol errors: %s", monoDiagSummary(bag))
	}

	semaOpts := sema.Options{
		Reporter:   &diag.BagReporter{Bag: bag},
		Symbols:    &symbolsRes,
		Types:      typeInterner,
		ModulePath: builder.StringsInterner.Intern("core"),
	}
	if withRecorder {
		semaOpts.Instantiations = NewInstantiationMapRecorder(instMap)
	}
	semaRes := sema.Check(context.Background(), builder, result.File, semaOpts)
	if bag.HasErrors() {
		return nil, nil, fmt.Errorf("sema errors: %s", monoDiagSummary(bag))
	}
	if finalize {
		identity, identityErr := sema.NewInstantiationKeyContext(typeInterner, &symbolsRes, func(id source.FileID) (string, error) {
			if fs.Get(id) == nil {
				return "", fmt.Errorf("unknown source file %d", id)
			}
			return "test.sg", nil
		})
		if identityErr != nil {
			return nil, nil, identityErr
		}
		semaRes.InstantiationIdentity = &identity
		if closureErr := semaRes.FinalizeInstantiationClosure(identity, 64); closureErr != nil {
			return nil, nil, closureErr
		}
	}

	mod, err := hir.LowerWithOptions(context.Background(), builder, result.File, &semaRes, &symbolsRes, lowerOpts)
	if err != nil {
		return nil, nil, err
	}

	mm, err := MonomorphizeModule(mod, instMap, &semaRes, Options{})
	return mm, typeInterner, err
}
