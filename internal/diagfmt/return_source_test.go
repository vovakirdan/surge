package diagfmt

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/source"
)

func TestReturnSourceTypeDiagnostic(t *testing.T) {
	for _, name := range []string{"callback", "nested_callback"} {
		t.Run(name, func(t *testing.T) {
			builder := ast.NewBuilder(ast.Hints{}, nil)
			stringType := builder.Types.NewPath(source.Span{}, []ast.TypePathSegment{{Name: builder.StringsInterner.Intern("string")}})
			refType := builder.Types.NewUnary(source.Span{}, ast.TypeUnaryRef, stringType)
			start, count := builder.Items.AllocateAttrs([]ast.Attr{{Name: builder.StringsInterner.Intern("return_source")}})
			fn := builder.Types.NewFn(source.Span{}, []ast.TypeFnParam{{Type: refType, AttrStart: start, AttrCount: count}, {Type: refType}}, refType)
			want := "fn(@return_source &string, &string) -> &string"
			if name == "nested_callback" {
				fn = builder.Types.NewFn(source.Span{}, []ast.TypeFnParam{{Type: fn}}, refType)
				want = "fn(" + want + ") -> &string"
			}
			if got := formatTypeExprInline(builder, fn); got != want {
				t.Fatalf("diagnostic type = %q, want %q", got, want)
			}
		})
	}
}
