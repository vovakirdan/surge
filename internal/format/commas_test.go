package format

import (
	"fmt"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
)

func parseSource(t *testing.T, src []byte) (*source.FileSet, *source.File, *ast.Builder, ast.FileID) {
	t.Helper()

	fs := source.NewFileSetWithBase("")
	fileID := fs.AddVirtual("fmt.sg", src)
	sf := fs.Get(fileID)

	bag := diag.NewBag(128)
	lx := lexer.New(sf, lexer.Options{Reporter: (&lexer.ReporterAdapter{Bag: bag}).Reporter()})
	builder := ast.NewBuilder(ast.Hints{}, nil)
	opts := parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: 128,
	}
	result := parser.ParseFile(fs, lx, builder, opts)
	if bag.HasErrors() {
		issues := make([]string, 0, bag.Len())
		for _, d := range bag.Items() {
			issues = append(issues, fmt.Sprintf("%s: %s", d.Code, d.Message))
		}
		t.Fatalf("parse failed: %v", issues)
	}

	return fs, sf, builder, result.File
}

func TestFormatFileBasic(t *testing.T) {
	src := []byte(
		"import std/math::{sin as s ,cos,};\n" +
			"type Vec2 = { x: int , y: int, };\n" +
			"type Shape = Circle(Point ,int,) | nothing;\n" +
			"fn foo<T>(a: int=call(x ,y,z ,), b :int,) -> Vec2;\n",
	)
	_, sf, builder, fileID := parseSource(t, src)

	formatted, err := FormatFile(sf, builder, fileID, Options{})
	if err != nil {
		t.Fatalf("FormatFile failed: %v", err)
	}

	got := string(formatted)
	want := "import std/math::{sin as s, cos};\n" +
		"type Vec2 = { x: int, y: int, };\n" +
		"type Shape = Circle(Point, int,) | nothing;\n" +
		"fn foo<T>(a: int = call(x, y, z,), b: int,) -> Vec2;\n"

	if got != want {
		t.Fatalf("FormatFile mismatch:\nwant %q\ngot  %q", want, got)
	}

	if ok, msg := CheckRoundTrip(sf, Options{}, 128); !ok {
		t.Fatalf("CheckRoundTrip failed: %s", msg)
	}
}
