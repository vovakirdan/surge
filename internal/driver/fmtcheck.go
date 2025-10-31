package driver

import (
	"bytes"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
)

// PrettyNoop returns the original file bytes. This is a placeholder for a real pretty-printer.
// It still allows round-trip scaffolding (parse -> print -> parse) without panics.
func PrettyNoop(sf *source.File) []byte {
	// preserve original as-is (already LF-normalized and without BOM in FileSet)
	return append([]byte(nil), sf.Content...)
}

// RunFmtCheck parses the file, prints code (currently no-op), re-parses, and
// verifies “coarse” structural equality (sequence of top-level item kinds).
// It returns (ok, report string). 'ok' means structure matched and no parser errors.
func RunFmtCheck(sf *source.File, maxDiagnostics int) (success bool, msg string) {
	// 1) parse original
	firstBag := diag.NewBag(maxDiagnostics)
	firstBuilder, firstFileID := parseOnce(sf, firstBag)
	if firstBuilder == nil {
		return false, "fmt-check: initial parse failed"
	}
	if hasErrors(firstBag) {
		return false, "fmt-check: initial parse has errors"
	}

	// 2) pretty print (no-op) -> bytes
	out := PrettyNoop(sf)

	// 3) reparse from bytes
	fs2 := source.NewFileSetWithBase("")
	f2 := fs2.AddVirtual(sf.Path, out)
	secondBag := diag.NewBag(maxDiagnostics)
	secondBuilder, secondFileID := parseOnce(fs2.Get(f2), secondBag)
	if secondBuilder == nil || hasErrors(secondBag) {
		return false, "fmt-check: reparse failed"
	}

	// 4) compare structure (kinds of top-level items)
	if !sameTopItemKinds(firstBuilder, firstFileID, secondBuilder, secondFileID) {
		return false, "fmt-check: top-level item kinds differ after round-trip"
	}

	return true, "fmt-check: OK"
}

func parseOnce(sf *source.File, bag *diag.Bag) (*ast.Builder, ast.FileID) {
	lx := lexer.New(sf, lexer.Options{Reporter: (&lexer.ReporterAdapter{Bag: bag}).Reporter()})
	builder := ast.NewBuilder(ast.Hints{}, nil)

	var maxErr uint
	if m, err := safecast.Conv[uint](bag.Cap()); err == nil {
		maxErr = m
	}

	opts := parser.Options{Reporter: &diag.BagReporter{Bag: bag}, MaxErrors: maxErr}
	res := parser.ParseFile(source.NewFileSet(), lx, builder, opts) // fs arg is unused by parser except for spans mapping
	return builder, res.File
}

func hasErrors(b *diag.Bag) bool {
	for _, d := range b.Items() {
		if d.Severity >= diag.SevError {
			return true
		}
	}
	return false
}

func sameTopItemKinds(b1 *ast.Builder, f1 ast.FileID, b2 *ast.Builder, f2 ast.FileID) bool {
	file1 := b1.Files.Get(f1)
	file2 := b2.Files.Get(f2)
	if file1 == nil || file2 == nil {
		return false
	}
	getKinds := func(b *ast.Builder, f *ast.File) []ast.ItemKind {
		kinds := make([]ast.ItemKind, 0, len(f.Items))
		for _, id := range f.Items {
			if it := b.Items.Get(id); it != nil {
				kinds = append(kinds, it.Kind)
			}
		}
		return kinds
	}
	k1, k2 := getKinds(b1, file1), getKinds(b2, file2)
	return bytes.Equal(itemKindsToBytes(k1), itemKindsToBytes(k2))
}

func itemKindsToBytes(kinds []ast.ItemKind) []byte {
	buf := make([]byte, len(kinds))
	for i, k := range kinds {
		buf[i] = byte(k)
	}
	return buf
}
