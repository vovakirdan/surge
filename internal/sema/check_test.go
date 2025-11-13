package sema

import (
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
