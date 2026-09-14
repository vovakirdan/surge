package symbols

import (
	"slices"
	"testing"

	"surge/internal/types"
)

func TestFunctionReturnSourceTypeKeys(t *testing.T) {
	cases := []struct {
		name, key, canonical string
		slots                []uint32
	}{
		{"legacy", "fn(&string,int)->&string", "fn(&string,int)->&string", nil},
		{"explicit0", "fn@return_source{0}(&string,int)->&string", "fn@return_source{0}(&string,int)->&string", []uint32{0}},
		{"union", "fn@return_source{1,0,1}(&string,&string)->&string", "fn@return_source{0,1}(&string,&string)->&string", []uint32{0, 1}},
		{"empty", "fn@return_source{}()->string", "fn@return_source{}()->string", []uint32{}},
		{"nested_param", "fn(fn@return_source{0}(&string)->&string,int)->bool", "fn(fn@return_source{0}(&string)->&string,int)->bool", nil},
		{"nested_result", "fn(int)->fn@return_source{0}(&string)->&string", "fn(int)->fn@return_source{0}(&string)->&string", nil},
		{"nested_generic", "fn(Box<fn@return_source{0,1}(&string,&string)->&string,int>,bool)->int", "fn(Box<fn@return_source{0,1}(&string,&string)->&string,int>,bool)->int", nil},
		{"nested_tuple", "fn((fn(&string)->&string,int),bool)->int", "fn((fn(&string)->&string,int),bool)->int", nil},
		{"nested_array", "fn([fn(&string)->&string; 2],bool)->int", "fn([fn(&string)->&string; 2],bool)->int", nil},
		{"qualified_param", "fn@return_source{0}(&remote::Item,other::Value)->&remote::Item", "fn@return_source{0}(&remote::Item,other::Value)->&remote::Item", []uint32{0}},
		{"malformed_metadata", "fn@return_source{no}(&string)->&string", "", nil},
		{"invalid_slot", "fn@return_source{1}(&string)->&string", "", nil},
		{"unbalanced", "fn([int),bool)->int", "", nil},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			params, result, sources, ok := ParseFunctionTypeKey(TypeKey(test.key))
			if ok != (test.canonical != "") {
				t.Fatalf("parse success = %t for %q", ok, test.key)
			}
			if !ok {
				if test.name == "malformed_metadata" {
					for _, bad := range []TypeKey{"fn@return_source(&string)->&string", "fn@return_source{0,}(&string)->&string", "fn@return_source{4294967296}(&string)->&string"} {
						if _, _, _, accepted := ParseFunctionTypeKey(bad); accepted {
							t.Fatalf("accepted malformed key %q", bad)
						}
					}
				}
				if test.name == "invalid_slot" && FunctionTypeKey([]TypeKey{"", "int"}, "int", types.ExplicitReturnSources(1)) != "" {
					t.Fatal("writer silently dropped an unresolved formal slot")
				}
				return
			}
			if sources.IsAllInputs() != (test.slots == nil) || !slices.Equal(sources.Slots(), test.slots) {
				t.Fatalf("wrong declared sources: %+v", sources)
			}
			if got := FunctionTypeKey(params, result, sources); got != TypeKey(test.canonical) {
				t.Fatalf("round trip = %q, want %q", got, test.canonical)
			}
			if test.name == "nested_param" && (len(params) != 2 || params[0] != "fn@return_source{0}(&string)->&string" || result != "bool") {
				t.Fatal("nested arrow ended the outer parameter list")
			}
		})
	}
}

func returnSourceKeyTestSignature(t *testing.T, src string) *FunctionSignature {
	t.Helper()
	builder, _, bag := parseSnippet(t, src)
	if bag.HasErrors() {
		t.Fatalf("parse failed: %v", bag.Items())
	}
	return buildFunctionSignature(builder, builder.Items.Fns.Get(1))
}

func TestReturnSourceSignatureKeyPreservation(t *testing.T) {
	cases := []struct{ name, src, param, result, identity string }{
		{"callback_param", "fn f(cb: fn(@return_source &string, &string) -> &string);", "fn@return_source{0}(&string,&string)->&string", "", ""},
		{"callback_result", "fn f() -> fn(@return_source &string) -> &string;", "", "fn@return_source{0}(&string)->&string", ""},
		{"legacy_named", "fn f(a: &string) -> &string;", "&string", "&string", "&string,->&string"},
		{"named_contract", "fn f(@return_source a: &string) -> &string;", "&string", "&string", "&string,->&string@return_source{0}"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			sig := returnSourceKeyTestSignature(t, test.src)
			if test.param != "" && (len(sig.Params) != 1 || string(sig.Params[0]) != test.param) {
				t.Fatalf("parameter keys = %v", sig.Params)
			}
			if test.result != "" && string(sig.Result) != test.result {
				t.Fatalf("result key = %q", sig.Result)
			}
			if test.identity != "" && signatureKey(sig) != test.identity {
				t.Fatalf("signature identity = %q", signatureKey(sig))
			}
		})
	}
}

func TestReturnSourceSignatureShape(t *testing.T) {
	cases := []struct {
		name        string
		left, right TypeKey
		same        bool
	}{
		{"shape_ignores_sources", "fn@return_source{0}(&string,&string)->&string", "fn(&string,&string)->&string", true},
		{"result_shape_still_distinct", "fn@return_source{0}(&string)->&string", "fn(&string)->int", false},
		{"nested_ignores_sources", "Box<fn@return_source{0}(&string)->&string>", "Box<fn(&string)->&string>", true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			a := &FunctionSignature{Params: []TypeKey{test.left}, Variadic: []bool{false}, Result: "int"}
			b := &FunctionSignature{Params: []TypeKey{test.right}, Variadic: []bool{false}, Result: "int"}
			if got := signaturesEqual(a, b); got != test.same {
				t.Fatalf("ordinary shape equality = %t, want %t", got, test.same)
			}
			a.Params, b.Params, a.Result, b.Result = nil, nil, test.left, test.right
			if got := signaturesEqual(a, b); got != test.same {
				t.Fatalf("result shape equality = %t, want %t", got, test.same)
			}
		})
	}
}
