package diagfmt

import (
	"io"
	"surge/internal/diag"
	"surge/internal/source"
)

// JSON форматирует диагностики в JSON формат
func JSON(w io.Writer, bag *diag.Bag, fs *source.FileSet, opts JSONOpts) {
	// TODO: реализовать JSON форматирование
	_ = w
	_ = bag
	_ = fs
	_ = opts
}
