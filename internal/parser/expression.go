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

		if tok.Kind == token.FatArrow && p.allowFatArrow == 0 {
			p.emitDiagnostic(
				diag.SynFatArrowOutsideParallel,
				diag.SevError,
				tok.Span,
				"'=>' is only allowed inside parallel expressions or compare arms",
				nil,
			)
			p.advance()
			return ast.NoExprID, false
		}

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
	type prefixKind uint8
	const (
		prefixUnary prefixKind = iota
		prefixSpawn
	)

	type prefixOp struct {
		kind  prefixKind
		span  source.Span
		unary ast.ExprUnaryOp
	}

	var prefixes []prefixOp

	// Собираем все префиксы
	for {
		tok := p.lx.Peek()

		if tok.Kind == token.KwSpawn {
			spawnTok := p.advance()
			prefixes = append(prefixes, prefixOp{
				kind: prefixSpawn,
				span: spawnTok.Span,
			})
			continue
		}

		// Специальная обработка для &mut
		if tok.Kind == token.Amp {
			ampTok := p.advance() // съедаем &
			nextTok := p.lx.Peek()
			if nextTok.Kind == token.KwMut {
				// Это &mut
				mutTok := p.advance() // съедаем mut
				finalSpan := ampTok.Span.Cover(mutTok.Span)
				prefixes = append(prefixes, prefixOp{
					kind:  prefixUnary,
					span:  finalSpan,
					unary: ast.ExprUnaryRefMut,
				})
			} else {
				// Обычный &
				prefixes = append(prefixes, prefixOp{
					kind:  prefixUnary,
					span:  ampTok.Span,
					unary: ast.ExprUnaryRef,
				})
			}
			continue
		}

		// Специальная обработка для own
		if tok.Kind == token.KwOwn {
			ownTok := p.advance()
			prefixes = append(prefixes, prefixOp{
				kind:  prefixUnary,
				span:  ownTok.Span,
				unary: ast.ExprUnaryOwn,
			})
			continue
		}

		// Обычные унарные операторы
		if op, ok := p.getUnaryOperator(tok.Kind); ok {
			opTok := p.advance()
			prefixes = append(prefixes, prefixOp{
				kind:  prefixUnary,
				span:  opTok.Span,
				unary: op,
			})
			continue
		}

		break
	}

	// Парсим базовое выражение
	expr, ok := p.parsePostfixExpr()
	if !ok {
		return ast.NoExprID, false
	}

	// Применяем префиксы справа налево
	for i := len(prefixes) - 1; i >= 0; i-- {
		exprSpan := prefixes[i].span
		if node := p.arenas.Exprs.Get(expr); node != nil {
			exprSpan = prefixes[i].span.Cover(node.Span)
		}
		switch prefixes[i].kind {
		case prefixUnary:
			expr = p.arenas.Exprs.NewUnary(exprSpan, prefixes[i].unary, expr)
		case prefixSpawn:
			expr = p.arenas.Exprs.NewSpawn(exprSpan, expr)
		}
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
		case token.Lt:
			if ident, ok := p.arenas.Exprs.Ident(expr); !ok || ident == nil || p.arenas.StringsInterner.MustLookup(ident.Name) != "default" {
				return expr, true
			}
			typeArgs, ok := p.parseTypeArgs()
			if !ok {
				return ast.NoExprID, false
			}
			if !p.at(token.LParen) {
				p.err(diag.SynUnexpectedToken, "expected '(' after type arguments")
				return ast.NoExprID, false
			}
			newExpr, ok := p.parseCallExpr(expr, typeArgs)
			if !ok {
				return ast.NoExprID, false
			}
			expr = newExpr
		case token.LParen:
			// Вызов функции: expr(args...)
			newExpr, ok := p.parseCallExpr(expr, nil)
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
		case token.Colon:
			if p.suspendColonCast > 0 {
				return expr, true
			}
			colonTok := p.advance()
			if !startsTypeExpr(p.lx.Peek().Kind) {
				p.err(diag.SynExpectType, "expected type after ':'")
				return ast.NoExprID, false
			}
			typeID, ok := p.parseTypePrefix()
			if !ok || typeID == ast.NoTypeID {
				return ast.NoExprID, false
			}
			typeSpan := p.arenas.Types.Get(typeID).Span
			exprSpan := p.arenas.Exprs.Get(expr).Span
			finalSpan := exprSpan.Cover(colonTok.Span).Cover(typeSpan)
			expr = p.arenas.Exprs.NewCast(finalSpan, expr, typeID, ast.NoExprID)

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
		return p.parseIdentOrStructLiteral()

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

	case token.KwParallel:
		return p.parseParallelExpr()

	case token.KwAsync:
		return p.parseAsyncExpr()

	case token.LBrace:
		return p.parseStructLiteral(ast.NoTypeID, source.Span{})

	default:
		// договоримся, что обрабатываем все ошибки до этого момента
		// p.err(diag.SynExpectExpression, "expected expression")
		return ast.NoExprID, false
	}
}

