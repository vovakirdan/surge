package sema

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

func TestBorrowRejectsDoubleMutable(t *testing.T) {
	builder, fileID := newTestBuilder()
	xName := intern(builder, "x")
	aName := intern(builder, "a")
	bName := intern(builder, "b")

	initX := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "0"))
	stmtX := builder.Stmts.NewLet(source.Span{}, xName, ast.NoTypeID, initX, true)

	firstBorrow := builder.Exprs.NewUnary(source.Span{}, ast.ExprUnaryRefMut, builder.Exprs.NewIdent(source.Span{}, xName))
	stmtA := builder.Stmts.NewLet(source.Span{}, aName, ast.NoTypeID, firstBorrow, false)

	secondBorrow := builder.Exprs.NewUnary(source.Span{}, ast.ExprUnaryRefMut, builder.Exprs.NewIdent(source.Span{}, xName))
	stmtB := builder.Stmts.NewLet(source.Span{}, bName, ast.NoTypeID, secondBorrow, false)

	addFunction(builder, fileID, "main", []ast.StmtID{stmtX, stmtA, stmtB})

	diags := runSema(t, builder, fileID)
	if !hasCode(diags, diag.SemaBorrowConflict) {
		t.Fatalf("expected %v diagnostic, got codes %v", diag.SemaBorrowConflict, diagCodes(diags))
	}
}

func TestBorrowBlocksMutationWhileShared(t *testing.T) {
	builder, fileID := newTestBuilder()
	xName := intern(builder, "x")
	rName := intern(builder, "r")

	initX := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "0"))
	stmtX := builder.Stmts.NewLet(source.Span{}, xName, ast.NoTypeID, initX, true)

	share := builder.Exprs.NewUnary(source.Span{}, ast.ExprUnaryRef, builder.Exprs.NewIdent(source.Span{}, xName))
	stmtR := builder.Stmts.NewLet(source.Span{}, rName, ast.NoTypeID, share, false)

	assign := builder.Exprs.NewBinary(source.Span{}, ast.ExprBinaryAssign, builder.Exprs.NewIdent(source.Span{}, xName), builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "1")))
	stmtAssign := builder.Stmts.NewExpr(source.Span{}, assign)

	addFunction(builder, fileID, "main", []ast.StmtID{stmtX, stmtR, stmtAssign})

	diags := runSema(t, builder, fileID)
	if !hasCode(diags, diag.SemaBorrowMutation) {
		t.Fatalf("expected %v diagnostic, got codes %v", diag.SemaBorrowMutation, diagCodes(diags))
	}
}

func TestBorrowMoveDetectedOnCall(t *testing.T) {
	builder, fileID := newTestBuilder()
	sName := intern(builder, "s")
	rName := intern(builder, "r")

	initS := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitString, intern(builder, "hi"))
	stmtS := builder.Stmts.NewLet(source.Span{}, sName, ast.NoTypeID, initS, true)

	share := builder.Exprs.NewUnary(source.Span{}, ast.ExprUnaryRef, builder.Exprs.NewIdent(source.Span{}, sName))
	stmtR := builder.Stmts.NewLet(source.Span{}, rName, ast.NoTypeID, share, false)

	callTarget := builder.Exprs.NewIdent(source.Span{}, intern(builder, "take_owned"))
	call := builder.Exprs.NewCall(source.Span{}, callTarget, []ast.ExprID{builder.Exprs.NewIdent(source.Span{}, sName)}, nil, false)
	callStmt := builder.Stmts.NewExpr(source.Span{}, call)

	addFunction(builder, fileID, "main", []ast.StmtID{stmtS, stmtR, callStmt})
	addFunction(builder, fileID, "take_owned", nil)

	diags := runSema(t, builder, fileID)
	if !hasCode(diags, diag.SemaBorrowMove) {
		t.Fatalf("expected %v diagnostic, got codes %v", diag.SemaBorrowMove, diagCodes(diags))
	}
}

