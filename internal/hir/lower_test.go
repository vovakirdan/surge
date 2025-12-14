package hir_test

import (
	"bytes"
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

func TestLowerSimpleFunction(t *testing.T) {
	src := `
fn add(a: int, b: int) -> int {
    return a + b;
}
`
	module, interner, err := parseAndLower(t, src)
	if err != nil {
		t.Fatalf("failed to lower: %v", err)
	}
	if module == nil {
		t.Fatal("module is nil")
	}

	if len(module.Funcs) != 1 {
		t.Errorf("expected 1 function, got %d", len(module.Funcs))
	}

	fn := module.Funcs[0]
	if fn.Name != "add" {
		t.Errorf("expected function name 'add', got %q", fn.Name)
	}

	if len(fn.Params) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(fn.Params))
	}

	if fn.Body == nil {
		t.Error("expected function body")
	} else if len(fn.Body.Stmts) == 0 {
		t.Error("expected statements in body")
	}

	// Test printing
	var buf bytes.Buffer
	if err := hir.Dump(&buf, module, interner); err != nil {
		t.Fatalf("failed to dump: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "fn add") {
		t.Error("output should contain 'fn add'")
	}
	if !strings.Contains(output, "return") {
		t.Error("output should contain 'return'")
	}
}

func TestLowerIfStatement(t *testing.T) {
	src := `
fn test(x: int) -> int {
    if x > 0 {
        return 1;
    } else {
        return 0;
    }
}
`
	module, _, err := parseAndLower(t, src)
	if err != nil {
		t.Fatalf("failed to lower: %v", err)
	}
	if module == nil {
		t.Fatal("module is nil")
	}

	if len(module.Funcs) != 1 {
		t.Errorf("expected 1 function, got %d", len(module.Funcs))
	}

	fn := module.Funcs[0]
	if fn.Body == nil || len(fn.Body.Stmts) == 0 {
		t.Fatal("expected statements in body")
	}

	// First statement should be an if
	if fn.Body.Stmts[0].Kind != hir.StmtIf {
		t.Errorf("expected StmtIf, got %v", fn.Body.Stmts[0].Kind)
	}
}

func TestLowerWhileLoop(t *testing.T) {
	src := `
fn loop_test() {
    let mut i = 0;
    while i < 10 {
        i = i + 1;
    }
}
`
	module, _, err := parseAndLower(t, src)
	if err != nil {
		t.Fatalf("failed to lower: %v", err)
	}
	if module == nil {
		t.Fatal("module is nil")
	}

	fn := module.Funcs[0]
	if fn.Body == nil || len(fn.Body.Stmts) < 2 {
		t.Fatal("expected at least 2 statements in body")
	}

	// Second statement should be a while
	if fn.Body.Stmts[1].Kind != hir.StmtWhile {
		t.Errorf("expected StmtWhile, got %v", fn.Body.Stmts[1].Kind)
	}
}

func TestLowerLetBinding(t *testing.T) {
	src := `
fn test() {
    let x = 42;
    let mut y = x + 1;
}
`
	module, _, err := parseAndLower(t, src)
	if err != nil {
		t.Fatalf("failed to lower: %v", err)
	}
	if module == nil {
		t.Fatal("module is nil")
	}

	fn := module.Funcs[0]
	if fn.Body == nil || len(fn.Body.Stmts) < 2 {
		t.Fatal("expected at least 2 statements in body")
	}

	// Both statements should be let
	if fn.Body.Stmts[0].Kind != hir.StmtLet {
		t.Errorf("expected StmtLet for stmt 0, got %v", fn.Body.Stmts[0].Kind)
	}
	if fn.Body.Stmts[1].Kind != hir.StmtLet {
		t.Errorf("expected StmtLet for stmt 1, got %v", fn.Body.Stmts[1].Kind)
	}

	// Check mutability
	data0 := fn.Body.Stmts[0].Data.(hir.LetData)
	if data0.IsMut {
		t.Error("first let should be immutable")
	}

	data1 := fn.Body.Stmts[1].Data.(hir.LetData)
	if !data1.IsMut {
		t.Error("second let should be mutable")
	}
}

func parseAndLower(t *testing.T, src string) (*hir.Module, *types.Interner, error) {
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
		ModulePath: "test",
		FilePath:   "test.sg",
	})

	// Run sema
	semaOpts := sema.Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  &symbolsRes,
		Types:    typeInterner,
	}
	semaRes := sema.Check(context.Background(), builder, result.File, semaOpts)

	// Lower to HIR
	module, err := hir.Lower(context.Background(), builder, result.File, &semaRes, &symbolsRes)
	return module, typeInterner, err
}
