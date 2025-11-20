package sema

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/source"
)

func TestImplicitWidenFixedToDynamic(t *testing.T) {
	builder, fileID := newTestBuilder()

	intName := intern(builder, "int")
	int8Name := intern(builder, "int8")

	intType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: intName}})
	int8Type := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: int8Name}})

	one := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "1"))
	smallName := intern(builder, "small")
	small := builder.Items.NewLet(
		smallName,
		int8Type,
		one,
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
	builder.PushItem(fileID, small)

	identSmall := builder.Exprs.NewIdent(source.Span{}, smallName)
	wide := builder.Items.NewLet(
		intern(builder, "wide"),
		intType,
		identSmall,
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
	builder.PushItem(fileID, wide)

	diags := runSema(t, builder, fileID)
	if got := len(diags.Items()); got != 0 {
		t.Fatalf("expected no diagnostics, got %d: %v", got, diagCodes(diags))
	}
}

func TestNumericCastAllowsCheckedNarrowing(t *testing.T) {
	builder, fileID := newTestBuilder()

	int8Name := intern(builder, "int8")
	int8Type := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: int8Name}})

	value := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "300"))
	castExpr := builder.Exprs.NewCast(source.Span{}, value, int8Type, ast.NoExprID)

	item := builder.Items.NewLet(
		intern(builder, "narrow"),
		int8Type,
		castExpr,
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
	builder.PushItem(fileID, item)

	diags := runSema(t, builder, fileID)
	if got := len(diags.Items()); got != 0 {
		t.Fatalf("expected no diagnostics, got %d: %v", got, diagCodes(diags))
	}
}
