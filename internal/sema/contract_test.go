package sema

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
	"surge/internal/symbols"
)

func TestContractSemantics_Positive(t *testing.T) {
	src := `
contract Hashable<T>{
    field data: T;
    fn hash(self: T) -> uint;
    fn display(self: &T);
}
`
	bag := runContractSema(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", diagnosticsSummary(bag))
	}
}

func TestContractSemantics_NoGenerics(t *testing.T) {
	src := `
contract Logger{
    fn log() -> nothing;
}
`
	bag := runContractSema(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", diagnosticsSummary(bag))
	}
}

func TestContractSemantics_UnknownType(t *testing.T) {
	src := `
contract Bad<T>{
    field missing: UnknownType;
}
`
	bag := runContractSema(t, src)
	if !hasCodeContract(bag, diag.SemaUnresolvedSymbol) {
		t.Fatalf("expected unresolved symbol, got %v", diagnosticsSummary(bag))
	}
}

func TestContractSemantics_DuplicateField(t *testing.T) {
	src := `
contract C<T>{
    field x: T;
    field x: int;
}
`
	bag := runContractSema(t, src)
	if !hasCodeContract(bag, diag.SemaContractDuplicateField) {
		t.Fatalf("expected duplicate field, got %v", diagnosticsSummary(bag))
	}
}

func TestContractSemantics_DuplicateMethod(t *testing.T) {
	src := `
contract C<T>{
    fn foo(self: T) -> int;
    fn foo(self: T) -> int;
}
`
	bag := runContractSema(t, src)
	if !hasCodeContract(bag, diag.SemaContractDuplicateMethod) {
		t.Fatalf("expected duplicate method, got %v", diagnosticsSummary(bag))
	}
}

func TestContractSemantics_OverloadAllowed(t *testing.T) {
	src := `
contract C<T>{
    fn foo(self: T) -> int;
    @overload fn foo(self: &T) -> int;
}
`
	bag := runContractSema(t, src)
	if hasCodeContract(bag, diag.SemaContractDuplicateMethod) {
		t.Fatalf("expected overload to allow duplicates, got %v", diagnosticsSummary(bag))
	}
}

func TestContractSemantics_SelfTypeMismatch(t *testing.T) {
	src := `
contract C<T, U>{
    fn foo(value: T, other: U) -> T;
}
`
	bag := runContractSema(t, src)
	if bag.HasErrors() {
		t.Fatalf("expected no self type constraint errors, got %v", diagnosticsSummary(bag))
	}
}

func TestContractSemantics_UnusedGeneric(t *testing.T) {
	src := `
contract C<T, U>{
    fn foo(self: T) -> T;
}
`
	bag := runContractSema(t, src)
	if !hasCodeContract(bag, diag.SemaContractUnusedTypeParam) {
		t.Fatalf("expected unused generic error, got %v", diagnosticsSummary(bag))
	}
}

func TestContractSemantics_SelfOrder(t *testing.T) {
	src := `
contract C<T>{
    fn foo(value: T, other: T) -> T;
}
`
	bag := runContractSema(t, src)
	if bag.HasErrors() {
		t.Fatalf("expected no self order error, got %v", diagnosticsSummary(bag))
	}
}

func TestContractSemantics_UnknownAttribute(t *testing.T) {
	src := `
contract C<T>{
    @wrong field value: T;
}
`
	bag := runContractSema(t, src)
	if !hasCodeContract(bag, diag.SemaContractUnknownAttr) {
		t.Fatalf("expected unknown attribute, got %v", diagnosticsSummary(bag))
	}
}

func runContractSema(t *testing.T, src string) *diag.Bag {
	t.Helper()
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(bag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(32)
	Check(builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})
	return semaBag
}

func parseSource(t *testing.T, input string) (*ast.Builder, ast.FileID, *diag.Bag) {
	t.Helper()
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte(input))
	file := fs.Get(fileID)

	bag := diag.NewBag(128)
	reporter := &diag.BagReporter{Bag: bag}

	lx := lexer.New(file, lexer.Options{Reporter: reporter})
	builder := ast.NewBuilder(ast.Hints{}, nil)
	result := parser.ParseFile(context.Background(), fs, lx, builder, parser.Options{Reporter: reporter, MaxErrors: 128})
	if result.Bag == nil {
		result.Bag = bag
	}
	return builder, result.File, result.Bag
}

func resolveSymbols(t *testing.T, builder *ast.Builder, fileID ast.FileID) *symbols.Result {
	t.Helper()
	bag := diag.NewBag(64)
	res := symbols.ResolveFile(builder, fileID, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: bag},
	})
	if bag.HasErrors() {
		t.Fatalf("unexpected symbol resolve diagnostics: %s", diagnosticsSummary(bag))
	}
	return &res
}

func hasCodeContract(bag *diag.Bag, code diag.Code) bool {
	if bag == nil {
		return false
	}
	for _, d := range bag.Items() {
		if d.Code == code {
			return true
		}
	}
	return false
}

func diagnosticsSummary(bag *diag.Bag) string {
	if bag == nil {
		return "<nil bag>"
	}
	items := bag.Items()
	if len(items) == 0 {
		return "<none>"
	}
	lines := make([]string, len(items))
	for i, d := range items {
		lines[i] = fmt.Sprintf("[%s] %s", d.Code.ID(), d.Message)
	}
	return strings.Join(lines, "; ")
}
