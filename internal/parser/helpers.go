package parser

import (
	"surge/internal/diag"
	_ "surge/internal/lexer"
	"surge/internal/source"
	"surge/internal/token"
)

// expect — ожидаем конкретный токен. Если нет — репортим и возвращаем (invalid,false).
func (p *Parser) expect(k token.Kind, code diag.Code, msg string) (token.Token, bool) {
	if p.at(k) {
		return p.lx.Next(), true
	}
	p.report(code, diag.SevError, p.lx.Peek().Span, msg)
	return token.Token{Kind: token.Invalid, Span: p.lx.Peek().Span, Text: p.lx.Peek().Text}, false
}

// want - желаем увидеть токен, но кидаем warning, если нет
func (p *Parser) want(k token.Kind, code diag.Code, msg string) (token.Token, bool) {
	if p.at(k) {
		return p.lx.Next(), true
	}
	p.report(code, diag.SevWarning, p.lx.Peek().Span, msg)
	return p.lx.Peek(), false
}

// репортует ошибку и передает текущий спан
func (p *Parser) err(code diag.Code, msg string) bool {
	return p.report(code, diag.SevError, p.lx.Peek().Span, msg)
}

// репортует warning и передает текущий спан
func (p *Parser) warn(code diag.Code, msg string) bool {
	return p.report(code, diag.SevWarning, p.lx.Peek().Span, msg)
}

// репортует info и передает текущий спан
func (p *Parser) info(code diag.Code, msg string) bool {
	return p.report(code, diag.SevInfo, p.lx.Peek().Span, msg)
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
