package sema

import (
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
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

func TestTypeCheckerStructFieldAccess(t *testing.T) {
	builder := ast.NewBuilder(ast.Hints{}, nil)
	file := builder.Files.New(source.Span{})

	intName := builder.StringsInterner.Intern("int")
	stringName := builder.StringsInterner.Intern("string")
	personName := builder.StringsInterner.Intern("Person")
	ageField := builder.StringsInterner.Intern("age")
	nameField := builder.StringsInterner.Intern("name")

	intType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: intName}})
	stringType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: stringName}})
	fields := []ast.TypeStructFieldSpec{
		{Name: ageField, Type: intType},
		{Name: nameField, Type: stringType},
	}
	typeItemID := builder.NewTypeStruct(personName, nil, nil, false, source.Span{}, source.Span{}, source.Span{}, source.Span{}, nil, ast.VisPrivate, ast.NoTypeID, fields, nil, false, source.Span{}, source.Span{})
	builder.PushItem(file, typeItemID)

	intLiteral := builder.StringsInterner.Intern("25")
	strLiteral := builder.StringsInterner.Intern("John")
	ageValue := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intLiteral)
	nameValue := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitString, strLiteral)
	personTypeExpr := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: personName}})
	structExpr := builder.Exprs.NewStruct(source.Span{}, personTypeExpr, []ast.ExprStructField{
		{Name: ageField, Value: ageValue},
		{Name: nameField, Value: nameValue},
	}, nil, false, false)

	pName := builder.StringsInterner.Intern("p")
	letPersonID := builder.Items.NewLet(pName, ast.NoTypeID, structExpr, false, ast.VisPrivate, nil, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{})
	builder.PushItem(file, letPersonID)

	pIdent := builder.Exprs.NewIdent(source.Span{}, pName)
	memberExpr := builder.Exprs.NewMember(source.Span{}, pIdent, ageField)
	ageBinding := builder.StringsInterner.Intern("age_var")
	letAgeID := builder.Items.NewLet(ageBinding, ast.NoTypeID, memberExpr, false, ast.VisPrivate, nil, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{}, source.Span{})
	builder.PushItem(file, letAgeID)

	table := symbols.NewTable(symbols.Hints{}, builder.StringsInterner)
	fileScope := table.FileRoot(builder.Files.Get(file).Span.File, builder.Files.Get(file).Span)

	registerSymbol := func(sym *symbols.Symbol) symbols.SymbolID {
		sym.Scope = fileScope
		id := table.Symbols.New(sym)
		scope := table.Scopes.Get(fileScope)
		scope.Symbols = append(scope.Symbols, id)
		scope.NameIndex[sym.Name] = append(scope.NameIndex[sym.Name], id)
		return id
	}

	typeSymID := registerSymbol(&symbols.Symbol{
		Name: personName,
		Kind: symbols.SymbolType,
		Span: builder.Files.Get(file).Span,
		Decl: symbols.SymbolDecl{
			ASTFile: file,
			Item:    typeItemID,
		},
	})
	pSymID := registerSymbol(&symbols.Symbol{
		Name: pName,
		Kind: symbols.SymbolLet,
		Span: source.Span{},
		Decl: symbols.SymbolDecl{
			ASTFile: file,
			Item:    letPersonID,
		},
	})
	ageSymID := registerSymbol(&symbols.Symbol{
		Name: ageBinding,
		Kind: symbols.SymbolLet,
		Span: source.Span{},
		Decl: symbols.SymbolDecl{
			ASTFile: file,
			Item:    letAgeID,
		},
	})

	symRes := symbols.Result{
		Table:     table,
		File:      file,
		FileScope: fileScope,
		ItemSymbols: map[ast.ItemID][]symbols.SymbolID{
			typeItemID:  {typeSymID},
			letPersonID: {pSymID},
			letAgeID:    {ageSymID},
		},
		ExprSymbols: map[ast.ExprID]symbols.SymbolID{
			pIdent: pSymID,
		},
	}

	res := Check(builder, file, Options{Symbols: &symRes})
	memberType := res.ExprTypes[memberExpr]
	if memberType == types.NoTypeID {
		t.Fatalf("expected member expression to have a type")
	}
	if label := res.TypeInterner.MustLookup(memberType).Kind; label != types.KindInt {
		t.Fatalf("expected member type KindInt, got %v", label)
	}
}
func TestAliasBinaryRequiresMatchingTypes(t *testing.T) {
	builder, fileID := newTestBuilder()
	gasName := intern(builder, "Gasoline")
	stringName := intern(builder, "string")

	stringType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: stringName}})
	aliasItem := builder.NewTypeAlias(gasName, nil, nil, false, source.Span{}, source.Span{}, source.Span{}, source.Span{}, nil, ast.VisPrivate, stringType, source.Span{})
	builder.PushItem(fileID, aliasItem)

	gasType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: gasName}})

	aLit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitString, intern(builder, "A"))
	stmtA := builder.Stmts.NewLet(source.Span{}, intern(builder, "a"), gasType, aLit, false)

	bLit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitString, intern(builder, "B"))
	stmtB := builder.Stmts.NewLet(source.Span{}, intern(builder, "b"), gasType, bLit, false)

	aIdent := builder.Exprs.NewIdent(source.Span{}, intern(builder, "a"))
	bIdent := builder.Exprs.NewIdent(source.Span{}, intern(builder, "b"))
	sumExpr := builder.Exprs.NewBinary(source.Span{}, ast.ExprBinaryAdd, aIdent, bIdent)
	stmtGood := builder.Stmts.NewLet(source.Span{}, intern(builder, "fuel"), gasType, sumExpr, false)

	rawLit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitString, intern(builder, "raw"))
	badExpr := builder.Exprs.NewBinary(source.Span{}, ast.ExprBinaryAdd, builder.Exprs.NewIdent(source.Span{}, intern(builder, "a")), rawLit)
	stmtBad := builder.Stmts.NewLet(source.Span{}, intern(builder, "bad"), gasType, badExpr, false)

	addFunction(builder, fileID, "main", []ast.StmtID{stmtA, stmtB, stmtGood, stmtBad})

	diags := runSema(t, builder, fileID)
	items := diags.Items()
	if len(items) != 1 {
		t.Fatalf("expected one diagnostic, got %v", items)
	}
	if items[0].Code != diag.SemaInvalidBinaryOperands {
		t.Fatalf("expected %v, got %v", diag.SemaInvalidBinaryOperands, items[0].Code)
	}
	if !strings.Contains(items[0].Message, "__add") {
		t.Fatalf("expected message to reference __add, got %q", items[0].Message)
	}
}

