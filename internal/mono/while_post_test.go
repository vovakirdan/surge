package mono

import (
	"errors"
	"slices"
	"testing"

	"surge/internal/hir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Nothing in Cond or Body mentions the tested type or symbols: each traversal
// must reach Post itself, independently of the normal numeric-loop body.
func whilePostFixture(ty types.TypeID) hir.Stmt {
	return hir.Stmt{Kind: hir.StmtWhile, Data: hir.WhileData{
		Body: &hir.Block{},
		Post: &hir.Expr{Kind: hir.ExprCall, Type: ty, Data: hir.CallData{
			SymbolID: 11,
			Args:     []*hir.Expr{{Kind: hir.ExprVarRef, Type: ty, Data: hir.VarRefData{Name: "only_post", SymbolID: 12}}},
		}},
	}}
}

func TestWhilePostCloneSubstitutionAndTypes(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	param := in.RegisterTypeParam(in.Strings.Intern("T"), 1, 0, false, types.NoTypeID)
	original := whilePostFixture(param)
	cloned := cloneStmt(original)
	op := original.Data.(hir.WhileData).Post
	cp := cloned.Data.(hir.WhileData).Post
	if cp == op || cp.Data.(hir.CallData).Args[0] == op.Data.(hir.CallData).Args[0] {
		t.Fatal("cloned Post shares an expression with the generic body")
	}
	subst := &Subst{Types: in, ExactArgs: map[types.TypeID]types.TypeID{param: in.Builtins().Float}}
	if err := subst.ApplyStmt(&cloned); err != nil {
		t.Fatal(err)
	}
	if cp.Type != in.Builtins().Float || cp.Data.(hir.CallData).Args[0].Type != in.Builtins().Float || op.Type != param {
		t.Fatal("Post substitution is missing or changed the original instantiation")
	}
	seen := make(map[types.TypeID]bool)
	collectTypesFromStmt(&cloned, func(ty types.TypeID) { seen[ty] = true })
	if !seen[in.Builtins().Float] || seen[param] {
		t.Fatalf("Post-only concrete type collection = %v", seen)
	}
}

func TestWhilePostSymbolRewritesAndDCEEdges(t *testing.T) {
	st := whilePostFixture(types.NoTypeID)
	calls, refs := 0, 0
	if err := rewriteCallsInStmt(&st, func(_ *hir.Expr, data *hir.CallData) error {
		calls++
		data.SymbolID = 21
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := rewriteVarRefsInStmt(&st, func(_ *hir.Expr, data *hir.VarRefData) error {
		refs++
		data.SymbolID = 22
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || refs != 1 {
		t.Fatalf("Post visits: calls=%d refs=%d, want one each", calls, refs)
	}
	got := collectCallSymsFrom(nil, &hir.Block{Stmts: []hir.Stmt{st}})
	slices.Sort(got)
	if !slices.Equal(got, []symbols.SymbolID{21, 22}) {
		t.Fatalf("DCE edges for Post-only callee and function value = %v", got)
	}
}

func TestWhilePostTraversalErrorsAndNil(t *testing.T) {
	want := errors.New("post visitor failed")
	st := whilePostFixture(types.NoTypeID)
	if err := rewriteCallsInStmt(&st, func(*hir.Expr, *hir.CallData) error { return want }); !errors.Is(err, want) {
		t.Fatalf("Post call error = %v", err)
	}
	if err := rewriteVarRefsInStmt(&st, func(*hir.Expr, *hir.VarRefData) error { return want }); !errors.Is(err, want) {
		t.Fatalf("Post ref error = %v", err)
	}
	nilPost := hir.Stmt{Kind: hir.StmtWhile, Data: hir.WhileData{}}
	if err := (&Subst{}).ApplyStmt(&nilPost); err != nil {
		t.Fatal(err)
	}
	if cloneStmt(nilPost).Data.(hir.WhileData).Post != nil {
		t.Fatal("ordinary while acquired a Post during cloning")
	}
	if err := rewriteCallsInStmt(&nilPost, func(*hir.Expr, *hir.CallData) error { return want }); err != nil {
		t.Fatal(err)
	}
	if err := rewriteVarRefsInStmt(&nilPost, func(*hir.Expr, *hir.VarRefData) error { return want }); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedLoopCandidateSurvivesMonomorphization(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	param := in.RegisterTypeParam(in.Strings.Intern("T"), 1, 0, false, types.NoTypeID)
	for _, kind := range []hir.GeneratedDropKind{hir.GeneratedDropCountedScalar, hir.GeneratedDropNumericIterableResource} {
		original := hir.Stmt{Kind: hir.StmtLet, Data: hir.LetData{Type: param, GeneratedDrop: kind}}
		cloned := cloneStmt(original)
		subst := &Subst{Types: in, ExactArgs: map[types.TypeID]types.TypeID{param: in.Builtins().Float}}
		if err := subst.ApplyStmt(&cloned); err != nil {
			t.Fatal(err)
		}
		data := cloned.Data.(hir.LetData)
		if data.Type != in.Builtins().Float || data.GeneratedDrop != kind || original.Data.(hir.LetData).Type != param {
			t.Fatalf("candidate %v did not survive independent concrete substitution: %+v", kind, data)
		}
	}
}
