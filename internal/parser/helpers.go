package parser

import (
	"surge/internal/diag"
	_ "surge/internal/lexer"
	"surge/internal/source"
	"surge/internal/token"
)

// advance — съедает следующий токен и обновляет lastSpan
func (p *Parser) advance() token.Token {
	tok := p.lx.Next()
	if tok.Kind != token.EOF && tok.Kind != token.Invalid {
		p.lastSpan = tok.Span
	}
	return tok
}

// getDiagnosticSpan — возвращает лучший span для диагностики
// Если текущий токен EOF или Invalid с нулевой длиной, используем позицию после lastSpan
func (p *Parser) getDiagnosticSpan() source.Span {
	peek := p.lx.Peek()
	// Если peek это EOF или Invalid с нулевой длиной span, используем позицию после lastSpan
	if (peek.Kind == token.EOF || peek.Kind == token.Invalid) && peek.Span.Start == peek.Span.End && peek.Span.Start == 0 {
		if p.lastSpan.End > 0 {
			return source.Span{
				File:  p.lastSpan.File,
				Start: p.lastSpan.End,
				End:   p.lastSpan.End,
			}
		}
	}
	return peek.Span
}

// expect — ожидаем конкретный токен. Если нет — репортим и возвращаем (invalid,false).
func (p *Parser) expect(k token.Kind, code diag.Code, msg string) (token.Token, bool) {
	if p.at(k) {
		return p.advance(), true
	}
	// Используем лучший span для диагностики
	diagSpan := p.getDiagnosticSpan()
	p.report(code, diag.SevError, diagSpan, msg)
	return token.Token{Kind: token.Invalid, Span: diagSpan, Text: p.lx.Peek().Text}, false
}

// want - желаем увидеть токен, но кидаем warning, если нет
func (p *Parser) want(k token.Kind, code diag.Code, msg string) (token.Token, bool) {
	if p.at(k) {
		return p.advance(), true
	}
	diagSpan := p.getDiagnosticSpan()
	p.report(code, diag.SevWarning, diagSpan, msg)
	return p.lx.Peek(), false
}

// репортует ошибку и передает текущий спан
func (p *Parser) err(code diag.Code, msg string) bool {
	return p.report(code, diag.SevError, p.getDiagnosticSpan(), msg)
}

// репортует warning и передает текущий спан
func (p *Parser) warn(code diag.Code, msg string) bool {
	return p.report(code, diag.SevWarning, p.getDiagnosticSpan(), msg)
}

// репортует info и передает текущий спан
func (p *Parser) info(code diag.Code, msg string) bool {
	return p.report(code, diag.SevInfo, p.getDiagnosticSpan(), msg)
}

func (p *Parser) report(code diag.Code, sev diag.Severity, sp source.Span, msg string) bool {
	if p.opts.Reporter != nil {
		if sev == diag.SevError {
			p.opts.CurrentErrors++
		}
		if !p.opts.Enough() {
			p.opts.Reporter.Report(code, sev, sp, msg, nil, nil)
			return true
		}
		return false // достигли максимального количества ошибок
	}
	return false // нет reporter - ничего не записали
}
