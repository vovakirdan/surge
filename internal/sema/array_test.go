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
	info, ok := res.TypeInterner.StructInfo(arrayType)
	if !ok || info == nil {
		t.Fatalf("expected struct info for array type")
	}
	name := builder.StringsInterner.MustLookup(info.Name)
	if name != "ArrayFixed" && name != "Array" {
		t.Fatalf("expected struct named Array or ArrayFixed, got %s", name)
	}
	if len(info.TypeArgs) == 0 || info.TypeArgs[0] != res.TypeInterner.Builtins().Int {
		t.Fatalf("expected element int, got args %v", info.TypeArgs)
	}
	if name == "ArrayFixed" {
		if vals := res.TypeInterner.StructValueArgs(arrayType); len(vals) != 1 || vals[0] != 2 {
			t.Fatalf("expected fixed length 2, got %v", vals)
		}
	}
}
