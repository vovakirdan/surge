package sema

import (
	"context"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func addSimpleFn(builder *ast.Builder, file ast.FileID, name string, params []ast.FnParam, ret ast.TypeID, attrs []ast.Attr) {
	item := builder.NewFn(
		intern(builder, name),
		source.Span{},
		nil,
		nil,
		false,
		source.Span{},
		nil,
		params,
		nil,
		false,
		source.Span{},
		source.Span{},
		source.Span{},
		source.Span{},
		ret,
		ast.NoStmtID,
		0,
		attrs,
		source.Span{},
	)
	builder.PushItem(file, item)
}

func TestCallResolverPrefersExactOverCoercion(t *testing.T) {
	builder, file := newTestBuilder()
	intType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: intern(builder, "int")}})
	int32Type := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: intern(builder, "int32")}})

	paramName := intern(builder, "x")
	addSimpleFn(builder, file, "foo", []ast.FnParam{{Name: paramName, Type: intType}}, intType, nil)
	addSimpleFn(builder, file, "foo", []ast.FnParam{{Name: paramName, Type: int32Type}}, int32Type, []ast.Attr{{Name: intern(builder, "overload")}})

	lit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "1"))
	call := builder.Exprs.NewCall(source.Span{}, builder.Exprs.NewIdent(source.Span{}, intern(builder, "foo")), []ast.ExprID{lit}, nil, nil, false)
	addTopLevelLet(builder, file, call)

	resolveBag := diag.NewBag(8)
	symRes := symbols.ResolveFile(builder, file, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: resolveBag},
	})
	if len(resolveBag.Items()) > 0 {
		t.Fatalf("unexpected resolve diagnostics: %v", resolveBag.Items())
	}
	semaBag := diag.NewBag(8)
	res := Check(context.Background(), builder, file, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  &symRes,
	})
	if len(semaBag.Items()) > 0 {
		t.Fatalf("unexpected sema diagnostics: %v", semaBag.Items())
	}
	got := res.ExprTypes[call]
	if got != res.TypeInterner.Builtins().Int {
		t.Fatalf("expected call type int, got %v", got)
	}
}

func TestCallResolverReportsAmbiguousOverload(t *testing.T) {
	builder, file := newTestBuilder()
	int32Type := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: intern(builder, "int32")}})
	uint32Type := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: intern(builder, "uint32")}})

	paramName := intern(builder, "x")
	addSimpleFn(builder, file, "bar", []ast.FnParam{{Name: paramName, Type: int32Type}}, int32Type, nil)
	addSimpleFn(builder, file, "bar", []ast.FnParam{{Name: paramName, Type: uint32Type}}, uint32Type, []ast.Attr{{Name: intern(builder, "overload")}})

	lit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "1"))
	call := builder.Exprs.NewCall(source.Span{}, builder.Exprs.NewIdent(source.Span{}, intern(builder, "bar")), []ast.ExprID{lit}, nil, nil, false)
	addTopLevelLet(builder, file, call)

	semaBag := runSema(t, builder, file)
	if !hasCode(semaBag, diag.SemaAmbiguousOverload) {
		t.Fatalf("expected ambiguous overload diagnostic, got %v", diagCodes(semaBag))
	}
}

func TestCallResolverReportsNoOverload(t *testing.T) {
	builder, file := newTestBuilder()
	intType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: intern(builder, "int")}})
	paramName := intern(builder, "x")
	addSimpleFn(builder, file, "baz", []ast.FnParam{{Name: paramName, Type: intType}}, intType, nil)

	strLit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitString, intern(builder, "\"hi\""))
	call := builder.Exprs.NewCall(source.Span{}, builder.Exprs.NewIdent(source.Span{}, intern(builder, "baz")), []ast.ExprID{strLit}, nil, nil, false)
	addTopLevelLet(builder, file, call)

	semaBag := runSema(t, builder, file)
	if !hasCode(semaBag, diag.SemaNoOverload) {
		t.Fatalf("expected no overload diagnostic, got %v", diagCodes(semaBag))
	}
}

