package sema

import (
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
)

func TestCastAcceptsValidToSignature(t *testing.T) {
	builder, fileID := newTestBuilder()
	intName := intern(builder, "int")
	myIntName := intern(builder, "MyInt")
	toName := intern(builder, "__to")

	intType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: intName}})
	myIntType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: myIntName}})

	aliasID := builder.NewTypeAlias(myIntName, nil, nil, false, source.Span{}, source.Span{}, source.Span{}, source.Span{}, nil, ast.VisPrivate, intType, source.Span{})
	builder.PushItem(fileID, aliasID)

	addExternTo(builder, fileID, myIntType, intType, intType, toName, nil)

	xName := intern(builder, "x")
	yName := intern(builder, "y")

	stmtX := builder.Stmts.NewLet(source.Span{}, xName, myIntType, builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "1")), false)
	castExpr := builder.Exprs.NewCast(source.Span{}, builder.Exprs.NewIdent(source.Span{}, xName), intType, ast.NoExprID)
	stmtY := builder.Stmts.NewLet(source.Span{}, yName, intType, castExpr, false)
	stmtReturn := builder.Stmts.NewReturn(source.Span{}, builder.Exprs.NewIdent(source.Span{}, yName))

	addFunctionWithReturn(builder, fileID, "main", []ast.StmtID{stmtX, stmtY, stmtReturn}, intType)

	diags := runSema(t, builder, fileID)
	if got := len(diags.Items()); got != 0 {
		t.Fatalf("expected no diagnostics, got %d: %+v", got, diagCodes(diags))
	}
}

func TestCastRejectsInvalidToSignature(t *testing.T) {
	builder, fileID := newTestBuilder()
	intName := intern(builder, "int")
	myIntName := intern(builder, "MyInt")
	toName := intern(builder, "__to")

	intType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: intName}})
	myIntType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: myIntName}})

	aliasID := builder.NewTypeAlias(myIntName, nil, nil, false, source.Span{}, source.Span{}, source.Span{}, source.Span{}, nil, ast.VisPrivate, intType, source.Span{})
	builder.PushItem(fileID, aliasID)

	uintName := intern(builder, "uint")
	uintType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: uintName}})

	addExternTo(builder, fileID, myIntType, intType, uintType, toName, []ast.FnParam{
		{Name: intern(builder, "extra"), Type: intType},
	})

	diags := runSema(t, builder, fileID)
	items := diags.Items()
	if len(items) != 1 {
		t.Fatalf("expected one diagnostic, got %d: %+v", len(items), diagCodes(diags))
	}
	if items[0].Code != diag.SemaTypeMismatch {
		t.Fatalf("expected %v, got %v", diag.SemaTypeMismatch, items[0].Code)
	}
	if items[0].Severity != diag.SevError {
		t.Fatalf("expected severity error, got %v", items[0].Severity)
	}
	if msg := items[0].Message; msg == "" || !strings.Contains(msg, "__to") {
		t.Fatalf("expected message to mention __to, got %q", msg)
	}
}

func addExternTo(builder *ast.Builder, fileID ast.FileID, receiverType, targetType, returnType ast.TypeID, name source.StringID, extraParams []ast.FnParam) {
	params := []ast.FnParam{
		{Name: intern(builder, "self"), Type: receiverType},
		{Name: intern(builder, "target"), Type: targetType},
	}
	if len(extraParams) > 0 {
		params = append(params, extraParams...)
	}
	fnPayload := builder.NewExternFn(
		name,
		source.Span{},
		nil,
		nil,
		false,
		source.Span{},
		params,
		nil,
		false,
		source.Span{},
		source.Span{},
		source.Span{},
		source.Span{},
		returnType,
		ast.NoStmtID,
		0,
		nil,
		source.Span{},
	)
	externID := builder.NewExtern(
		receiverType,
		nil,
		[]ast.ExternMemberSpec{
			{Kind: ast.ExternMemberFn, Fn: fnPayload},
		},
		source.Span{},
	)
	builder.PushItem(fileID, externID)
}
