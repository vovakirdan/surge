package sema

import (
	"context"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestMapLiteralTypeInference(t *testing.T) {
	builder := ast.NewBuilder(ast.Hints{}, nil)
	file := builder.Files.New(source.Span{})

	keyA := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitString, builder.StringsInterner.Intern("a"))
	valA := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, builder.StringsInterner.Intern("1"))
	keyB := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitString, builder.StringsInterner.Intern("b"))
	valB := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, builder.StringsInterner.Intern("2"))

	entries := []ast.ExprMapEntry{
		{Key: keyA, Value: valA},
		{Key: keyB, Value: valB},
	}
	mapExpr := builder.Exprs.NewMap(source.Span{}, entries, nil, false)

	letName := builder.StringsInterner.Intern("m")
	letID := builder.Items.NewLet(letName, ast.NoTypeID, mapExpr, false, ast.VisPrivate, nil, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{})
	builder.PushItem(file, letID)

	symRes := symbols.ResolveFile(builder, file, &symbols.ResolveOptions{})
	res := Check(context.Background(), builder, file, Options{Symbols: &symRes})

	mapType := res.ExprTypes[mapExpr]
	if mapType == types.NoTypeID {
		t.Fatalf("expected map literal to have a type")
	}
	keyType, valueType, ok := res.TypeInterner.MapInfo(mapType)
	if !ok {
		t.Fatalf("expected Map<K, V> type, got %v", mapType)
	}
	if keyType != res.TypeInterner.Builtins().String {
		t.Fatalf("expected key type string, got type #%d", keyType)
	}
	if valueType != res.TypeInterner.Builtins().Int {
		t.Fatalf("expected value type int, got type #%d", valueType)
	}
}

func TestMapKeyRestriction(t *testing.T) {
	src := `
fn main() {
    let _ = { [1] => 2 };
}
`
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(bag))
	}
	symRes := resolveSymbols(t, builder, fileID)

	semaBag := diag.NewBag(16)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})

	if !hasCode(semaBag, diag.SemaTypeMismatch) {
		t.Fatalf("expected %v diagnostic, got codes %v", diag.SemaTypeMismatch, diagCodes(semaBag))
	}
	found := false
	for _, item := range semaBag.Items() {
		if item.Code == diag.SemaTypeMismatch && strings.Contains(item.Message, "map key type must be hashable") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected map key restriction message, got: %s", diagnosticsSummary(semaBag))
	}
}
