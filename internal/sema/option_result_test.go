package sema

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
)

func TestReturnAutoWrapsOptionAndNothing(t *testing.T) {
	builder, fileID := newTestBuilder()
	addOptionResultPrelude(builder, fileID)

	intType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: intern(builder, "int")}})
	optInt := builder.Types.NewOptional(source.Span{}, intType)

	retInt := builder.Stmts.NewReturn(source.Span{}, builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "1")))
	addFunctionWithReturn(builder, fileID, "opt_int", []ast.StmtID{retInt}, optInt)

	retNothing := builder.Stmts.NewReturn(source.Span{}, builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitNothing, source.NoStringID))
	addFunctionWithReturn(builder, fileID, "opt_nothing", []ast.StmtID{retNothing}, optInt)

	retBad := builder.Stmts.NewReturn(source.Span{}, builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitString, intern(builder, "oops")))
	addFunctionWithReturn(builder, fileID, "opt_wrong", []ast.StmtID{retBad}, optInt)

	diags := runSema(t, builder, fileID)
	for _, d := range diags.Items() {
		t.Logf("%s: %s", d.Code, d.Message)
	}
	if codes := diagCodes(diags); len(codes) != 1 || codes[0] != diag.SemaTypeMismatch {
		t.Fatalf("expected a single type mismatch, got %v", codes)
	}
}

func TestReturnAutoWrapsResultOk(t *testing.T) {
	builder, fileID := newTestBuilder()
	addOptionResultPrelude(builder, fileID)

	intType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: intern(builder, "int")}})
	resultInt := builder.Types.NewErrorable(source.Span{}, intType, ast.NoTypeID)

	retInt := builder.Stmts.NewReturn(source.Span{}, builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "2")))
	addFunctionWithReturn(builder, fileID, "res_ok", []ast.StmtID{retInt}, resultInt)

	retBad := builder.Stmts.NewReturn(source.Span{}, builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitString, intern(builder, "fail")))
	addFunctionWithReturn(builder, fileID, "res_bad", []ast.StmtID{retBad}, resultInt)

	diags := runSema(t, builder, fileID)
	for _, d := range diags.Items() {
		t.Logf("%s: %s", d.Code, d.Message)
	}
	if codes := diagCodes(diags); len(codes) != 1 || codes[0] != diag.SemaTypeMismatch {
		t.Fatalf("expected a single type mismatch for bad result, got %v", codes)
	}
}

func addOptionResultPrelude(builder *ast.Builder, fileID ast.FileID) {
	optionName := intern(builder, "Option")
	erringName := intern(builder, "Erring")
	errorName := intern(builder, "Error")
	tName := intern(builder, "T")
	eName := intern(builder, "E")
	someName := intern(builder, "Some")
	successName := intern(builder, "Success")

	tPath := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: tName}})
	ePath := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: eName}})

	someTag := builder.NewTag(someName, source.Span{}, []source.StringID{tName}, nil, false, source.Span{}, nil, source.Span{}, source.Span{}, source.Span{}, []ast.TypeID{tPath}, nil, false, nil, ast.VisPrivate, source.Span{})
	builder.PushItem(fileID, someTag)
	optionMembers := []ast.TypeUnionMemberSpec{
		{Kind: ast.TypeUnionMemberTag, TagName: someName, TagArgs: []ast.TypeID{tPath}},
		{Kind: ast.TypeUnionMemberNothing},
	}
	optionID := builder.NewTypeUnion(optionName, []source.StringID{tName}, nil, false, source.Span{}, nil, source.Span{}, source.Span{}, source.Span{}, nil, ast.VisPrivate, optionMembers, source.Span{}, source.Span{})
	builder.PushItem(fileID, optionID)

	successTag := builder.NewTag(successName, source.Span{}, []source.StringID{tName}, nil, false, source.Span{}, nil, source.Span{}, source.Span{}, source.Span{}, []ast.TypeID{tPath}, nil, false, nil, ast.VisPrivate, source.Span{})
	builder.PushItem(fileID, successTag)
	// Erring<T, E> = Success(T) | E (error types pass through directly, no tag wrapper)
	erringMembers := []ast.TypeUnionMemberSpec{
		{Kind: ast.TypeUnionMemberTag, TagName: successName, TagArgs: []ast.TypeID{tPath}},
		{Kind: ast.TypeUnionMemberType, Type: ePath},
	}
	erringID := builder.NewTypeUnion(erringName, []source.StringID{tName, eName}, nil, false, source.Span{}, nil, source.Span{}, source.Span{}, source.Span{}, nil, ast.VisPrivate, erringMembers, source.Span{}, source.Span{})
	builder.PushItem(fileID, erringID)

	stringType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: intern(builder, "string")}})
	uintType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: intern(builder, "uint")}})
	errorFields := []ast.TypeStructFieldSpec{
		{Name: intern(builder, "message"), Type: stringType},
		{Name: intern(builder, "code"), Type: uintType},
	}
	errorID := builder.NewTypeStruct(errorName, nil, nil, false, source.Span{}, nil, source.Span{}, source.Span{}, source.Span{}, nil, ast.VisPrivate, ast.NoTypeID, errorFields, nil, false, source.Span{}, source.Span{})
	builder.PushItem(fileID, errorID)
}
