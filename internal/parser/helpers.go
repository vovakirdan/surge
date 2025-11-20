package parser

import (
	"slices"
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
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
	diagSpan := p.lastSpan.ZeroideToEnd()
	var fn func(*diag.ReportBuilder)
	if len(augment) > 0 {
		fn = augment[0]
	}
	p.emitDiagnostic(code, diag.SevError, diagSpan, msg, fn)
	return token.Token{Kind: token.Invalid, Span: diagSpan, Text: p.lx.Peek().Text}, false
}

// репортует ошибку и передает текущий спан
func (p *Parser) err(code diag.Code, msg string) {
	p.report(code, diag.SevError, p.getDiagnosticSpan(), msg)
}

func (p *Parser) report(code diag.Code, sev diag.Severity, sp source.Span, msg string) {
	p.emitDiagnostic(code, sev, sp, msg, nil)
}

func (p *Parser) emitDiagnostic(code diag.Code, sev diag.Severity, sp source.Span, msg string, augment func(*diag.ReportBuilder)) {
	if p.opts.Reporter == nil {
		return
	}
	if sev == diag.SevError {
		p.opts.CurrentErrors++
	}
	if p.opts.Enough() {
		return
	}
	if augment == nil {
		p.opts.Reporter.Report(code, sev, sp, msg, nil, nil)
		return
	}
	builder := diag.NewReportBuilder(p.opts.Reporter, sev, code, sp, msg)
	augment(builder)
	builder.Emit()
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

func (p *Parser) parseAttributes() ([]ast.Attr, source.Span, bool) {
	var attrs []ast.Attr
	var combined source.Span

	for p.at(token.At) {
		atTok := p.advance()
		attr := ast.Attr{
			Span: atTok.Span,
		}

		nameID, ok := p.parseIdent()
		if !ok {
			return attrs, combined, false
		}
		attr.Name = nameID
		attr.Span = attr.Span.Cover(p.lastSpan)

		if p.at(token.LParen) {
			op := p.advance()
			attr.Span = attr.Span.Cover(op.Span)

			if p.at(token.RParen) {
				closeTok := p.advance()
				attr.Span = attr.Span.Cover(closeTok.Span)
			} else {
				for {
					exprID, ok := p.parseExpr()
					if !ok {
						return attrs, combined, false
					}
					attr.Args = append(attr.Args, exprID)
					if expr := p.arenas.Exprs.Get(exprID); expr != nil {
						attr.Span = attr.Span.Cover(expr.Span)
					}

					if p.at(token.Comma) {
						comma := p.advance()
						attr.Span = attr.Span.Cover(comma.Span)
						continue
					}
					break
				}

				closeTok, ok := p.expect(
					token.RParen,
					diag.SynUnclosedParen,
					"expected ')' to close attribute arguments",
					func(b *diag.ReportBuilder) {
						if b == nil {
							return
						}
						fixID := fix.MakeFixID(diag.SynUnclosedParen, p.lastSpan.ZeroideToEnd())
						suggestion := fix.InsertText(
							"insert ')' to close attribute arguments",
							p.lastSpan.ZeroideToEnd(),
							")",
							"",
							fix.WithID(fixID),
							fix.WithKind(diag.FixKindRefactor),
							fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
						)
						b.WithFixSuggestion(suggestion)
						b.WithNote(p.lastSpan.ZeroideToEnd(), "insert ')' to close attribute arguments")
					},
				)
				if !ok {
					return attrs, combined, false
				}
				attr.Span = attr.Span.Cover(closeTok.Span)
			}
		}

		attrs = append(attrs, attr)
		if len(attrs) == 1 {
			combined = attr.Span
		} else {
			combined = combined.Cover(attr.Span)
		}
	}

	return attrs, combined, true
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

func isBlockRecoveryToken(k token.Kind) bool {
	switch k {
	case token.KwFn, token.KwImport, token.KwExtern, token.KwTag,
		token.KwMacro, token.KwPragma,
		token.KwElse, token.KwFinally:
		return true
	default:
		return false
	}
}

// isBlockStatementStarter reports whether a token can start a new statement inside a block.
func isBlockStatementStarter(kind token.Kind) bool {
	switch kind {
	case token.LBrace, token.KwLet, token.KwConst, token.KwReturn, token.KwIf, token.KwWhile,
		token.KwFor, token.KwBreak, token.KwContinue, token.KwCompare:
		return true
	default:
		return false
	}
}

// resyncStatement — восстановление на уровне statement.
// Пропускаем токены до тех пор, пока не встретим ';', начало нового statement, '}'
// (закрытие текущего блока) или EOF. Для корректной работы игнорируем закрывающие
// скобки внутри вложенных конструкций.
func (p *Parser) resyncStatement() {
	braceDepth := 0
	parenDepth := 0
	bracketDepth := 0

	for !p.at(token.EOF) {
		tok := p.lx.Peek()

		switch tok.Kind {
		case token.Semicolon:
			if braceDepth == 0 && parenDepth == 0 && bracketDepth == 0 {
				return
			}
		case token.LBrace:
			braceDepth++
		case token.RBrace:
			if braceDepth > 0 {
				braceDepth--
				break
			}
			if parenDepth == 0 && bracketDepth == 0 {
				if !p.at(token.EOF) {
					p.advance()
				}
				return
			}
		case token.LParen:
			parenDepth++
		case token.RParen:
			if parenDepth > 0 {
				parenDepth--
				break
			}
			if braceDepth == 0 && bracketDepth == 0 {
				if !p.at(token.EOF) {
					p.advance()
				}
				return
			}
		case token.LBracket:
			bracketDepth++
		case token.RBracket:
			if bracketDepth > 0 {
				bracketDepth--
				break
			}
			if braceDepth == 0 && parenDepth == 0 {
				if !p.at(token.EOF) {
					p.advance()
				}
				return
			}
		default:
			if braceDepth == 0 && parenDepth == 0 && bracketDepth == 0 && isBlockStatementStarter(tok.Kind) {
				return
			}
		}

		p.advance()
	}
}

// FakeError — эмулирует ошибку в указанном span
// используется для генерации диагностик для дебага
func (p *Parser) FakeError(msg string, span source.Span) {
	p.emitDiagnostic(diag.UnknownCode, diag.SevError, span, msg, nil)
}
