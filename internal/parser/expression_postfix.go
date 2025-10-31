package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/token"
)

// parseCallExpr парсит вызов функции: expr(args...)
func (p *Parser) parseCallExpr(target ast.ExprID) (ast.ExprID, bool) {
	p.advance() // съедаем '('

	var args []ast.ExprID

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
			p.advance() // съедаем ','

			// Разрешаем завершающую запятую
			if p.at(token.RParen) {
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

	return p.arenas.Exprs.NewCall(finalSpan, target, args), true
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

// parseMemberExpr парсит доступ к полю: expr.field
func (p *Parser) parseMemberExpr(target ast.ExprID) (ast.ExprID, bool) {
	p.advance() // съедаем '.'

	if !p.at(token.Ident) {
		p.err(diag.SynExpectIdentifier, "expected field name after '.'")
		return ast.NoExprID, false
	}

	fieldTok := p.advance()
	fieldNameID := p.arenas.StringsInterner.Intern(fieldTok.Text)

	// Вычисляем общий span
	targetSpan := p.arenas.Exprs.Get(target).Span
	finalSpan := targetSpan.Cover(fieldTok.Span)

	return p.arenas.Exprs.NewMember(finalSpan, target, fieldNameID), true
}

// parseCastExpr парсит приведение типов: expr to Type
func (p *Parser) parseCastExpr(value ast.ExprID) (ast.ExprID, bool) {
	toTok := p.advance() // съедаем 'to'

	typeID, ok := p.parseTypePrefix()
	if !ok || typeID == ast.NoTypeID {
		if ok && typeID == ast.NoTypeID {
			p.err(diag.SynExpectType, "expected type after 'to'")
		}
		return ast.NoExprID, false
	}

	typeSpan := p.arenas.Types.Get(typeID).Span
	valueSpan := p.arenas.Exprs.Get(value).Span
	finalSpan := valueSpan.Cover(toTok.Span).Cover(typeSpan)

	return p.arenas.Exprs.NewCast(finalSpan, value, typeID), true
}