func TestCallResolverInfersGenericReturn(t *testing.T) {
	builder, file := newTestBuilder()
	tName := intern(builder, "T")
	param := ast.FnParam{Name: intern(builder, "x"), Type: builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: tName}})}
	builder.PushItem(file, builder.NewFn(
		intern(builder, "id"),
		source.Span{},
		[]source.StringID{tName},
		nil,
		false,
		source.Span{},
		[]ast.TypeParamSpec{{Name: tName}},
		[]ast.FnParam{param},
		nil,
		false,
		source.Span{},
		source.Span{},
		source.Span{},
		source.Span{},
		param.Type,
		ast.NoStmtID,
		0,
		nil,
		source.Span{},
	))

	lit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "1"))
	call := builder.Exprs.NewCall(source.Span{}, builder.Exprs.NewIdent(source.Span{}, intern(builder, "id")), []ast.ExprID{lit}, nil, nil, false)
	addTopLevelLet(builder, file, call)

	resolveBag := diag.NewBag(8)
	symRes := symbols.ResolveFile(builder, file, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: resolveBag},
	})
	if len(resolveBag.Items()) > 0 {
		t.Fatalf("unexpected resolve diagnostics: %v", resolveBag.Items())
	}
	semaBag := diag.NewBag(8)
	res := Check(context.Background(), builder, file, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  &symRes,
	})
	if len(semaBag.Items()) > 0 {
		t.Fatalf("unexpected diagnostics: %v", semaBag.Items())
	}
	if res.ExprTypes[call] != res.TypeInterner.Builtins().Int {
		t.Fatalf("expected call type int, got %v", res.ExprTypes[call])
	}
}

func TestFunctionInstantiationsRecorded(t *testing.T) {
	builder, file := newTestBuilder()
	tName := intern(builder, "T")
	param := ast.FnParam{Name: intern(builder, "x"), Type: builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: tName}})}
	genericFn := builder.NewFn(
		intern(builder, "id"),
		source.Span{},
		[]source.StringID{tName},
		nil,
		false,
		source.Span{},
		[]ast.TypeParamSpec{{Name: tName}},
		[]ast.FnParam{param},
		nil,
		false,
		source.Span{},
		source.Span{},
		source.Span{},
		source.Span{},
		param.Type,
		ast.NoStmtID,
		0,
		nil,
		source.Span{},
	)
	builder.PushItem(file, genericFn)

	intLit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "1"))
	callInt := builder.Exprs.NewCall(source.Span{}, builder.Exprs.NewIdent(source.Span{}, intern(builder, "id")), []ast.ExprID{intLit}, nil, nil, false)
	builder.PushItem(file, builder.Items.NewLet(intern(builder, "a"), ast.NoTypeID, callInt, false, ast.VisPrivate, nil, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}))

	strLit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitString, intern(builder, "\"s\""))
	callStr := builder.Exprs.NewCall(source.Span{}, builder.Exprs.NewIdent(source.Span{}, intern(builder, "id")), []ast.ExprID{strLit}, nil, nil, false)
	builder.PushItem(file, builder.Items.NewLet(intern(builder, "b"), ast.NoTypeID, callStr, false, ast.VisPrivate, nil, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}))

	otherInt := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "2"))
	dupCall := builder.Exprs.NewCall(source.Span{}, builder.Exprs.NewIdent(source.Span{}, intern(builder, "id")), []ast.ExprID{otherInt}, nil, nil, false)
	builder.PushItem(file, builder.Items.NewLet(intern(builder, "c"), ast.NoTypeID, dupCall, false, ast.VisPrivate, nil, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}))

	resolveBag := diag.NewBag(8)
	symRes := symbols.ResolveFile(builder, file, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: resolveBag},
	})
	if len(resolveBag.Items()) > 0 {
		t.Fatalf("unexpected resolve diagnostics: %v", resolveBag.Items())
	}
	semaBag := diag.NewBag(8)
	res := Check(context.Background(), builder, file, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  &symRes,
	})
	if len(semaBag.Items()) > 0 {
		t.Fatalf("unexpected sema diagnostics: %v", semaBag.Items())
	}

	idSym := lookupSymbolByName(&symRes, intern(builder, "id"))
	if idSym == symbols.NoSymbolID {
		t.Fatalf("id symbol not found")
	}
	insts := res.FunctionInstantiations[idSym]
	if len(insts) != 2 {
		t.Fatalf("expected 2 instantiations, got %d (%v)", len(insts), insts)
	}
	seen := make(map[types.TypeID]struct{})
	for _, args := range insts {
		if len(args) != 1 {
			t.Fatalf("expected single type arg, got %v", args)
		}
		seen[args[0]] = struct{}{}
	}
	if len(seen) != 2 {
		t.Fatalf("expected distinct type args recorded, got %v", insts)
	}
}
