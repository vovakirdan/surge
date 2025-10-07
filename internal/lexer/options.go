package lexer

import (
	"surge/internal/diag"
	"surge/internal/source"
)

type Options struct {
	Reporter diag.Reporter
}

func (lx *Lexer) reportLex(code diag.Code, sev diag.Severity, sp source.Span, msg string) {
	if lx.opts.Reporter != nil {
		lx.opts.Reporter.Report(code, sev, sp, msg, nil, nil)
	}
}

func (lx *Lexer) errLex(code diag.Code, sp source.Span, msg string) {
	lx.reportLex(code, diag.SevError, sp, msg)
}

func (lx *Lexer) warnLex(code diag.Code, sp source.Span, msg string) {
	lx.reportLex(code, diag.SevWarning, sp, msg)
}

func (lx *Lexer) infoLex(code diag.Code, sp source.Span, msg string) {
	lx.reportLex(code, diag.SevInfo, sp, msg)
}
