package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

// parseIdentExpr парсит выражение-идентификатор
func (p *Parser) parseIdentExpr() (ast.ExprID, bool) {
	tok := p.advance()
	if tok.Kind != token.Ident {
		p.err(diag.SynExpectIdentifier, "expected identifier")
		return ast.NoExprID, false
	}

	nameID := p.arenas.StringsInterner.Intern(tok.Text)
	return p.arenas.Exprs.NewIdent(tok.Span, nameID), true
}

// parseNumericLiteral парсит числовые литералы
func (p *Parser) parseNumericLiteral() (ast.ExprID, bool) {
	tok := p.advance()

	var kind ast.ExprLitKind
	switch tok.Kind {
	case token.IntLit:
		kind = ast.ExprLitInt
	case token.UintLit:
		kind = ast.ExprLitUint
	case token.FloatLit:
		kind = ast.ExprLitFloat
	default:
		p.err(diag.SynUnexpectedToken, "expected numeric literal")
		return ast.NoExprID, false
	}

	// Сохраняем сырое значение для sema
	valueID := p.arenas.StringsInterner.Intern(tok.Text)
	return p.arenas.Exprs.NewLiteral(tok.Span, kind, valueID), true
}

// parseStringLiteral парсит строковые литералы
func (p *Parser) parseStringLiteral() (ast.ExprID, bool) {
	tok := p.advance()
	if tok.Kind != token.StringLit {
		p.err(diag.SynUnexpectedToken, "expected string literal")
		return ast.NoExprID, false
	}

	valueID := p.arenas.StringsInterner.Intern(tok.Text)
	return p.arenas.Exprs.NewLiteral(tok.Span, ast.ExprLitString, valueID), true
}

// parseBoolLiteral парсит булевы литералы
func (p *Parser) parseBoolLiteral() (ast.ExprID, bool) {
	tok := p.advance()

	var kind ast.ExprLitKind
	switch tok.Kind {
	case token.KwTrue:
		kind = ast.ExprLitTrue
	case token.KwFalse:
		kind = ast.ExprLitFalse
	default:
		p.err(diag.SynUnexpectedToken, "expected boolean literal")
		return ast.NoExprID, false
	}

	valueID := p.arenas.StringsInterner.Intern(tok.Text)
	return p.arenas.Exprs.NewLiteral(tok.Span, kind, valueID), true
}

// parseNothingLiteral парсит nothing литерал
func (p *Parser) parseNothingLiteral() (ast.ExprID, bool) {
	tok := p.advance()
	if tok.Kind != token.NothingLit {
		p.err(diag.SynUnexpectedToken, "expected 'nothing'")
		return ast.NoExprID, false
	}

	valueID := p.arenas.StringsInterner.Intern(tok.Text)
	return p.arenas.Exprs.NewLiteral(tok.Span, ast.ExprLitNothing, valueID), true
}

// parseParenExpr парсит выражения в скобках - может быть группировкой или tuple
func (p *Parser) parseParenExpr() (ast.ExprID, bool) {
	openTok := p.advance() // съедаем '('

	commas := make([]source.Span, 0, 2)
	var trailing bool

	// Проверяем на пустой tuple
	if p.at(token.RParen) {
		closeTok := p.advance()
		finalSpan := openTok.Span.Cover(closeTok.Span)
		return p.arenas.Exprs.NewTuple(finalSpan, []ast.ExprID{}, commas, trailing), true
	}

	// Парсим первое выражение
	first, ok := p.parseExpr()
	if !ok {
		return ast.NoExprID, false
	}

	// Проверяем есть ли запятая - если да, то это tuple
	if p.at(token.Comma) {
		var elements []ast.ExprID
		elements = append(elements, first)

		for p.at(token.Comma) {
			commaTok := p.advance() // съедаем ','
			commas = append(commas, commaTok.Span)

			// Разрешаем завершающую запятую
			if p.at(token.RParen) {
				trailing = true
				break
			}

			var expr ast.ExprID
			expr, ok = p.parseExpr()
			if !ok {
				return ast.NoExprID, false
			}
			elements = append(elements, expr)
		}

		var closeTok token.Token
		closeTok, ok = p.expect(token.RParen, diag.SynUnclosedParen, "expected ')' after tuple elements", nil)
		if !ok {
			return ast.NoExprID, false
		}

		finalSpan := openTok.Span.Cover(closeTok.Span)
		return p.arenas.Exprs.NewTuple(finalSpan, elements, commas, trailing), true
	}

	// Не tuple - обычная группировка
	closeTok, ok := p.expect(token.RParen, diag.SynUnclosedParen, "expected ')' after expression", nil)
	if !ok {
		return ast.NoExprID, false
	}

	finalSpan := openTok.Span.Cover(closeTok.Span)
	return p.arenas.Exprs.NewGroup(finalSpan, first), true
}