func (p *Parser) parseCompareExpr() (ast.ExprID, bool) {
	p.allowFatArrow++
	defer func() { p.allowFatArrow-- }()
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

func (p *Parser) parseParallelExpr() (ast.ExprID, bool) {
	p.allowFatArrow++
	defer func() { p.allowFatArrow-- }()
	parallelTok := p.advance()

	var modeTok token.Token
	switch p.lx.Peek().Kind {
	case token.KwMap:
		modeTok = p.advance()
	case token.KwReduce:
		modeTok = p.advance()
	default:
		p.emitDiagnostic(
			diag.SynUnexpectedToken,
			diag.SevError,
			p.lx.Peek().Span,
			"expected 'map' or 'reduce' after 'parallel'",
			nil,
		)
		return ast.NoExprID, false
	}

	iterableExpr, ok := p.parseExpr()
	if !ok {
		return ast.NoExprID, false
	}

	withTok, ok := p.expect(token.KwWith, diag.SynUnexpectedToken, "expected 'with' after parallel iterable")
	if !ok {
		return ast.NoExprID, false
	}

	var (
		initExpr ast.ExprID
		commaTok token.Token
	)

	if modeTok.Kind == token.KwReduce {
		initExpr, ok = p.parseExpr()
		if !ok {
			return ast.NoExprID, false
		}
		commaTok, ok = p.expect(token.Comma, diag.SynUnexpectedToken, "expected ',' between reduce initializer and argument list")
		if !ok {
			return ast.NoExprID, false
		}
	}

	args, argsSpan, ok := p.parseParallelArgList()
	if !ok {
		return ast.NoExprID, false
	}

	arrowTok, ok := p.expect(token.FatArrow, diag.SynUnexpectedToken, "expected '=>' after parallel argument list")
	if !ok {
		return ast.NoExprID, false
	}

	bodyExpr, ok := p.parseExpr()
	if !ok {
		return ast.NoExprID, false
	}

	span := parallelTok.Span.Cover(modeTok.Span)
	if node := p.arenas.Exprs.Get(iterableExpr); node != nil {
		span = span.Cover(node.Span)
	}
	span = span.Cover(withTok.Span)
	if initExpr.IsValid() {
		if node := p.arenas.Exprs.Get(initExpr); node != nil {
			span = span.Cover(node.Span)
		}
		if commaTok.Kind != token.Invalid {
			span = span.Cover(commaTok.Span)
		}
	}
	span = span.Cover(argsSpan)
	span = span.Cover(arrowTok.Span)
	if node := p.arenas.Exprs.Get(bodyExpr); node != nil {
		span = span.Cover(node.Span)
	}

	if modeTok.Kind == token.KwMap {
		exprID := p.arenas.Exprs.NewParallelMap(span, iterableExpr, args, bodyExpr)
		return exprID, true
	}

	exprID := p.arenas.Exprs.NewParallelReduce(span, iterableExpr, initExpr, args, bodyExpr)
	return exprID, true
}

func (p *Parser) parseAsyncExpr() (ast.ExprID, bool) {
	asyncTok := p.advance()

	if !p.at(token.LBrace) {
		p.err(diag.SynUnexpectedToken, "expected '{' after 'async'")
		return ast.NoExprID, false
	}

	bodyID, ok := p.parseBlock()
	if !ok {
		return ast.NoExprID, false
	}

	span := asyncTok.Span
	if stmt := p.arenas.Stmts.Get(bodyID); stmt != nil {
		span = span.Cover(stmt.Span)
	}
	return p.arenas.Exprs.NewAsync(span, bodyID), true
}

func (p *Parser) parseStructLiteral(typeID ast.TypeID, typeSpan source.Span) (ast.ExprID, bool) {
	openTok := p.advance()
	fields := make([]ast.ExprStructField, 0)
	var commas []source.Span
	trailing := false
	positional := false

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		p.suspendColonCast++
		fieldExpr, ok := p.parseExpr()
		p.suspendColonCast--
		if !ok {
			p.resyncStructLiteralField()
			continue
		}

		if !positional && p.at(token.Colon) {
			if ident, ok := p.arenas.Exprs.Ident(fieldExpr); ok && ident != nil {
				p.advance()
				valueExpr, valueOK := p.parseExpr()
				if !valueOK {
					p.resyncStructLiteralField()
					continue
				}
				fields = append(fields, ast.ExprStructField{
					Name:  ident.Name,
					Value: valueExpr,
				})
				goto handleComma
			}
			p.err(diag.SynExpectIdentifier, "expected identifier before ':' in struct literal")
			p.resyncStructLiteralField()
			continue
		}

		positional = true
		fields = append(fields, ast.ExprStructField{
			Name:  source.NoStringID,
			Value: fieldExpr,
		})

	handleComma:
		if p.at(token.Comma) {
			commaTok := p.advance()
			commas = append(commas, commaTok.Span)
			if p.at(token.RBrace) {
				trailing = true
				break
			}
			continue
		}

		break
	}

	closeTok, ok := p.expect(token.RBrace, diag.SynUnclosedBrace, "expected '}' to close struct literal", nil)
	if !ok {
		return ast.NoExprID, false
	}

	span := openTok.Span.Cover(closeTok.Span)
	if typeSpan != (source.Span{}) {
		span = typeSpan.Cover(span)
	}
	exprID := p.arenas.Exprs.NewStruct(span, typeID, fields, commas, trailing, positional)
	return exprID, true
}

