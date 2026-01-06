package lexer

import (
	"surge/internal/diag"
	"surge/internal/dialect"
)

// Options configures the lexer.
type Options struct {
	Reporter        diag.Reporter
	DialectEvidence *dialect.Evidence
}

// SetDialectEvidence sets the container for collecting foreign dialect signals.
func (lx *Lexer) SetDialectEvidence(e *dialect.Evidence) {
	if lx == nil {
		return
	}
	lx.opts.DialectEvidence = e
}
