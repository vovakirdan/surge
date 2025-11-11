package sema

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
)

func TestCheckInitializesTypeInterner(t *testing.T) {
	builder := ast.NewBuilder(ast.Hints{}, nil)
	file := builder.Files.New(source.Span{})
	res := Check(builder, file, Options{})
	if res.TypeInterner == nil {
		t.Fatalf("expected type interner")
	}
}

func TestBinaryLiteralTypeInference(t *testing.T) {
	builder := ast.NewBuilder(ast.Hints{}, nil)
	file := builder.Files.New(source.Span{})

	one := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, builder.StringsInterner.Intern("1"))
	two := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, builder.StringsInterner.Intern("2"))
	sum := builder.Exprs.NewBinary(source.Span{}, ast.ExprBinaryAdd, one, two)
	addTopLevelLet(builder, file, sum)

	res := Check(builder, file, Options{})
	got := res.ExprTypes[sum]
	want := res.TypeInterner.Builtins().Int
	if got != want {
		t.Fatalf("expected int type, got %v", got)
	}
}

func TestBinaryTypeMismatchEmitsDiagnostic(t *testing.T) {
	builder := ast.NewBuilder(ast.Hints{}, nil)
	file := builder.Files.New(source.Span{})

	intLit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, builder.StringsInterner.Intern("1"))
	boolLit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitTrue, builder.StringsInterner.Intern("true"))
	expr := builder.Exprs.NewBinary(source.Span{}, ast.ExprBinaryAdd, intLit, boolLit)
	addTopLevelLet(builder, file, expr)

	bag := diag.NewBag(4)
	Check(builder, file, Options{Reporter: &diag.BagReporter{Bag: bag}})
	items := bag.Items()
	if len(items) == 0 {
		t.Fatalf("expected diagnostics")
	}
	if items[0].Code != diag.SemaInvalidBinaryOperands {
		t.Fatalf("expected %v, got %v", diag.SemaInvalidBinaryOperands, items[0].Code)
	}
}

func addTopLevelLet(builder *ast.Builder, file ast.FileID, expr ast.ExprID) {
	name := builder.StringsInterner.Intern("tmp")
	itemID := builder.Items.NewLet(
		name,
		ast.NoTypeID,
		expr,
		false,
		ast.VisPrivate,
		nil,
		source.Span{},
		source.Span{},
		source.Span{},
		source.Span{},
		source.Span{},
		source.Span{},
		source.Span{},
	)
	builder.PushItem(file, itemID)
}