func TestSpawnRejectsReferences(t *testing.T) {
	builder, fileID := newTestBuilder()
	xName := intern(builder, "x")
	rName := intern(builder, "r")
	tName := intern(builder, "task")

	initX := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "0"))
	stmtX := builder.Stmts.NewLet(source.Span{}, xName, ast.NoTypeID, initX, true)

	share := builder.Exprs.NewUnary(source.Span{}, ast.ExprUnaryRef, builder.Exprs.NewIdent(source.Span{}, xName))
	stmtR := builder.Stmts.NewLet(source.Span{}, rName, ast.NoTypeID, share, false)

	useRef := intern(builder, "use_ref")
	call := builder.Exprs.NewCall(source.Span{}, builder.Exprs.NewIdent(source.Span{}, useRef), []ast.ExprID{builder.Exprs.NewIdent(source.Span{}, rName)}, nil, false)
	spawnExpr := builder.Exprs.NewSpawn(source.Span{}, call)
	stmtSpawn := builder.Stmts.NewLet(source.Span{}, tName, ast.NoTypeID, spawnExpr, false)

	addFunction(builder, fileID, "main", []ast.StmtID{stmtX, stmtR, stmtSpawn})
	addFunction(builder, fileID, "use_ref", nil)

	diags := runSema(t, builder, fileID)
	if !hasCode(diags, diag.SemaBorrowThreadEscape) {
		t.Fatalf("expected %v diagnostic, got codes %v", diag.SemaBorrowThreadEscape, diagCodes(diags))
	}
}

func TestBorrowFieldBlocksMutation(t *testing.T) {
	builder, fileID := newTestBuilder()
	p := intern(builder, "p")
	fieldF := intern(builder, "f")

	init := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "0"))
	stmtP := builder.Stmts.NewLet(source.Span{}, p, ast.NoTypeID, init, true)

	fieldBorrowTarget := builder.Exprs.NewMember(source.Span{}, builder.Exprs.NewIdent(source.Span{}, p), fieldF)
	fieldBorrow := builder.Exprs.NewUnary(source.Span{}, ast.ExprUnaryRefMut, fieldBorrowTarget)
	stmtBorrow := builder.Stmts.NewLet(source.Span{}, intern(builder, "rf"), ast.NoTypeID, fieldBorrow, false)

	assignTarget := builder.Exprs.NewMember(source.Span{}, builder.Exprs.NewIdent(source.Span{}, p), fieldF)
	assignExpr := builder.Exprs.NewBinary(
		source.Span{},
		ast.ExprBinaryAssign,
		assignTarget,
		builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "1")),
	)
	stmtAssign := builder.Stmts.NewExpr(source.Span{}, assignExpr)

	addFunction(builder, fileID, "main", []ast.StmtID{stmtP, stmtBorrow, stmtAssign})

	diags := runSema(t, builder, fileID)
	if !hasCode(diags, diag.SemaBorrowMutation) {
		t.Fatalf("expected %v diagnostic, got codes %v", diag.SemaBorrowMutation, diagCodes(diags))
	}
}

func TestBorrowFieldIndependentMutation(t *testing.T) {
	builder, fileID := newTestBuilder()
	p := intern(builder, "p")
	fieldF := intern(builder, "f")
	fieldG := intern(builder, "g")

	init := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "0"))
	stmtP := builder.Stmts.NewLet(source.Span{}, p, ast.NoTypeID, init, true)

	fieldBorrowTarget := builder.Exprs.NewMember(source.Span{}, builder.Exprs.NewIdent(source.Span{}, p), fieldF)
	fieldBorrow := builder.Exprs.NewUnary(source.Span{}, ast.ExprUnaryRef, fieldBorrowTarget)
	stmtBorrow := builder.Stmts.NewLet(source.Span{}, intern(builder, "rf"), ast.NoTypeID, fieldBorrow, false)

	assignTarget := builder.Exprs.NewMember(source.Span{}, builder.Exprs.NewIdent(source.Span{}, p), fieldG)
	assignExpr := builder.Exprs.NewBinary(
		source.Span{},
		ast.ExprBinaryAssign,
		assignTarget,
		builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "1")),
	)
	stmtAssign := builder.Stmts.NewExpr(source.Span{}, assignExpr)

	addFunction(builder, fileID, "main", []ast.StmtID{stmtP, stmtBorrow, stmtAssign})

	diags := runSema(t, builder, fileID)
	if len(diags.Items()) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diagCodes(diags))
	}
}

