package parser_test

import (
	"bytes"
	"context"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
)

func benchParse(b *testing.B, program []byte) {
	fs := source.NewFileSetWithBase("")
	fileID := fs.AddVirtual("bench.sg", program)
	file := fs.Get(fileID)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		builder := ast.NewBuilder(ast.Hints{}, nil)
		bag := diag.NewBag(0)
		lx := lexer.New(file, lexer.Options{})
		parser.ParseFile(context.Background(), fs, lx, builder, parser.Options{
			Reporter: &diag.BagReporter{Bag: bag},
		})
	}
}

func BenchmarkParseShort(b *testing.B) {
	src := []byte(`import std/time; fn main(){}`)
	benchParse(b, src)
}

func BenchmarkParseLarge(b *testing.B) {
	var buf bytes.Buffer
	buf.WriteString("import std/a;\n")
	for i := range 2000 {
		buf.WriteString("fn f")
		buf.WriteByte(byte('a' + (i % 26)))
		buf.WriteString("(){ let x:i32=1; }\n")
	}
	benchParse(b, buf.Bytes())
}
