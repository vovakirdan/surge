package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/token"
)

// parseCallExpr парсит вызов функции: expr(args...)
func (p *Parser) parseCallExpr(target ast.ExprID, typeArgs []ast.TypeID) (ast.ExprID, bool) {
	p.advance() // съедаем '('

	var args []ast.ExprID
	var commas []source.Span
	var trailing bool

	// Парсим аргументы
	if !p.at(token.RParen) {
		for {
			arg, ok := p.parseExpr()
			if !ok {
				// Ошибка парсинга аргумента - восстанавливаемся
				p.resyncUntil(token.RParen, token.Comma, token.Semicolon, token.LBrace)
				if p.at(token.RParen) {
					p.advance()
				}
				return ast.NoExprID, false
			}
			// Проверяем spread-оператор: expr...
			if p.at(token.DotDotDot) {
				spreadTok := p.advance()
				argSpan := p.arenas.Exprs.Get(arg).Span
				arg = p.arenas.Exprs.NewSpread(argSpan.Cover(spreadTok.Span), arg)
			}

			args = append(args, arg)

			if !p.at(token.Comma) {
				break
			}
			commaTok := p.advance() // съедаем ','
			commas = append(commas, commaTok.Span)

			// Разрешаем завершающую запятую
			if p.at(token.RParen) {
				trailing = true
				break
			}
		}
	}

	closeTok, ok := p.expect(token.RParen, diag.SynUnclosedParen, "expected ')' after function arguments", nil)
	if !ok {
		// Восстанавливаемся после ошибки ожидания ')'
		p.resyncUntil(token.Semicolon, token.LBrace, token.RBrace, token.EOF)
		return ast.NoExprID, false
	}

	// Вычисляем общий span
	targetSpan := p.arenas.Exprs.Get(target).Span
	finalSpan := targetSpan.Cover(closeTok.Span)

	return p.arenas.Exprs.NewCall(finalSpan, target, args, typeArgs, commas, trailing), true
}

// parseIndexExpr парсит индексацию: expr[index]
func (p *Parser) parseIndexExpr(target ast.ExprID) (ast.ExprID, bool) {
	p.advance() // съедаем '['

	index, ok := p.parseExpr()
	if !ok {
		p.err(diag.SynExpectExpression, "expected index expression")
		return ast.NoExprID, false
	}

	closeTok, ok := p.expect(token.RBracket, diag.SynExpectRightBracket, "expected ']' after index", nil)
	if !ok {
		return ast.NoExprID, false
	}

	// Вычисляем общий span
	targetSpan := p.arenas.Exprs.Get(target).Span
	finalSpan := targetSpan.Cover(closeTok.Span)

	return p.arenas.Exprs.NewIndex(finalSpan, target, index), true
}

// parseMemberExpr парсит доступ к полю или .await: expr.field / expr.await
func (p *Parser) parseMemberExpr(target ast.ExprID) (ast.ExprID, bool) {
	p.advance() // съедаем '.'

	switch p.lx.Peek().Kind {
	case token.KwAwait, token.Ident:
		fieldTok := p.advance()
		fieldName := fieldTok.Text
		if fieldTok.Kind == token.KwAwait {
			fieldName = "await"
		}
		fieldNameID := p.arenas.StringsInterner.Intern(fieldName)

		targetSpan := p.arenas.Exprs.Get(target).Span
		finalSpan := targetSpan.Cover(fieldTok.Span)

		return p.arenas.Exprs.NewMember(finalSpan, target, fieldNameID), true

	default:
		p.err(diag.SynExpectIdentifier, "expected field name or 'await' after '.'")
		return ast.NoExprID, false
	}
}

// parseCastExpr парсит приведение типов: expr to Type
func (p *Parser) parseCastExpr(value ast.ExprID) (ast.ExprID, bool) {
	toTok := p.advance() // съедаем 'to'

	if startsTypeExpr(p.lx.Peek().Kind) {
		typeID, ok := p.parseTypePrefix()
		if !ok || typeID == ast.NoTypeID {
			return ast.NoExprID, false
		}
		typeSpan := p.arenas.Types.Get(typeID).Span
		valueSpan := p.arenas.Exprs.Get(value).Span
		finalSpan := valueSpan.Cover(toTok.Span).Cover(typeSpan)
		return p.arenas.Exprs.NewCast(finalSpan, value, typeID, ast.NoExprID), true
	}

	rawExpr, ok := p.parseUnaryExpr()
	if !ok {
		return ast.NoExprID, false
	}
	valueSpan := p.arenas.Exprs.Get(value).Span
	rawSpan := p.arenas.Exprs.Get(rawExpr).Span
	finalSpan := valueSpan.Cover(toTok.Span).Cover(rawSpan)
	return p.arenas.Exprs.NewCast(finalSpan, value, ast.NoTypeID, rawExpr), true
}

func startsTypeExpr(kind token.Kind) bool {
	switch kind {
	case token.Ident, token.NothingLit, token.LParen, token.KwFn, token.KwOwn,
		token.Amp, token.AndAnd, token.Star:
		return true
	default:
		return false
	}
}