func TestBorrowIndexBlocksMutation(t *testing.T) {
	builder, fileID := newTestBuilder()
	arr := intern(builder, "arr")

	init := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "0"))
	stmtArr := builder.Stmts.NewLet(source.Span{}, arr, ast.NoTypeID, init, true)

	index := builder.Exprs.NewIndex(
		source.Span{},
		builder.Exprs.NewIdent(source.Span{}, arr),
		builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "0")),
	)
	borrow := builder.Exprs.NewUnary(source.Span{}, ast.ExprUnaryRefMut, index)
	stmtBorrow := builder.Stmts.NewLet(source.Span{}, intern(builder, "ri"), ast.NoTypeID, borrow, false)

	assignIndex := builder.Exprs.NewIndex(
		source.Span{},
		builder.Exprs.NewIdent(source.Span{}, arr),
		builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "0")),
	)
	assignExpr := builder.Exprs.NewBinary(
		source.Span{},
		ast.ExprBinaryAssign,
		assignIndex,
		builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "2")),
	)
	stmtAssign := builder.Stmts.NewExpr(source.Span{}, assignExpr)

	addFunction(builder, fileID, "main", []ast.StmtID{stmtArr, stmtBorrow, stmtAssign})

	diags := runSema(t, builder, fileID)
	if !hasCode(diags, diag.SemaBorrowMutation) {
		t.Fatalf("expected %v diagnostic, got codes %v", diag.SemaBorrowMutation, diagCodes(diags))
	}
}

func TestBorrowParentChildConflict(t *testing.T) {
	builder, fileID := newTestBuilder()
	p := intern(builder, "p")
	field := intern(builder, "f")

	init := builder.Exprs.NewLiteral(source.Span{}, ast.ExprLitInt, intern(builder, "0"))
	stmtP := builder.Stmts.NewLet(source.Span{}, p, ast.NoTypeID, init, true)

	wholeBorrow := builder.Exprs.NewUnary(source.Span{}, ast.ExprUnaryRefMut, builder.Exprs.NewIdent(source.Span{}, p))
	stmtWhole := builder.Stmts.NewLet(source.Span{}, intern(builder, "whole"), ast.NoTypeID, wholeBorrow, false)

	fieldExpr := builder.Exprs.NewMember(source.Span{}, builder.Exprs.NewIdent(source.Span{}, p), field)
	fieldBorrow := builder.Exprs.NewUnary(source.Span{}, ast.ExprUnaryRefMut, fieldExpr)
	stmtField := builder.Stmts.NewLet(source.Span{}, intern(builder, "field"), ast.NoTypeID, fieldBorrow, false)

	addFunction(builder, fileID, "main", []ast.StmtID{stmtP, stmtWhole, stmtField})

	diags := runSema(t, builder, fileID)
	if !hasCode(diags, diag.SemaBorrowConflict) {
		t.Fatalf("expected %v diagnostic, got codes %v", diag.SemaBorrowConflict, diagCodes(diags))
	}
}

func newTestBuilder() (*ast.Builder, ast.FileID) {
	builder := ast.NewBuilder(ast.Hints{}, nil)
	fileID := builder.Files.New(source.Span{})
	return builder, fileID
}

func intern(builder *ast.Builder, s string) source.StringID {
	if builder == nil || builder.StringsInterner == nil {
		return source.NoStringID
	}
	return builder.StringsInterner.Intern(s)
}

func addFunction(builder *ast.Builder, file ast.FileID, name string, stmts []ast.StmtID) {
	var body ast.StmtID
	if len(stmts) > 0 {
		body = builder.Stmts.NewBlock(source.Span{}, stmts)
	}
	item := builder.NewFn(
		intern(builder, name),
		source.Span{},
		nil,
		nil,
		false,
		source.Span{},
		nil,
		nil,
		false,
		source.Span{},
		source.Span{},
		source.Span{},
		source.Span{},
		ast.NoTypeID,
		body,
		0,
		nil,
		source.Span{},
	)
	builder.PushItem(file, item)
}

func runSema(t *testing.T, builder *ast.Builder, file ast.FileID) *diag.Bag {
	t.Helper()
	resBag := diag.NewBag(16)
	symRes := symbols.ResolveFile(builder, file, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: resBag},
	})
	semaBag := diag.NewBag(16)
	Check(builder, file, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  &symRes,
	})
	return semaBag
}

func hasCode(bag *diag.Bag, code diag.Code) bool {
	if bag == nil {
		return false
	}
	for _, item := range bag.Items() {
		if item.Code == code {
			return true
		}
	}
	return false
}

func diagCodes(bag *diag.Bag) []diag.Code {
	if bag == nil {
		return nil
	}
	codes := make([]diag.Code, 0, len(bag.Items()))
	for _, item := range bag.Items() {
		codes = append(codes, item.Code)
	}
	return codes
}
