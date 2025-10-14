package parser

import (
	"slices"
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

// currentErrorSpan — возвращает оптимальный span для ошибок expect
// Если Peek().Kind == EOF, возвращает позицию сразу после lastSpan
func (p *Parser) currentErrorSpan() source.Span {
	peek := p.lx.Peek()
	if peek.Kind == token.EOF {
		return source.Span{
			File:  p.lastSpan.File,
			Start: p.lastSpan.End,
			End:   p.lastSpan.End,
		}
	}
	return peek.Span
}

// expect — ожидаем конкретный токен. Если нет — репортим и возвращаем (invalid,false).
func (p *Parser) expect(k token.Kind, code diag.Code, msg string, augment ...func(*diag.ReportBuilder)) (token.Token, bool) {
	if p.at(k) {
		return p.advance(), true
	}
	// Используем currentErrorSpan для более точной диагностики
	diagSpan := p.currentErrorSpan()
	var fn func(*diag.ReportBuilder)
	if len(augment) > 0 {
		fn = augment[0]
	}
	p.emitDiagnostic(code, diag.SevError, diagSpan, msg, fn)
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
	return p.emitDiagnostic(code, sev, sp, msg, nil)
}

func (p *Parser) emitDiagnostic(code diag.Code, sev diag.Severity, sp source.Span, msg string, augment func(*diag.ReportBuilder)) bool {
	if p.opts.Reporter == nil {
		return false
	}
	if sev == diag.SevError {
		p.opts.CurrentErrors++
	}
	if p.opts.Enough() {
		return false
	}
	if augment == nil {
		p.opts.Reporter.Report(code, sev, sp, msg, nil, nil)
		return true
	}
	builder := diag.NewReportBuilder(p.opts.Reporter, sev, code, sp, msg)
	augment(builder)
	builder.Emit()
	return true
}

// resyncUntil — consume tokens until Peek() matches any stop token or EOF.
// Stop token остаётся на входе (не съедаем).
func (p *Parser) resyncUntil(stop ...token.Kind) {
	for !p.at(token.EOF) {
		peek := p.lx.Peek().Kind
		if slices.Contains(stop, peek) {
			return
		}
		p.advance() // съедаем текущий токен и продолжаем
	}
}

// resyncUntilIncluding — то же, но съедает найденный стоп-токен
// (полезно, чтобы сбросить ;/}).
func (p *Parser) resyncUntilIncluding(stop ...token.Kind) {
	p.resyncUntil(stop...)
	if !p.at(token.EOF) {
		peek := p.lx.Peek().Kind
		if slices.Contains(stop, peek) {
			p.advance() // съедаем найденный стоп-токен
			return
		}
	}
}

// resyncIfStuck — защититься от бесконечного цикла:
// если Peek().Span == lastSpanEnd, force advance().
func (p *Parser) resyncIfStuck(max int) {
	for range max {
		if p.at(token.EOF) {
			return
		}
		peek := p.lx.Peek()
		if peek.Span.Start == p.lastSpan.End {
			p.advance() // force advance if stuck
		} else {
			return
		}
	}
}

// Specialized wrapper functions for different grammar constructs

// resyncImportGroup — восстановление внутри группы импорта
// до '}', ';', или EOF, а потом съедает '}' если найден
func (p *Parser) resyncImportGroup() {
	p.resyncUntil(token.RBrace, token.Semicolon, token.EOF)
	if p.at(token.RBrace) {
		p.advance() // съедаем найденную '}'
	}
}

// resyncStatement — восстановление на уровне statement
// до ';', '}', или EOF
func (p *Parser) resyncStatement() {
	p.resyncUntil(token.Semicolon, token.RBrace, token.EOF)
}

// resyncExpression — восстановление на уровне выражения
// до ';', ',', закрывающих скобок, или EOF
func (p *Parser) resyncExpression() {
	p.resyncUntil(token.Semicolon, token.Comma, token.RBrace, token.RParen, token.RBracket, token.EOF)
}
