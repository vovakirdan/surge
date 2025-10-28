package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
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

	case token.KwCompare:
		return p.parseCompareExpr()

	default:
		// договоримся, что обрабатываем все ошибки до этого момента
		// p.err(diag.SynExpectExpression, "expected expression")
		return ast.NoExprID, false
	}
}

func (p *Parser) parseCompareExpr() (ast.ExprID, bool) {
	compareTok := p.advance()

	subjectExpr, ok := p.parseExpr()
	if !ok {
		return ast.NoExprID, false
	}

	insertSpan := p.lastSpan.ZeroideToEnd()
	openTok, ok := p.expect(
		token.LBrace,
		diag.SynUnexpectedToken,
		"expected '{' to start compare arms",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynUnexpectedToken, insertSpan)
			suggestion := fix.InsertText(
				"insert '{' to start compare arms",
				insertSpan,
				" {",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert '{' before compare arms")
		},
	)
	if !ok {
		return ast.NoExprID, false
	}

	var arms []ast.ExprCompareArm
	for !p.at(token.RBrace) && !p.at(token.EOF) {
		arm, armOK := p.parseCompareArm()
		if !armOK {
			p.resyncUntil(token.Semicolon, token.RBrace, token.EOF)
			if p.at(token.Semicolon) {
				p.advance()
			}
			if p.at(token.RBrace) {
				break
			}
			continue
		}
		arms = append(arms, arm)

		if p.at(token.Semicolon) {
			p.advance()
			if p.at(token.RBrace) {
				break
			}
		}
	}

	closeTok, ok := p.expect(
		token.RBrace,
		diag.SynUnclosedBrace,
		"expected '}' to close compare expression",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			insert := p.lastSpan.ZeroideToEnd()
			fixID := fix.MakeFixID(diag.SynUnclosedBrace, insert)
			suggestion := fix.InsertText(
				"insert '}' to close compare expression",
				insert,
				"}",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insert, "insert missing '}'")
		},
	)
	if !ok {
		return ast.NoExprID, false
	}

	exprSpan := compareTok.Span
	if subject := p.arenas.Exprs.Get(subjectExpr); subject != nil {
		exprSpan = exprSpan.Cover(subject.Span)
	}
	exprSpan = exprSpan.Cover(openTok.Span)
	exprSpan = exprSpan.Cover(closeTok.Span)
	exprID := p.arenas.Exprs.NewCompare(exprSpan, subjectExpr, arms)
	return exprID, true
}

func (p *Parser) parseCompareArm() (ast.ExprCompareArm, bool) {
	arm := ast.ExprCompareArm{}

	if p.at(token.KwFinally) {
		finallyTok := p.advance()
		arm.IsFinally = true
		arm.PatternSpan = finallyTok.Span
		if p.at(token.KwIf) {
			ifTok := p.advance()
			p.emitDiagnostic(
				diag.SynUnexpectedToken,
				diag.SevError,
				ifTok.Span,
				"'if' guard is not allowed on 'finally' arm",
				nil,
			)
			// best-effort consume guard expression to avoid cascading errors
			p.parseExpr()
		}
	} else {
		patternStart := p.lx.Peek().Span
		patternExpr, ok := p.parseExpr()
		if !ok {
			return arm, false
		}
		arm.Pattern = patternExpr
		if node := p.arenas.Exprs.Get(patternExpr); node != nil {
			arm.PatternSpan = patternStart.Cover(node.Span)
		} else {
			arm.PatternSpan = patternStart
		}

		if p.at(token.KwIf) {
			p.advance()
			guardExpr, ok := p.parseExpr()
			if !ok {
				return arm, false
			}
			arm.Guard = guardExpr
		}
	}

	if _, ok := p.expect(token.FatArrow, diag.SynUnexpectedToken, "expected '=>' after compare arm pattern"); !ok {
		return arm, false
	}

	resultExpr, ok := p.parseExpr()
	if !ok {
		return arm, false
	}
	arm.Result = resultExpr
	return arm, true
}
