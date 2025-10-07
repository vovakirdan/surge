package diagfmt

import (
	"io"
	"surge/internal/diag"
	"surge/internal/source"
)

// Pretty форматирует диагностики в человекочитаемый вид.
// Идёт по bag.Items() (ожидается bag.Sort() заранее).
// Для каждого diag печатает:
// <path>:<line>:<col>: <SEV> <CODE>: <Message>
// затем контекст строки с подчёркиванием ^~~~ по Span, затем Notes с аналогичным форматом.
// Цвет включается опцией.
func Pretty(w io.Writer, bag *diag.Bag, fs *source.FileSet, opts PrettyOpts) {
	// TODO: реализовать форматирование
	_ = w
	_ = bag
	_ = fs
	_ = opts
}
