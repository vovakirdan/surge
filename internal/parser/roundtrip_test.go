package parser_test

import (
	"bytes"
	"context"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
)

// Seeds cover key surfaces from LANGUAGE.md to exercise parser/round-trip.
var seeds = [][]byte{
	[]byte("import std/time; fn main() {}"),
	[]byte("fn f() -> i32;"),
	[]byte("fn g<T,U>(a: T, ...b: U) { return; }"),
	[]byte("type A = {x: i32, y: i32};"),
	[]byte("type U = i32 | nothing;"),
	[]byte("type T = Option(i32) | Result(nothing);"),
	[]byte("import foo::{Bar, Baz};"),
}

// Minimal property: parse -> pretty(no-op) -> parse again; must not error
// and top-level item-kind sequence must be equal.
func TestRoundTrip_NoOpPretty(t *testing.T) {
	for _, s := range seeds {
		fs := source.NewFileSetWithBase("")
		fid := fs.AddVirtual("seed.sg", s)
		sf := fs.Get(fid)

		ok, report := driver.RunFmtCheck(sf, 256)
		if !ok {
			t.Fatalf("round-trip failed for seed %q: %s", string(s), report)
		}
	}
}

// Simple fuzz: parser must not panic; round-trip must not error.
// (Go 1.18+ fuzzing can be added separately; here keep as table to be always-on.)
func TestParseDoesNotPanicOnBasicCorpus(_ *testing.T) {
	for _, s := range seeds {
		fs := source.NewFileSetWithBase("")
		fid := fs.AddVirtual("seed.sg", s)
		sf := fs.Get(fid)

		bag := diag.NewBag(256)
		lx := lexer.New(sf, lexer.Options{Reporter: (&lexer.ReporterAdapter{Bag: bag}).Reporter()})
		builder := ast.NewBuilder(ast.Hints{}, nil)
		_ = parser.ParseFile(context.Background(), fs, lx, builder, parser.Options{Reporter: &diag.BagReporter{Bag: bag}, MaxErrors: 128})

		// no hard assertion on diagnostics, only that we didn't crash
	}
}

// Structural equality helper used in RunFmtCheck is tested (implicitly) above,
// but add a tiny negative case: different top-level kinds → detect mismatch.
func TestRoundTrip_DetectsKindMismatch(t *testing.T) {
	orig := []byte("import std;")
	printed := []byte("fn main() {}") // different top-level kind on purpose

	// parse original
	fs1 := source.NewFileSetWithBase("")
	f1 := fs1.AddVirtual("x.sg", orig)
	sf1 := fs1.Get(f1)
	bag1 := diag.NewBag(64)
	lx1 := lexer.New(sf1, lexer.Options{Reporter: (&lexer.ReporterAdapter{Bag: bag1}).Reporter()})
	b1 := ast.NewBuilder(ast.Hints{}, nil)
	r1 := parser.ParseFile(context.Background(), fs1, lx1, b1, parser.Options{Reporter: &diag.BagReporter{Bag: bag1}, MaxErrors: 32})
	if r1.File == 0 {
		t.Fatalf("parse1 failed")
	}

	// parse printed (simulating pretty)
	fs2 := source.NewFileSetWithBase("")
	f2 := fs2.AddVirtual("x.sg", printed)
	sf2 := fs2.Get(f2)
	bag2 := diag.NewBag(64)
	lx2 := lexer.New(sf2, lexer.Options{Reporter: (&lexer.ReporterAdapter{Bag: bag2}).Reporter()})
	b2 := ast.NewBuilder(ast.Hints{}, nil)
	r2 := parser.ParseFile(context.Background(), fs2, lx2, b2, parser.Options{Reporter: &diag.BagReporter{Bag: bag2}, MaxErrors: 32})
	if r2.File == 0 {
		t.Fatalf("parse2 failed")
	}

	if bytes.Equal(itemKindsToBytes(b1, r1.File), itemKindsToBytes(b2, r2.File)) {
		t.Fatalf("expected kind mismatch to be detected")
	}
}

func itemKindsToBytes(b *ast.Builder, fid ast.FileID) []byte {
	f := b.Files.Get(fid)
	out := make([]byte, 0, len(f.Items))
	for _, id := range f.Items {
		if it := b.Items.Get(id); it != nil {
			out = append(out, byte(it.Kind))
		}
	}
	return out
}
