package hir_test

import (
	"context"
	"fmt"
	"strings"
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

// Which body clones a type is decided once for the whole program, after every
// module has merged, and published back into the per-file result this lowering
// reads. This harness runs sema alone, so it reproduces the state that gap
// leaves behind: the request is recorded and the answer never arrives.
//
// Lowering on would emit an ordinary call to a function named `clone`, which is
// not what the program means and which nothing downstream would recognise as
// wrong. Refusing here keeps the seam closed.
func TestLowerRefusesACloneThatWasNeverAnswered(t *testing.T) {
	src := `
type Box = { text: string }
extern<Box> {
    pub fn __clone(self: &Box) -> Box {
        return Box { text = self.text };
    }
}
fn duplicate(value: &Box) -> Box {
    return clone(value);
}
`
	_, _, err := parseAndLower(t, src)
	if err == nil {
		t.Fatal("an unanswered clone lowered as an ordinary call")
	}
	if !strings.Contains(err.Error(), "no published implementation") {
		t.Fatalf("lowering error = %v", err)
	}
}

// The refusal has to be narrow. A Copy value is duplicated without any
// implementation to publish, and a clone inside a generic is answered by the
// deferred mechanism instead — neither records a direct request, so neither may
// be refused for lacking an answer to one.
func TestLowerAcceptsClonesThatNeedNoPublishedImplementation(t *testing.T) {
	cases := map[string]string{
		"copy type": `
@copy type Point = { x: int, y: int }
fn duplicate(value: &Point) -> Point {
    return clone(value);
}
`,
		"generic body": `
fn duplicate<T>(value: &T) -> T {
    return clone(value);
}
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseAndLower(t, src); err != nil {
				t.Fatalf("lowering refused a clone that needs no publication: %v", err)
			}
		})
	}
}

func parseAndLower(t *testing.T, src string) (*hir.Module, *types.Interner, error) {
	t.Helper()
	return parseAndLowerWithOptions(t, src, hir.LowerOptions{})
}

func parseAndLowerWithOptions(t *testing.T, src string, lowerOpts hir.LowerOptions) (*hir.Module, *types.Interner, error) {
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
		return nil, nil, fmt.Errorf("parse errors: %d", bag.Len())
	}

	// Run symbols resolution
	symbolsRes := symbols.ResolveFile(builder, result.File, &symbols.ResolveOptions{
		Reporter:   &diag.BagReporter{Bag: bag},
		Validate:   true,
		ModulePath: "core",
		FilePath:   "test.sg",
	})

	// Run sema
	semaOpts := sema.Options{
		Reporter:   &diag.BagReporter{Bag: bag},
		Symbols:    &symbolsRes,
		Types:      typeInterner,
		ModulePath: builder.StringsInterner.Intern("core"),
	}
	semaRes := sema.Check(context.Background(), builder, result.File, semaOpts)

	// Lower to HIR
	module, err := hir.LowerWithOptions(context.Background(), builder, result.File, &semaRes, &symbolsRes, lowerOpts)
	return module, typeInterner, err
}
