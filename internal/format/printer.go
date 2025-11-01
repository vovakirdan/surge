package format

import (
	"errors"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
)

type Options struct {
	IndentWidth int
	UseTabs     bool
	KeepDoc     bool
	KeepLine    bool
	KeepBlock   bool
}

func (o Options) withDefaults() Options {
	if o.IndentWidth == 0 {
		o.IndentWidth = 4
	}
	// Preserve comments by default.
	o.KeepDoc = true
	o.KeepLine = true
	o.KeepBlock = true
	return o
}

type printer struct {
	builder *ast.Builder
	file    *ast.File
	writer  *Writer
	opt     Options
}

func FormatFile(sf *source.File, b *ast.Builder, fid ast.FileID, opt Options) ([]byte, error) {
	if sf == nil {
		return nil, errors.New("format: nil source file")
	}
	if b == nil {
		return nil, errors.New("format: nil builder")
	}
	if !fid.IsValid() {
		return nil, errors.New("format: invalid file id")
	}
	file := b.Files.Get(fid)
	if file == nil {
		return nil, errors.New("format: missing ast file")
	}

	opt = opt.withDefaults()
	w := NewWriter(sf, opt)
	pr := printer{
		builder: b,
		file:    file,
		writer:  w,
		opt:     opt,
	}
	pr.printFile()
	return w.Bytes(), nil
}

func (p *printer) printFile() {
	if p.file == nil {
		return
	}
	contentLen := len(p.writer.sf.Content)
	prev := 0
	for _, itemID := range p.file.Items {
		item := p.builder.Items.Get(itemID)
		if item == nil {
			continue
		}
		start := clampToContent(int(item.Span.Start), contentLen)
		if prev < start {
			p.writer.CopyRange(prev, start)
		}
		p.printItem(itemID, item)
		end := max(clampToContent(int(item.Span.End), contentLen), start)
		prev = end
	}
	if prev < contentLen {
		p.writer.CopyRange(prev, contentLen)
	}
}

func (p *printer) printItem(id ast.ItemID, item *ast.Item) {
	switch item.Kind {
	case ast.ItemFn:
		if fn, ok := p.builder.Items.Fn(id); ok && fn != nil {
			p.printFnItem(id, item, fn)
			return
		}
	case ast.ItemType:
		if typeItem, ok := p.builder.Items.Type(id); ok && typeItem != nil {
			p.printTypeItem(item, typeItem)
			return
		}
	case ast.ItemImport:
		if imp, ok := p.builder.Items.Import(id); ok && imp != nil {
			p.printImportItem(item, imp)
			return
		}
	}
	// fallback copy
	p.writer.CopySpan(item.Span)
}

// CheckRoundTrip formats the file with the given options and re-parses it,
// ensuring that top-level item kinds remain identical to the original.
func CheckRoundTrip(sf *source.File, opt Options, maxDiag int) (ok bool, msg string) {
	origBag := diag.NewBag(maxDiag)
	origBuilder, origFileID := parseOnce(sf, origBag)
	if origBuilder == nil || origBuilder.Files.Get(origFileID) == nil {
		return false, "fmt-check: initial parse failed"
	}
	if origBag.HasErrors() {
		return false, "fmt-check: initial parse has errors"
	}

	formatted, err := FormatFile(sf, origBuilder, origFileID, opt)
	if err != nil {
		return false, "fmt-check: formatter failed: " + err.Error()
	}

	fs2 := source.NewFileSetWithBase("")
	fid := fs2.AddVirtual(sf.Path, formatted)
	rebuiltFile := fs2.Get(fid)
	newBag := diag.NewBag(maxDiag)
	newBuilder, newFileID := parseOnce(rebuiltFile, newBag)
	if newBuilder == nil || newBuilder.Files.Get(newFileID) == nil || newBag.HasErrors() {
		return false, "fmt-check: reparse failed"
	}

	if !sameTopItemKinds(origBuilder, origFileID, newBuilder, newFileID) {
		return false, "fmt-check: top-level item kinds differ after round-trip"
	}

	return true, "fmt-check: OK"
}

func parseOnce(sf *source.File, bag *diag.Bag) (*ast.Builder, ast.FileID) {
	lx := lexer.New(sf, lexer.Options{Reporter: (&lexer.ReporterAdapter{Bag: bag}).Reporter()})
	builder := ast.NewBuilder(ast.Hints{}, nil)
	opts := parser.Options{Reporter: &diag.BagReporter{Bag: bag}, MaxErrors: uint(bag.Cap())}
	res := parser.ParseFile(source.NewFileSet(), lx, builder, opts)
	return builder, res.File
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

	k1 := getKinds(b1, file1)
	k2 := getKinds(b2, file2)
	if len(k1) != len(k2) {
		return false
	}
	for i := range k1 {
		if k1[i] != k2[i] {
			return false
		}
	}
	return true
}

func clampToContent(pos, length int) int {
	if pos < 0 {
		return 0
	}
	if pos > length {
		return length
	}
	return pos
}
