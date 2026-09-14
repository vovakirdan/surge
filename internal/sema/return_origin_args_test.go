package sema

import (
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// This checks origin mapping against real resolver metadata. The public call
// typer currently rejects an omitted middle default before origin analysis.
func TestReturnOriginArgumentsPreserveOmittedMiddleDefault(t *testing.T) {
	builder, file, parseBag := parseSnippet(t, `fn choose(a: &string, ignored: int = 1, b: &string) -> &string { return b; }`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	bag := diag.NewBag(16)
	resolved := symbols.ResolveFile(builder, file, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: bag},
	})
	if bag.HasErrors() {
		t.Fatalf("unexpected resolve diagnostics: %s", diagnosticsSummary(bag))
	}
	items := builder.Files.Get(file).Items
	if len(items) != 1 || len(resolved.ItemSymbols[items[0]]) != 1 {
		t.Fatal("parsed declaration has no unique resolved function")
	}
	sym := resolved.Table.Symbols.Get(resolved.ItemSymbols[items[0]][0])
	if sym == nil || sym.Signature == nil {
		t.Fatal("resolved function has no signature")
	}
	sig := sym.Signature
	if len(sig.ParamNames) != 3 || !slices.Equal(sig.Params, []symbols.TypeKey{"&string", "int", "&string"}) ||
		!slices.Equal(sig.Defaults, []bool{false, true, false}) {
		t.Fatalf("original declaration metadata changed: %+v", sig)
	}
	actualA := builder.Exprs.NewIdent(source.Span{}, sig.ParamNames[0])
	actualB := builder.Exprs.NewIdent(source.Span{}, sig.ParamNames[2])
	call := &ast.ExprCallData{Args: []ast.CallArg{
		{Name: sig.ParamNames[2], Value: actualB},
		{Name: sig.ParamNames[0], Value: actualA},
	}}
	slots, err := mapReturnOriginArguments(sig, call, ast.NoExprID)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 3 || !slices.Equal(slots[0].exprs, []ast.ExprID{actualA}) || slots[0].defaulted ||
		len(slots[1].exprs) != 0 || !slots[1].defaulted ||
		!slices.Equal(slots[2].exprs, []ast.ExprID{actualB}) || slots[2].defaulted {
		t.Fatalf("middle default lost or shifted declaration slots: %+v", slots)
	}
}
