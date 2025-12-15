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

func TestMonoHasNoTypeParams(t *testing.T) {
	src := `
fn id<T>(x: T) -> T { return x; }

fn wrap<T>(x: T) -> T {
  return id(x);
}

fn main() {
  let a = wrap(1);
  let b = wrap("x");
}
`

	mm, typesIn, err := compileAndMonomorphize(t, src)
	if err != nil {
		t.Fatalf("failed to monomorphize: %v", err)
	}
	if err := validateMonoModuleNoTypeParams(mm, typesIn); err != nil {
		t.Fatalf("mono contains type params: %v", err)
	}
	if got, want := len(mm.Funcs), 5; got != want {
		t.Fatalf("unexpected mono func count: got=%d want=%d", got, want)
	}
}

func compileAndMonomorphize(t *testing.T, src string) (*MonoModule, *types.Interner, error) {
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
		return nil, nil, fmt.Errorf("parse errors: %d", bag.Len())
	}

	symbolsRes := symbols.ResolveFile(builder, result.File, &symbols.ResolveOptions{
		Reporter:   &diag.BagReporter{Bag: bag},
		Validate:   true,
		ModulePath: "test",
		FilePath:   "test.sg",
	})
	if bag.HasErrors() {
		return nil, nil, fmt.Errorf("symbol errors: %d", bag.Len())
	}

	semaOpts := sema.Options{
		Reporter:       &diag.BagReporter{Bag: bag},
		Symbols:        &symbolsRes,
		Types:          typeInterner,
		Instantiations: NewInstantiationMapRecorder(instMap),
	}
	semaRes := sema.Check(context.Background(), builder, result.File, semaOpts)
	if bag.HasErrors() {
		return nil, nil, fmt.Errorf("sema errors: %d", bag.Len())
	}

	mod, err := hir.Lower(context.Background(), builder, result.File, &semaRes, &symbolsRes)
	if err != nil {
		return nil, nil, err
	}

	mm, err := MonomorphizeModule(mod, instMap, &semaRes, Options{})
	return mm, typeInterner, err
}
