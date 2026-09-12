package hir

import (
	"bytes"
	"testing"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

func TestNumericForUsesOwnedCandidatesAndSinglePost(t *testing.T) {
	in := types.NewInterner()
	ctx := &normCtx{mod: &Module{TypeInterner: in}, nextTemp: 1}
	ty := in.Builtins().Int
	body := &Block{Stmts: []Stmt{{Kind: StmtContinue, Data: ContinueData{}}}}
	data := ForData{Kind: ForIn, VarName: "x", VarSym: 7, VarType: ty, Body: body,
		Iterable: ctx.binary(ast.ExprBinaryRange, ctx.intLit(1, ty, source.Span{}), ctx.intLit(3, ty, source.Span{}), types.NoTypeID, source.Span{}),
	}
	stmts, err := normalizeForIn(ctx, source.Span{}, data)
	if err != nil {
		t.Fatal(err)
	}
	outer := stmts[0].Data.(BlockStmtData).Block
	if len(outer.Stmts) != 3 {
		t.Fatalf("numeric outer statements = %d, want current/end/while", len(outer.Stmts))
	}
	for i := 0; i < 2; i++ {
		if got := outer.Stmts[i].Data.(LetData).GeneratedDrop; got != GeneratedDropCountedScalar {
			t.Fatalf("bound %d ownership candidate = %v", i, got)
		}
	}
	w := outer.Stmts[2].Data.(WhileData)
	if w.Post == nil || len(w.Body.Stmts) != 1 || w.Body.Stmts[0].Kind != StmtContinue {
		t.Fatal("numeric continue was rewritten or latch was appended to the body")
	}
	post := w.Post.Data.(BinaryOpData)
	if post.Op != ast.ExprBinaryAssign || !post.DropOverwritten || post.Right.Data.(BinaryOpData).Op != ast.ExprBinaryAdd {
		t.Fatal("numeric Post does not use the established overwrite operation")
	}
}

func TestIteratorOwnershipCandidatesAndElementFallback(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	name := in.Strings.Intern("Array")
	in.EnsureArrayNominal(name, in.Strings.Intern("T"), source.Span{}, 1)
	floatTy := in.Builtins().Float
	arrayTy := in.RegisterStructInstance(name, source.Span{}, []types.TypeID{floatTy})
	ctx := &normCtx{mod: &Module{TypeInterner: in}, nextTemp: 1}
	array := &Expr{Kind: ExprArrayLit, Type: arrayTy, Data: ArrayLitData{}}
	stmts, err := normalizeIterFor(ctx, source.Span{}, ForData{
		Kind: ForIn, VarName: "x", VarSym: 7, Body: &Block{},
		Iterable: &Expr{Kind: ExprOwnedTemp, Type: arrayTy, Data: OwnedTempData{Inner: array}},
	})
	if err != nil {
		t.Fatal(err)
	}
	outer := stmts[0].Data.(BlockStmtData).Block
	for i := 0; i < 2; i++ {
		if got := outer.Stmts[i].Data.(LetData).GeneratedDrop; got != GeneratedDropNumericIterableResource {
			t.Fatalf("hoisted source/cursor %d candidate = %v", i, got)
		}
	}
	w := outer.Stmts[2].Data.(WhileData)
	if w.Post != nil {
		t.Fatal("generic iterator acquired a numeric Post")
	}
	x := w.Body.Stmts[2].Data.(LetData)
	if x.Type != floatTy || x.GeneratedDrop != GeneratedDropCountedScalar {
		t.Fatalf("missing VarType/bindingType fallback: x type=%v candidate=%v", x.Type, x.GeneratedDrop)
	}
	if x.Value.Data.(TagPayloadData).SubjectBorrowed {
		t.Fatal("Some payload ceased to transfer its value")
	}
}

func TestWhilePostNormalizationAndLegacyReleaseTraversal(t *testing.T) {
	postBody := &Block{Stmts: []Stmt{{Kind: StmtFor, Data: ForData{Kind: ForClassic, Body: &Block{}}}}}
	post := &Expr{Kind: ExprBlock, Data: BlockExprData{Block: postBody}}
	st := Stmt{Kind: StmtWhile, Data: WhileData{Body: &Block{}, Post: post}}
	if _, err := normalizeStmt(&normCtx{nextTemp: 1}, &st); err != nil {
		t.Fatal(err)
	}
	if postBody.Stmts[0].Kind != StmtBlock {
		t.Fatal("for inside Post escaped normalization")
	}
	postBody.Stmts = []Stmt{{Kind: StmtReturn, Data: ReturnData{}}}
	release := Stmt{Kind: StmtEnvelopeRelease, Data: EnvelopeReleaseData{Cursor: true}}
	injectIterCursorReleaseBeforeReturns(&Block{Stmts: []Stmt{st}}, release)
	if postBody.Stmts[0].Kind != StmtBlock {
		t.Fatal("existing nonnumeric return release walker skipped Post")
	}
	wrapped := postBody.Stmts[0].Data.(BlockStmtData).Block.Stmts
	if len(wrapped) != 2 || wrapped[0].Kind != StmtEnvelopeRelease || wrapped[1].Kind != StmtReturn {
		t.Fatal("Post return did not retain the legacy release contract")
	}
}

func TestWhilePostPrintingAndClassicForRemainDistinct(t *testing.T) {
	ctx := &normCtx{nextTemp: 1}
	cond := ctx.boolLit(true, source.Span{})
	var buf bytes.Buffer
	p := NewPrinter(&buf, nil)
	w := WhileData{Cond: cond, Body: &Block{}}
	p.printWhile(w)
	if got := buf.String(); got != "while true {\n}\n" {
		t.Fatalf("nil-Post dump changed: %q", got)
	}
	buf.Reset()
	w.Post = cond
	p.printWhile(w)
	if got := buf.String(); got != "while true {\n} post true\n" {
		t.Fatalf("Post dump = %q", got)
	}
	stmts, err := normalizeForClassic(ctx, source.Span{}, ForData{
		Cond: cond, Post: cond, Body: &Block{Stmts: []Stmt{{Kind: StmtContinue, Data: ContinueData{}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	classic := stmts[0].Data.(BlockStmtData).Block.Stmts[0].Data.(WhileData)
	if classic.Post != nil || len(classic.Body.Stmts) != 2 || classic.Body.Stmts[0].Kind != StmtBlock {
		t.Fatal("classical for no longer uses its existing continue rewrite")
	}
}
