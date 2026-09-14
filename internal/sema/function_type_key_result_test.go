package sema

import (
	"testing"

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
			case "named_generic", "unresolved_generic":
				src, target, want = "fn use<T>(cb: fn() -> T) {}", "cb", "fn()->T"
			case "explicit_sources":
				src = "fn target(@return_source a: &string, b: &string) -> &string { return a; }"
				want = "fn@return_source{0}(&string,&string)->&string"
			case "typed_binding_widens":
				src = "fn first(@return_source a: &string, b: &string) -> &string { return a; } fn use() { let widened: fn(&string, &string) -> &string = first; }"
				target, want = "widened", "fn(&string,&string)->&string"
			}
			tc, _, syms := newContractChecker(t, src)
			id := functionKeySourceType(t, tc, syms, target)
			if name == "default_nothing" || name == "explicit_nothing" {
				info, ok := tc.types.FnInfo(id)
				if !ok || info.Result != tc.types.Builtins().Nothing {
					t.Fatal("source did not establish a known Nothing result")
				}
			}
			if name == "typed_binding_widens" {
				first, _ := tc.types.FnInfo(functionKeySourceType(t, tc, syms, "first"))
				wide, _ := tc.types.FnInfo(id)
				if first == nil || wide == nil || !first.ReturnSources().Equal(types.ExplicitReturnSources(0)) || !wide.ReturnSources().IsAllInputs() {
					t.Fatal("typed binding failed to retain its wider declared contract")
				}
			}
			switch name {
			case "unresolved_generic":
				info, ok := tc.types.FnInfo(id)
				if !ok || !types.ContainsGenericParam(tc.types, info.Result) || tc.typeKeyForType(id) != "fn()->T" {
					t.Fatal("source did not establish its original named generic callback")
				}
				// An incomplete reader context, not a source acceptance witness.
				tc.typeParamNames = map[types.TypeID]source.StringID{}
				want = ""
				t.Log("context control: original typed generic result with unavailable local name cache")
			case "failed_result", "nested_callback_result":
				// These are deliberate recovery descriptors, not accepted source.
				id = tc.types.RegisterFn(nil, types.NoTypeID)
				if name == "nested_callback_result" {
					id = tc.types.RegisterFn(nil, id)
				}
				want = ""
				t.Log("descriptor control: missing function result metadata; no source acceptance claim")
			}
			key := tc.typeKeyForType(id)
			if key != want {
				t.Fatalf("function result key = %q, want %q; unavailable result must not become Nothing", key, want)
			}
			params, result, sources, ok := symbols.ParseFunctionTypeKey(key)
			if want == "" {
				if ok || tc.typeFromKey(key) != types.NoTypeID {
					t.Fatal("unavailable function result rebuilt a usable descriptor")
				}
				return
			}
			info, valid := tc.types.FnInfo(id)
			if !valid || !ok || result == "" || len(params) != len(info.Params) || !sources.Equal(info.ReturnSources()) ||
				tc.types.RegisterFnWithReturnSources(info.Params, info.Result, sources) != id {
				t.Fatal("writer/parser relation roundtrip lost actual FnInfo sources or physical formals")
			}
			t.Logf("completed source-backed writer/parser contract roundtrip: %s", key)
		})
	}
}

func functionKeySourceType(t *testing.T, tc *typeChecker, syms *symbols.Result, name string) types.TypeID {
	t.Helper()
	want := tc.builder.StringsInterner.Intern(name)
	var found types.TypeID
	for symbol := symbols.SymbolID(1); int(symbol) <= syms.Table.Symbols.Len(); symbol++ {
		entry := syms.Table.Symbols.Get(symbol)
		if entry == nil || entry.Name != want {
			continue
		}
		id := tc.result.BindingTypes[symbol]
		if id == types.NoTypeID {
			id = entry.Type
		}
		if _, ok := tc.types.FnInfo(id); !ok || found != types.NoTypeID {
			t.Fatalf("source symbol %s has no unique function descriptor", name)
		}
		found = id
	}
	if found == types.NoTypeID {
		t.Fatalf("typed source symbol %s is missing", name)
	}
	return found
}
