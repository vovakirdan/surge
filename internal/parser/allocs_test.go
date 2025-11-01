package parser_test

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
)

func TestParseAllocs(t *testing.T) {
	fs := source.NewFileSetWithBase("")
	fileID := fs.AddVirtual("alloc.sg", []byte("import std/time; fn main(){ let x = 1; }"))
	file := fs.Get(fileID)

	allocs := testing.AllocsPerRun(100, func() {
		builder := ast.NewBuilder(ast.Hints{}, nil)
		bag := diag.NewBag(0)
		lx := lexer.New(file, lexer.Options{})
		parser.ParseFile(fs, lx, builder, parser.Options{
			Reporter: &diag.BagReporter{Bag: bag},
		})
	})

	t.Logf("allocs/op: %.1f", allocs)
}
