package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/token"
)

// parseExpr - главная точка входа для парсинга выражений
// Возвращает ExprID и флаг успеха
func (p *Parser) parseExpr() (ast.ExprID, bool) {
	return p.parseBinaryExpr(0) // минимальный приоритет = 0
}

// parseBinaryExpr реализует Pratt parsing для бинарных операторов
// minPrec - минимальный приоритет для текущего уровня
func (p *Parser) parseBinaryExpr(minPrec int) (ast.ExprID, bool) {
	// Парсим левую часть (унарные операторы + primary)
	left, ok := p.parseUnaryExpr()
	if !ok {
		return ast.NoExprID, false
	}

	// Обрабатываем бинарные операторы в цикле
	for {
		tok := p.lx.Peek()

		// Проверяем, является ли токен бинарным оператором
		prec, isRightAssoc := p.getBinaryOperatorPrec(tok.Kind)
		if prec < minPrec {
			break // приоритет слишком низкий
		}

		// Съедаем оператор
		opTok := p.advance()

		// Вычисляем приоритет для правой части
		nextMinPrec := prec + 1
		if isRightAssoc {
			nextMinPrec = prec
		}

		// Парсим правую часть
		right, ok := p.parseBinaryExpr(nextMinPrec)
		if !ok {
			p.err(diag.SynExpectExpression, "expected expression after binary operator")
			return ast.NoExprID, false
		}

		// Создаем узел бинарного выражения
		op := p.tokenKindToBinaryOp(opTok.Kind)
		leftSpan := p.arenas.Exprs.Get(left).Span
		rightSpan := p.arenas.Exprs.Get(right).Span
		finalSpan := leftSpan.Cover(rightSpan)

		left = p.arenas.Exprs.NewBinary(finalSpan, op, left, right)
	}

	return left, true
}

// parseUnaryExpr обрабатывает унарные операторы (префиксы)
func (p *Parser) parseUnaryExpr() (ast.ExprID, bool) {
	type prefixOp struct {
		op   ast.ExprUnaryOp
		span source.Span
	}

	var prefixes []prefixOp

	// Собираем все префиксы
	for {
		tok := p.lx.Peek()

		// Специальная обработка для &mut
		if tok.Kind == token.Amp {
			ampTok := p.advance() // съедаем &
			nextTok := p.lx.Peek()
			if nextTok.Kind == token.KwMut {
				// Это &mut
				mutTok := p.advance() // съедаем mut
				finalSpan := ampTok.Span.Cover(mutTok.Span)
				prefixes = append(prefixes, prefixOp{
					op:   ast.ExprUnaryRefMut,
					span: finalSpan,
				})
			} else {
				// Обычный &
				prefixes = append(prefixes, prefixOp{
					op:   ast.ExprUnaryRef,
					span: ampTok.Span,
				})
			}
			continue
		}

		// Специальная обработка для own
		if tok.Kind == token.KwOwn {
			ownTok := p.advance()
			prefixes = append(prefixes, prefixOp{
				op:   ast.ExprUnaryOwn,
				span: ownTok.Span,
			})
			continue
		}

		// Обычные унарные операторы
		if op, ok := p.getUnaryOperator(tok.Kind); ok {
			opTok := p.advance()
			prefixes = append(prefixes, prefixOp{
				op:   op,
				span: opTok.Span,
			})
		} else {
			break
		}
	}

	// Парсим базовое выражение
	expr, ok := p.parsePostfixExpr()
	if !ok {
		return ast.NoExprID, false
	}

	// Применяем префиксы справа налево
	for i := len(prefixes) - 1; i >= 0; i-- {
		exprSpan := p.arenas.Exprs.Get(expr).Span
		finalSpan := prefixes[i].span.Cover(exprSpan)
		expr = p.arenas.Exprs.NewUnary(finalSpan, prefixes[i].op, expr)
	}

	return expr, true
}

// parsePostfixExpr обрабатывает постфиксные операторы
func (p *Parser) parsePostfixExpr() (ast.ExprID, bool) {
	// Парсим базовое выражение
	expr, ok := p.parsePrimaryExpr()
	if !ok {
		return ast.NoExprID, false
	}

	// Обрабатываем постфиксы в цикле
	for {
		switch p.lx.Peek().Kind {
		case token.LParen:
			// Вызов функции: expr(args...)
			newExpr, ok := p.parseCallExpr(expr)
			if !ok {
				return ast.NoExprID, false
			}
			expr = newExpr

		case token.LBracket:
			// Индексация: expr[index]
			newExpr, ok := p.parseIndexExpr(expr)
			if !ok {
				return ast.NoExprID, false
			}
			expr = newExpr

		case token.Dot:
			// Доступ к полю: expr.field
			newExpr, ok := p.parseMemberExpr(expr)
			if !ok {
				return ast.NoExprID, false
			}
			expr = newExpr

		case token.KwTo:
			// Приведение типов: expr to Type
			newExpr, ok := p.parseCastExpr(expr)
			if !ok {
				return ast.NoExprID, false
			}
			expr = newExpr

		default:
			// Больше постфиксов нет
			return expr, true
		}
	}
}

// parsePrimaryExpr парсит основные (атомарные) выражения
func (p *Parser) parsePrimaryExpr() (ast.ExprID, bool) {
	switch p.lx.Peek().Kind {
	case token.Ident:
		// Идентификатор
		return p.parseIdentExpr()

	case token.IntLit, token.UintLit, token.FloatLit:
		// Числовые литералы
		return p.parseNumericLiteral()

	case token.StringLit:
		// Строковый литерал
		return p.parseStringLiteral()

	case token.KwTrue, token.KwFalse:
		// Булевы литералы
		return p.parseBoolLiteral()

	case token.NothingLit:
		// nothing литерал
		return p.parseNothingLiteral()

	case token.LParen:
		// Скобки: (expr)
		return p.parseParenExpr()
	
	case token.LBracket:
		// Массив: [expr]
		return p.parseArrayExpr()

	default:
		// договоримся, что обрабатываем все ошибки до этого момента
		// p.err(diag.SynExpectExpression, "expected expression")
		return ast.NoExprID, false
	}
}
