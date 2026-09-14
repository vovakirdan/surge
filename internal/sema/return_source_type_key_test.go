package sema

import (
	"slices"
	"testing"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestReturnSourceTypeKeyReconstruction(t *testing.T) {
	for _, name := range []string{"direct", "inference", "result", "nested_param", "nested_result", "nested_generic", "explicit_empty", "generic_callback", "invalid_marker"} {
		t.Run(name, func(t *testing.T) {
			in := types.NewInterner()
			in.Strings = source.NewInterner()
			ref := in.Intern(types.MakeReference(in.Builtins().String, false))
			narrow := in.RegisterFnWithReturnSources([]types.TypeID{ref}, ref, types.ExplicitReturnSources(0))
			all := in.RegisterFn([]types.TypeID{ref}, ref)
			checker := typeChecker{types: in, typeKeys: make(map[string]types.TypeID)}
			key := symbols.TypeKey("fn@return_source{0}(&string)->&string")
			want := narrow
			bindings := map[string]types.TypeID{"T": in.Builtins().String}
			params := map[string]struct{}{"T": {}}
			var got types.TypeID
			switch name {
			case "inference":
				bindings = make(map[string]types.TypeID)
				got = checker.instantiateTypeKeyWithInference("fn@return_source{0}(&T)->&T", all, bindings, params)
				if bindings["T"] != in.Builtins().String {
					t.Fatal("function inference lost T")
				}
				if wide := checker.instantiateTypeKeyWithInference("fn(&T)->&T", narrow, bindings, params); wide != all {
					t.Fatal("explicit widening lost expected AllInputs metadata")
				}
			case "result":
				got = checker.instantiateResultType("fn@return_source{0}(&T)->&T", bindings, params)
			case "nested_param":
				key = "fn(fn@return_source{0}(&string)->&string,int)->bool"
				want = in.RegisterFn([]types.TypeID{narrow, in.Builtins().Int}, in.Builtins().Bool)
			case "nested_result":
				key = "fn(int)->fn@return_source{0}(&string)->&string"
				want = in.RegisterFn([]types.TypeID{in.Builtins().Int}, narrow)
			case "nested_generic":
				pair := in.RegisterFnWithReturnSources([]types.TypeID{ref, ref}, ref, types.ExplicitReturnSources(0, 1))
				nominalKey := "Box<fn@return_source{0,1}(&string,&string)->&string,int>"
				box := in.RegisterStructInstance(in.Strings.Intern("Box"), source.Span{}, []types.TypeID{pair, in.Builtins().Int})
				checker.typeKeys[nominalKey] = box
				key = symbols.TypeKey("fn(" + nominalKey + ",bool)->int")
				want = in.RegisterFn([]types.TypeID{box, in.Builtins().Bool}, in.Builtins().Int)
				parts := splitTopLevel("fn@return_source{0,1}(&string,&string)->&string,int")
				if !slices.Equal(parts, []string{"fn@return_source{0,1}(&string,&string)->&string", "int"}) {
					t.Fatalf("generic argument keys split at nested metadata/arrow: %v", parts)
				}
				path := splitTypePathSegments("remote::Box<fn@return_source{0}(&string)->&other::Item>::Tail")
				if !slices.Equal(path, []string{"remote", "Box<fn@return_source{0}(&string)->&other::Item>", "Tail"}) {
					t.Fatalf("qualified path split inside a callback result: %v", path)
				}
			case "explicit_empty":
				key = "fn@return_source{}()->string"
				want = in.RegisterFnWithReturnSources(nil, in.Builtins().String, types.ExplicitReturnSources())
			case "generic_callback":
				key = "fn(fn@return_source{0}(&T)->&T)->fn@return_source{0}(&T)->&T"
				want = in.RegisterFn([]types.TypeID{narrow}, narrow)
				got = checker.instantiateResultType(key, bindings, params)
			case "invalid_marker":
				for _, bad := range []symbols.TypeKey{"fn@return_source{2}(&string)->&string", "fn@return_source{bad}(&string)->&string", "fn@return_source{1}(Unknown,&string)->&string"} {
					if checker.typeFromKey(bad) != types.NoTypeID || checker.instantiateResultType(bad, bindings, params) != types.NoTypeID || checker.instantiateTypeKeyWithInference(bad, all, bindings, params) != types.NoTypeID {
						t.Fatalf("invalid key recovered a usable/default function: %q", bad)
					}
				}
				return
			}
			if got == types.NoTypeID && name != "inference" && name != "result" && name != "generic_callback" {
				got = checker.typeFromKey(key)
			}
			if got == types.NoTypeID || got != want {
				t.Fatalf("reconstructed type = %d, want exact %d for %q", got, want, key)
			}
			info, ok := in.FnInfo(got)
			wantInfo, _ := in.FnInfo(want)
			if !ok || !info.ReturnSources().Equal(wantInfo.ReturnSources()) {
				t.Fatal("resolved function lost its declared relation")
			}
			if name == "direct" || name == "nested_param" || name == "nested_result" || name == "explicit_empty" {
				if written := checker.typeKeyForType(got); written != key {
					t.Fatalf("typed writer key = %q, want %q", written, key)
				}
			}
		})
	}
}
