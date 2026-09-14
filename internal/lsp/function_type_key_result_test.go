package lsp

import (
	"context"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestFunctionTypeKeyResultCompleteness(t *testing.T) {
	for _, name := range []string{"default_nothing", "explicit_nothing", "named_generic", "unresolved_generic", "failed_result", "nested_callback_result", "explicit_sources", "typed_binding_widens"} {
		t.Run(name, func(t *testing.T) {
			src, target, want := "fn target() {}", "target", symbols.TypeKey("fn()->nothing")
			switch name {
			case "explicit_nothing":
				src = "fn target() -> nothing { return nothing; }"
			case "named_generic":
				src, target, want = "fn use<T>(cb: fn() -> T) {}", "cb", "fn()->T"
			case "explicit_sources":
				src = "fn target(@return_source a: &string, b: &string) -> &string { return a; }"
				want = "fn@return_source{0}(&string,&string)->&string"
			case "typed_binding_widens":
				src = "fn first(@return_source a: &string, b: &string) -> &string { return a; } fn use() { let widened: fn(&string, &string) -> &string = first; }"
				target, want = "widened", "fn(&string,&string)->&string"
			}
			in, sourceTypes := functionKeyTypedSource(t, src)
			id := sourceTypes[target]
			info, valid := in.FnInfo(id)
			if !valid {
				t.Fatalf("typed source symbol %s has no function descriptor", target)
			}
			if name == "named_generic" && !types.ContainsGenericParam(in, info.Result) {
				t.Fatal("source did not establish its original named generic callback")
			}
			if (name == "default_nothing" || name == "explicit_nothing") && info.Result != in.Builtins().Nothing {
				t.Fatal("source did not establish a known Nothing result")
			}
			if name == "typed_binding_widens" {
				first, _ := in.FnInfo(sourceTypes["first"])
				if first == nil || !first.ReturnSources().Equal(types.ExplicitReturnSources(0)) || !info.ReturnSources().IsAllInputs() {
					t.Fatal("typed binding failed to retain its wider declared contract")
				}
			}
			switch name {
			case "unresolved_generic":
				// A missing-name recovery descriptor, not an accepted source type.
				generic := in.RegisterTypeParam(source.NoStringID, 1, 0, false, types.NoTypeID)
				id, want = in.RegisterFn(nil, generic), ""
				t.Log("descriptor control: generic result with unavailable name metadata")
			case "failed_result", "nested_callback_result":
				id = in.RegisterFn(nil, types.NoTypeID)
				if name == "nested_callback_result" {
					id = in.RegisterFn(nil, id)
				}
				want = ""
				t.Log("descriptor control: missing function result metadata; no source acceptance claim")
			}
			key := typeKeyForType(in, id)
			if key != want {
				t.Fatalf("function result key = %q, want %q; unavailable result must not become Nothing", key, want)
			}
			params, result, sources, ok := symbols.ParseFunctionTypeKey(key)
			if want == "" {
				if ok {
					t.Fatal("unavailable function result parsed as a complete descriptor")
				}
				return
			}
			if !ok || result == "" || len(params) != len(info.Params) || !sources.Equal(info.ReturnSources()) ||
				in.RegisterFnWithReturnSources(info.Params, info.Result, sources) != id {
				t.Fatal("writer/parser relation roundtrip lost actual FnInfo sources or physical formals")
			}
			t.Logf("completed source-backed writer/parser contract roundtrip: %s", key)
		})
	}
}

func functionKeyTypedSource(t *testing.T, text string) (*types.Interner, map[string]types.TypeID) {
	t.Helper()
	fs := source.NewFileSet()
	file := fs.Get(fs.AddVirtual("/writer-source.sg", []byte(text)))
	builder := ast.NewBuilder(ast.Hints{}, source.NewInterner())
	bag := diag.NewBag(32)
	reporter := &diag.BagReporter{Bag: bag}
	parsed := parser.ParseFile(context.Background(), fs, lexer.New(file, lexer.Options{Reporter: reporter}), builder, parser.Options{Reporter: reporter})
	if bag.HasErrors() {
		t.Fatalf("writer source parse failed: %+v", bag.Items())
	}
	syms := symbols.ResolveFile(builder, parsed.File, &symbols.ResolveOptions{Reporter: reporter})
	if bag.HasErrors() {
		t.Fatalf("writer source resolution failed: %+v", bag.Items())
	}
	result := sema.Check(context.Background(), builder, parsed.File, sema.Options{Symbols: &syms, Reporter: reporter})
	if bag.HasErrors() {
		t.Fatalf("writer source typing failed: %+v", bag.Items())
	}
	typed := make(map[string]types.TypeID)
	for i, symbol := range syms.Table.Symbols.Data() {
		id := result.BindingTypes[symbols.SymbolID(i+1)]
		if id == types.NoTypeID {
			id = symbol.Type
		}
		if _, ok := result.TypeInterner.FnInfo(id); ok {
			typed[builder.StringsInterner.MustLookup(symbol.Name)] = id
		}
	}
	return result.TypeInterner, typed
}