func (p *Parser) parseArrayExpr() (ast.ExprID, bool) {
	openTok := p.advance() // съедаем '['

	// проверяем на пустой массив

	if p.at(token.RBracket) {
		closeTok := p.advance()
		finalSpan := openTok.Span.Cover(closeTok.Span)
		return p.arenas.Exprs.NewArray(finalSpan, []ast.ExprID{}, nil, false), true
	}

	// Парсим первое выражение
	beforeErrors := p.opts.CurrentErrors
	first, ok := p.parseExpr()
	if !ok {
		if p.opts.CurrentErrors == beforeErrors {
			errSpan := p.currentErrorSpan()
			p.emitDiagnostic(
				diag.SynExpectExpression,
				diag.SevError,
				errSpan,
				"expected expression in array literal",
				nil,
			)
		}
		p.resyncUntil(token.RBracket, token.Semicolon)
		// попытаемся продолжить, чтобы зафиксировать ошибку закрывающей скобки
		return ast.NoExprID, false
	}

	// парсим элементы массива циклом
	var elements []ast.ExprID
	elements = append(elements, first)
	encounteredError := false
	commas := make([]source.Span, 0, 2)
	var trailing bool
	for p.at(token.Comma) {
		commaTok := p.advance() // съедаем ','
		commas = append(commas, commaTok.Span)
		if p.at(token.RBracket) {
			trailing = true
			break
		}
		beforeErrors = p.opts.CurrentErrors
		var expr ast.ExprID
		expr, ok = p.parseExpr()
		if !ok {
			if p.opts.CurrentErrors == beforeErrors {
				errSpan := p.currentErrorSpan()
				p.emitDiagnostic(
					diag.SynExpectExpression,
					diag.SevError,
					errSpan,
					"expected expression after ',' in array literal",
					nil,
				)
			}
			p.resyncUntil(token.RBracket, token.Semicolon, token.Comma)
			encounteredError = true
			break
		}
		elements = append(elements, expr)
	}

	closeTok, ok := p.expect(token.RBracket, diag.SynUnclosedSquareBracket, "expected ']' after array elements", func(b *diag.ReportBuilder) {
		if b == nil {
			return
		}
		insertPos := p.currentErrorSpan().ZeroideToStart()
		fixID := fix.MakeFixID(diag.SynUnclosedSquareBracket, insertPos)
		suggestion := fix.InsertText(
			"insert ']' to close array literal",
			insertPos,
			"]",
			"",
			fix.WithID(fixID),
			fix.WithKind(diag.FixKindRefactor),
			fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			fix.Preferred(),
		)
		b.WithFixSuggestion(suggestion)
		b.WithNote(insertPos, "insert missing closing bracket")
	})
	if !ok {
		return ast.NoExprID, false
	}

	if encounteredError {
		return ast.NoExprID, false
	}

	finalSpan := openTok.Span.Cover(closeTok.Span)
	return p.arenas.Exprs.NewArray(finalSpan, elements, commas, trailing), true
}
