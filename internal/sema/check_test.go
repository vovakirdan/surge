package sema

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/source"
)

func TestCheckInitializesTypeInterner(t *testing.T) {
	builder := ast.NewBuilder(ast.Hints{}, nil)
	file := builder.Files.New(source.Span{})
	res := Check(builder, file, Options{})
	if res.TypeInterner == nil {
		t.Fatalf("expected type interner")
	}
}