func TestAliasBinaryWithForeignType(t *testing.T) {
	builder, fileID := newTestBuilder()
	gasName := intern(builder, "Gasoline")
	stringName := intern(builder, "string")

	stringType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: stringName}})
	aliasItem := builder.NewTypeAlias(gasName, nil, nil, false, source.Span{}, source.Span{}, source.Span{}, source.Span{}, nil, ast.VisPrivate, stringType, source.Span{})
	builder.PushItem(fileID, aliasItem)
	gasType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: gasName}})

	aLit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitString, intern(builder, "Fuel"))
	stmtA := builder.Stmts.NewLet(source.Span{}, intern(builder, "a"), gasType, aLit, false)

	aIdent := builder.Exprs.NewIdent(source.Span{}, intern(builder, "a"))
	count := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "2"))
	mulExpr := builder.Exprs.NewBinary(source.Span{}, ast.ExprBinaryMul, aIdent, count)
	stmtMul := builder.Stmts.NewLet(source.Span{}, intern(builder, "double"), ast.NoTypeID, mulExpr, false)

	addFunction(builder, fileID, "main", []ast.StmtID{stmtA, stmtMul})

	diags := runSema(t, builder, fileID)
	if len(diags.Items()) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags.Items())
	}
}

func TestStringMulIntrinsicAvailable(t *testing.T) {
	builder := ast.NewBuilder(ast.Hints{}, nil)
	file := builder.Files.New(source.Span{})

	strID := builder.StringsInterner.Intern("\"s\"")
	strLit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitString, strID)
	intID := builder.StringsInterner.Intern("2")
	intLit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intID)
	mul := builder.Exprs.NewBinary(source.Span{}, ast.ExprBinaryMul, strLit, intLit)
	addTopLevelLet(builder, file, mul)

	bag := diag.NewBag(2)
	res := Check(builder, file, Options{Reporter: &diag.BagReporter{Bag: bag}})
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics for string * int: %v", bag.Items())
	}
	got := res.ExprTypes[mul]
	want := res.TypeInterner.Builtins().String
	if got != want {
		t.Fatalf("expected string type, got %v", got)
	}
}

