package format

import (
	"fmt"
	"slices"
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

func TestNormalizeCommas(t *testing.T) {
	src := []byte("fn f(a: int,b :int ,  c: int ,d:int,) { call(x ,y,z ,); }\n")
	_, sf, builder, fileID := parseSource(t, src)

	out := NormalizeCommas(sf, builder, fileID)
	got := string(out)
	want := "fn f(a: int, b :int, c: int, d:int,) { call(x, y, z,); }\n"
	if got != want {
		t.Fatalf("NormalizeCommas mismatch:\nwant %q\ngot  %q", want, got)
	}

	_, _, builder2, fileID2 := parseSource(t, out)
	if !slices.Equal(kinds(builder, fileID), kinds(builder2, fileID2)) {
		t.Fatalf("top-level item kinds differ after NormalizeCommas")
	}
}

func kinds(b *ast.Builder, fid ast.FileID) []ast.ItemKind {
	file := b.Files.Get(fid)
	if file == nil {
		return nil
	}
	result := make([]ast.ItemKind, 0, len(file.Items))
	for _, itemID := range file.Items {
		if item := b.Items.Get(itemID); item != nil {
			result = append(result, item.Kind)
		}
	}
	return result
}