func (p *Parser) resyncStructLiteralField() {
	p.resyncUntil(token.Comma, token.RBrace, token.Semicolon, token.EOF)
	if p.at(token.Comma) || p.at(token.Semicolon) {
		p.advance()
	}
}

func (p *Parser) parseParallelArgList() ([]ast.ExprID, source.Span, bool) {
	openTok, ok := p.expect(token.LParen, diag.SynUnexpectedToken, "expected '(' to start parallel argument list")
	if !ok {
		return nil, source.Span{}, false
	}

	args := make([]ast.ExprID, 0, 2)
	listSpan := openTok.Span

	if p.at(token.RParen) {
		closeTok := p.advance()
		listSpan = listSpan.Cover(closeTok.Span)
		return args, listSpan, true
	}

	for {
		argExpr, exprOK := p.parseExpr()
		if !exprOK {
			return nil, source.Span{}, false
		}
		args = append(args, argExpr)
		if node := p.arenas.Exprs.Get(argExpr); node != nil {
			listSpan = listSpan.Cover(node.Span)
		}

		if p.at(token.Comma) {
			commaTok := p.advance()
			listSpan = listSpan.Cover(commaTok.Span)
			if p.at(token.RParen) {
				closeTok := p.advance()
				listSpan = listSpan.Cover(closeTok.Span)
				return args, listSpan, true
			}
			continue
		}

		break
	}

	closeTok, ok := p.expect(token.RParen, diag.SynUnclosedParen, "expected ')' to close parallel argument list")
	if !ok {
		return nil, source.Span{}, false
	}
	listSpan = listSpan.Cover(closeTok.Span)
	return args, listSpan, true
}
