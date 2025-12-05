package sema

import (
	"context"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
	"surge/internal/symbols"
)

func runConstSema(t *testing.T, src string) *diag.Bag {
	t.Helper()
	fs := source.NewFileSetWithBase("")
	fileID := fs.AddVirtual("test.sg", []byte(src))
	file := fs.Get(fileID)

	bag := diag.NewBag(16)
	builder := ast.NewBuilder(ast.Hints{}, nil)
	lx := lexer.New(file, lexer.Options{})
	parseRes := parser.ParseFile(context.Background(), fs, lx, builder, parser.Options{Reporter: &diag.BagReporter{Bag: bag}})
	if bag.Len() > 0 {
		t.Fatalf("unexpected parse diagnostics: %v", bag.Items())
	}

	res := symbols.ResolveFile(builder, parseRes.File, &symbols.ResolveOptions{
		Reporter:   &diag.BagReporter{Bag: bag},
		ModulePath: "test",
		FilePath:   file.Path,
	})
	Check(context.Background(), builder, parseRes.File, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  &res,
	})
	return bag
}

func collectCodes(bag *diag.Bag) []diag.Code {
	items := bag.Items()
	codes := make([]diag.Code, 0, len(items))
	for _, it := range items {
		codes = append(codes, it.Code)
	}
	return codes
}

func containsCode(codes []diag.Code, want diag.Code) bool {
	for _, c := range codes {
		if c == want {
			return true
		}
	}
	return false
}

func TestConstBasic(t *testing.T) {
	src := `
const A = 10;
const B: int = A + 2;

fn main() {
    let value = B;
}
`
	bag := runConstSema(t, src)
	if bag.Len() != 0 {
		t.Fatalf("unexpected diagnostics: %v", collectCodes(bag))
	}
}

func TestConstNotConstant(t *testing.T) {
	src := `
fn foo() -> int { return 1; }
let some_var: int = 3;

const BAD = foo();
const BAD2 = some_var + 1;
`
	bag := runConstSema(t, src)
	codes := collectCodes(bag)
	if !containsCode(codes, diag.SemaConstNotConstant) || bag.Len() < 2 {
		t.Fatalf("expected const not constant errors, got %v", codes)
	}
}

func TestConstCycle(t *testing.T) {
	src := `
const A = B + 1;
const B = A + 1;
`
	bag := runConstSema(t, src)
	codes := collectCodes(bag)
	if !containsCode(codes, diag.SemaConstCycle) {
		t.Fatalf("expected const cycle diagnostic, got %v", codes)
	}
}

func TestConstInType(t *testing.T) {
	src := `
const N = 4;

fn main() {
    let buf: int[N];
}
`
	bag := runConstSema(t, src)
	if bag.Len() != 0 {
		t.Fatalf("unexpected diagnostics: %v", collectCodes(bag))
	}
}
