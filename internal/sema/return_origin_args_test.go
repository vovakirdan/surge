package sema

import (
	"crypto/sha256"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

func TestReturnOriginArgumentsKeepDeclarationSlots(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"reversed_named_trailing_default", `fn choose(a: &string, b: &string, ignored: int = 1) -> &string { return b; }
fn probe(a: &string, b: &string) -> &string { return choose(b: b, a: a); }
`},
		{"variadic_union", `fn choose(a: &string, ...rest: &string) -> &string { return a; }
fn probe(a: &string, b: &string) -> &string { return choose(a, b, a); }
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("RETURN_ORIGIN_SLOT_SOURCE sha256=%x source=%q", sha256.Sum256([]byte(tc.src)), tc.src)
			builder, file, bag := parseSnippet(t, tc.src)
			if bag.HasErrors() {
				t.Fatalf("PRECONDITION: parse diagnostics: %s", diagnosticsSummary(bag))
			}
			resolved := symbols.ResolveFile(builder, file, &symbols.ResolveOptions{Reporter: &diag.BagReporter{Bag: bag}})
			if bag.HasErrors() {
				t.Fatalf("PRECONDITION: resolver diagnostics: %s", diagnosticsSummary(bag))
			}
			items := builder.Files.Get(file).Items
			if len(items) != 2 || len(resolved.ItemSymbols[items[0]]) != 1 {
				t.Fatal("PRECONDITION: two source declarations are missing")
			}
			sig := resolved.Table.Symbols.Get(resolved.ItemSymbols[items[0]][0]).Signature
			fn, ok := builder.Items.Fn(items[1])
			if !ok || fn == nil || len(builder.Stmts.Block(fn.Body).Stmts) != 1 {
				t.Fatal("PRECONDITION: actual probe body is missing")
			}
			ret := builder.Stmts.Return(builder.Stmts.Block(fn.Body).Stmts[0])
			call, ok := builder.Exprs.Call(ret.Expr)
			if !ok || call == nil {
				t.Fatal("PRECONDITION: actual parsed call is missing")
			}
			slots, err := mapReturnOriginArguments(sig, call, ast.NoExprID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "variadic_union" {
				if len(slots) != 2 || !slices.Equal(slots[0].exprs, []ast.ExprID{call.Args[0].Value}) || !slices.Equal(slots[1].exprs, []ast.ExprID{call.Args[1].Value, call.Args[2].Value}) || slots[1].defaulted {
					t.Fatalf("variadic actuals lost their one declaration slot: %+v", slots)
				}
			} else if len(slots) != 3 || !slices.Equal(slots[0].exprs, []ast.ExprID{call.Args[1].Value}) || !slices.Equal(slots[1].exprs, []ast.ExprID{call.Args[0].Value}) || !slots[2].defaulted || len(slots[2].exprs) != 0 {
				t.Fatalf("named/default metadata shifted formal origins: %+v", slots)
			}
		})
	}
}

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
