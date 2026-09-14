package parser

import (
	"strings"
	"testing"

	"surge/internal/ast"
)

func TestReturnSourceParameterSyntax(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		count int
	}{
		{"named", "fn first(@return_source a: &string, b: &string) -> &string { return a; }", 1},
		{"method_self", "extern<string> { fn first(@return_source self: &string) -> &string; }", 1},
		{"extern", "extern<string> { fn first(a: &string, @return_source b: &string) -> &string; }", 1},
		{"callback", "type First = fn(@return_source &string, &string) -> &string;", 1},
		{"empty_parens", "type First = fn(@return_source() &string) -> &string;", 1},
		{"union_marks", "type Either = fn(@return_source &string, @return_source &string) -> &string;", 2},
		{"nested_callback", "type Nested = fn(fn(@return_source &string) -> &string) -> nothing;", 1},
		{"variadic", "type Sources = fn(@return_source ...&string) -> &string;", 1},
		{"default", "fn first(n: int = 0, @return_source a: &string) -> &string;", 1},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			builder, _, bag := parseSource(t, test.src)
			if bag.HasErrors() {
				t.Fatalf("parse failed: %s", diagnosticsSummary(bag))
			}
			var attached []ast.Attr
			for _, param := range builder.Items.FnParams.Slice() {
				attached = append(attached, builder.Items.CollectAttrs(param.AttrStart, param.AttrCount)...)
			}
			for _, fn := range builder.Types.Fns.Slice() {
				for _, param := range fn.Params {
					attached = append(attached, builder.Items.CollectAttrs(param.AttrStart, param.AttrCount)...)
				}
			}
			if len(attached) != test.count {
				t.Fatalf("attached markers = %d, want %d", len(attached), test.count)
			}
			for _, attr := range attached {
				if builder.StringsInterner.MustLookup(attr.Name) != "return_source" || len(attr.Args) != 0 ||
					!strings.HasPrefix(test.src[attr.Span.Start:attr.Span.End], "@return_source") {
					t.Fatalf("lost marker name, zero arity, or span: %+v", attr)
				}
			}
		})
	}
}
