package diagfmt

import (
	"io"
	"surge/internal/diag"
	"surge/internal/source"
)

// Sarif форматирует диагностики в SARIF формат (v2.1.0)
func Sarif(w io.Writer, bag *diag.Bag, fs *source.FileSet, meta SarifRunMeta) {
	// TODO: реализовать SARIF форматирование
	_ = w
	_ = bag
	_ = fs
	_ = meta
}
