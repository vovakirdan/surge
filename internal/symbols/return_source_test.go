package symbols

import (
	"os"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
)

func TestReturnSourceSignatureMetadata(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		slots []uint32
	}{
		{"named", "fn first(@return_source a: &string, b: &string) -> &string;", []uint32{0}},
		{"self_slot", "extern<string> { fn first(@return_source self: &string, other: &string) -> &string; }", []uint32{0}},
		{"default_slot", "fn first(n: int = 0, @return_source a: &string) -> &string;", []uint32{1}},
		{"extern", "extern<string> { fn first(a: &string, @return_source b: &string) -> &string; }", []uint32{1}},
		{"callback", "type First = fn(&string, @return_source &string) -> &string;", []uint32{1}},
		{"all_default", "fn first(a: &string) -> &string;", nil},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			builder, _, bag := parseSnippet(t, test.src)
			if bag.HasErrors() {
				t.Fatalf("parse failed: %v", bag.Items())
			}
			var syntax ReturnSourceSyntax
			if test.name == "callback" {
				for id := uint32(1); id <= builder.Types.Arena.Len(); id++ {
					if builder.Types.Get(ast.TypeID(id)).Kind == ast.TypeExprFn {
						syntax = FunctionTypeReturnSourceSyntax(builder, ast.TypeID(id))
					}
				}
			} else {
				fn := builder.Items.Fns.Get(1)
				sig := buildFunctionSignature(builder, fn)
				syntax = sig.ReturnSourceSyntax
				if len(syntax.Params()) != len(sig.Params) || syntax.Result() != fn.ReturnType ||
					(test.name == "self_slot" && !sig.HasSelf) || (test.name == "default_slot" && !sig.Defaults[0]) {
					t.Fatal("signature lost physical slots, self, defaults, or original roots")
				}
			}
			if !slices.Equal(syntax.Sources().Slots(), test.slots) || syntax.Sources().IsAllInputs() != (test.slots == nil) {
				t.Fatalf("wrong contract: %+v", syntax.Sources())
			}
			if syntax.Span().End <= syntax.Span().Start {
				t.Fatal("missing declaration span")
			}
			if len(test.slots) > 0 {
				markers := syntax.Markers()
				params := syntax.Params()
				markers[0].Slot = 99
				params[0] = ast.NoTypeID
				if !slices.Equal(syntax.Sources().Slots(), test.slots) || syntax.Params()[0] == ast.NoTypeID {
					t.Fatal("syntax exposed mutable marker or parameter storage")
				}
			}
		})
	}
}

func TestReturnSourceIntrinsicDeclarations(t *testing.T) {
	content, err := os.ReadFile("../../core/intrinsics.sg")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, match, context string }{
		{"array_dynamic_mut", "fn rt_array_get_mut<T>(", ""},
		{"array_fixed_mut", "fn rt_array_get_mut<T, const N:int>(", ""},
		{"map_ref", "fn rt_map_get_ref<K, V>(", ""},
		{"map_mut", "fn rt_map_get_mut<K, V>(", ""},
		{"array_dynamic_index", "fn __index(@return_source self: &Array<T>, index: int) -> &T;", "Array<T>"},
		{"array_fixed_index", "fn __index(@return_source self: &ArrayFixed<T, N>, index: int) -> &T;", "ArrayFixed<T, N>"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var lines []string
			for _, line := range strings.Split(string(content), "\n") {
				if strings.Contains(line, test.match) {
					lines = append(lines, strings.TrimSpace(line))
				}
			}
			if len(lines) != 1 {
				t.Fatalf("found %d declarations for %q", len(lines), test.match)
			}
			src := lines[0]
			if test.context != "" {
				src = "extern<" + test.context + "> { " + src + " }"
			}
			builder, _, bag := parseSnippet(t, src)
			if bag.HasErrors() {
				t.Fatalf("actual intrinsic parse failed: %v", bag.Items())
			}
			syntax := buildFunctionSignature(builder, builder.Items.Fns.Get(1)).ReturnSourceSyntax
			if syntax.Sources().IsAllInputs() || !slices.Equal(syntax.Sources().Slots(), []uint32{0}) ||
				len(syntax.Markers()) != 1 || syntax.Markers()[0].ArgumentCount != 0 {
				t.Fatal("actual intrinsic lost its exact receiver-only promise")
			}
		})
	}
}
