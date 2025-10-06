package lexer

import (
	"surge/internal/source"
)

// Reporter — тонкий интерфейс, чтобы не тянуть diag сюда.
// Лексер **только вызывает** его с параметрами; форматирует diag внешний слой.
type Reporter interface {
	Report(kind string, span source.Span, msg string)
}

type Options struct {
	Reporter Reporter // может быть nil — тогда ошибки игнорируем (но продолжаем лексить)
}

func (lx *Lexer) report(kind string, sp source.Span, msg string) {
	if lx.opts.Reporter != nil {
		lx.opts.Reporter.Report(kind, sp, msg)
	}
}
