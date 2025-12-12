package lexer

import (
	"surge/internal/diag"
	"surge/internal/dialect"
	"surge/internal/source"
)

type Options struct {
	Reporter        diag.Reporter
	DialectEvidence *dialect.Evidence
}

func (lx *Lexer) SetDialectEvidence(e *dialect.Evidence) {
	if lx == nil {
		return
	}
	lx.opts.DialectEvidence = e
}

func (lx *Lexer) reportLex(code diag.Code, sev diag.Severity, sp source.Span, msg string) {
	if lx.opts.Reporter != nil {
		lx.opts.Reporter.Report(code, sev, sp, msg, nil, nil)
	}
}

func (lx *Lexer) errLex(code diag.Code, sp source.Span, msg string) {
	lx.reportLex(code, diag.SevError, sp, msg)
}