func TestCastIntToStringUsesMagic(t *testing.T) {
	builder := ast.NewBuilder(ast.Hints{}, nil)
	file := builder.Files.New(source.Span{})

	intLit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, builder.StringsInterner.Intern("42"))
	stringPath := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: builder.StringsInterner.Intern("string")}})
	castExpr := builder.Exprs.NewCast(source.Span{}, intLit, stringPath, ast.NoExprID)
	addTopLevelLet(builder, file, castExpr)

	res, _, bag := checkWithSymbols(t, builder, file)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", bag.Items())
	}
	if got := res.ExprTypes[castExpr]; got != res.TypeInterner.Builtins().String {
		t.Fatalf("expected string type, got %v", got)
	}
}

func TestCastPreservesAliasTarget(t *testing.T) {
	builder, fileID := newTestBuilder()
	gasName := intern(builder, "Gasoline")
	stringName := intern(builder, "string")

	stringType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: stringName}})
	aliasItem := builder.NewTypeAlias(gasName, nil, nil, false, source.Span{}, source.Span{}, source.Span{}, source.Span{}, nil, ast.VisPrivate, stringType, source.Span{})
	builder.PushItem(fileID, aliasItem)

	value := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "1"))
	gasType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: gasName}})
	castExpr := builder.Exprs.NewCast(source.Span{}, value, gasType, ast.NoExprID)
	addTopLevelLet(builder, fileID, castExpr)

	res, symRes, bag := checkWithSymbols(t, builder, fileID)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", bag.Items())
	}
	symID := symRes.ItemSymbols[aliasItem][0]
	aliasType := symRes.Table.Symbols.Get(symID).Type
	if got := res.ExprTypes[castExpr]; got != aliasType {
		t.Fatalf("expected alias type %v, got %v", aliasType, got)
	}
}

func TestCastReportsMissingMethod(t *testing.T) {
	builder := ast.NewBuilder(ast.Hints{}, nil)
	file := builder.Files.New(source.Span{})

	boolLit := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitTrue, builder.StringsInterner.Intern("true"))
	floatType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: builder.StringsInterner.Intern("float")}})
	castExpr := builder.Exprs.NewCast(source.Span{}, boolLit, floatType, ast.NoExprID)
	addTopLevelLet(builder, file, castExpr)

	_, _, bag := checkWithSymbols(t, builder, file)
	items := bag.Items()
	if len(items) == 0 {
		t.Fatalf("expected diagnostics")
	}
	if items[0].Code != diag.SemaTypeMismatch {
		t.Fatalf("expected %v, got %v", diag.SemaTypeMismatch, items[0].Code)
	}
	if !strings.Contains(items[0].Message, "__to") {
		t.Fatalf("expected message to reference __to, got %q", items[0].Message)
	}
}

func checkWithSymbols(t *testing.T, builder *ast.Builder, file ast.FileID) (*Result, *symbols.Result, *diag.Bag) {
	t.Helper()
	bag := diag.NewBag(16)
	symRes := symbols.ResolveFile(builder, file, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: bag},
	})
	res := Check(builder, file, Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  &symRes,
	})
	return &res, &symRes, bag
}

func TestBinaryIsRequiresTypeOperand(t *testing.T) {
	builder := ast.NewBuilder(ast.Hints{}, nil)
	file := builder.Files.New(source.Span{})

	left := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, builder.StringsInterner.Intern("1"))
	right := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, builder.StringsInterner.Intern("2"))
	isExpr := builder.Exprs.NewBinary(source.Span{}, ast.ExprBinaryIs, left, right)
	addTopLevelLet(builder, file, isExpr)

	_, _, bag := checkWithSymbols(t, builder, file)
	items := bag.Items()
	if len(items) == 0 || items[0].Code != diag.SemaExpectTypeOperand {
		t.Fatalf("expected %v diagnostic, got %+v", diag.SemaExpectTypeOperand, items)
	}
}

func TestBinaryHeirRequiresTypeOperand(t *testing.T) {
	builder := ast.NewBuilder(ast.Hints{}, nil)
	file := builder.Files.New(source.Span{})

	left := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, builder.StringsInterner.Intern("1"))
	right := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitString, builder.StringsInterner.Intern("\"s\""))
	heirExpr := builder.Exprs.NewBinary(source.Span{}, ast.ExprBinaryHeir, left, right)
	addTopLevelLet(builder, file, heirExpr)

	_, _, bag := checkWithSymbols(t, builder, file)
	items := bag.Items()
	if len(items) == 0 || items[0].Code != diag.SemaExpectTypeOperand {
		t.Fatalf("expected %v diagnostic, got %+v", diag.SemaExpectTypeOperand, items)
	}
}
