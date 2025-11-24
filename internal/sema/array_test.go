package sema

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestArrayLiteralUsesNominalArray(t *testing.T) {
	builder := ast.NewBuilder(ast.Hints{}, nil)
	file := builder.Files.New(source.Span{})

	one := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, builder.StringsInterner.Intern("1"))
	two := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, builder.StringsInterner.Intern("2"))
	arr := builder.Exprs.NewArray(source.Span{}, []ast.ExprID{one, two}, nil, false)

	letName := builder.StringsInterner.Intern("xs")
	letID := builder.Items.NewLet(letName, ast.NoTypeID, arr, false, ast.VisPrivate, nil, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{})
	builder.PushItem(file, letID)

	symRes := symbols.ResolveFile(builder, file, &symbols.ResolveOptions{})
	res := Check(builder, file, Options{Symbols: &symRes})

	arrayType := res.ExprTypes[arr]
	if arrayType == types.NoTypeID {
		t.Fatalf("expected array literal to have a type")
	}
	desc := res.TypeInterner.MustLookup(arrayType)
	if desc.Kind != types.KindStruct {
		t.Fatalf("expected Array to be a nominal struct, got %v", desc.Kind)
	}
	info, ok := res.TypeInterner.StructInfo(arrayType)
	if !ok || info == nil {
		t.Fatalf("expected struct info for array type")
	}
	if name := builder.StringsInterner.MustLookup(info.Name); name != "Array" {
		t.Fatalf("expected struct named Array, got %s", name)
	}
	if len(info.TypeArgs) != 1 || info.TypeArgs[0] != res.TypeInterner.Builtins().Int {
		t.Fatalf("expected Array<int>, got args %v", info.TypeArgs)
	}
}
