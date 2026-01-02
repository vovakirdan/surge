package parser

import (
	"fmt"
	"strconv"
	"time"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
	"surge/internal/trace"
)

// parseExpr - главная точка входа для парсинга выражений
// Возвращает ExprID и флаг успеха
func (p *Parser) parseExpr() (ast.ExprID, bool) {
	return p.parseBinaryExpr(0) // минимальный приоритет = 0
}

// parseBinaryExpr реализует Pratt parsing для бинарных операторов
// minPrec - минимальный приоритет для текущего уровня
func (p *Parser) parseBinaryExpr(minPrec int) (ast.ExprID, bool) {
	// Track recursion depth
	p.exprDepth++
	defer func() { p.exprDepth-- }()

	// Only trace if depth < 20 to avoid noise
	var span *trace.Span
	if p.tracer != nil && p.tracer.Level() >= trace.LevelDebug && p.exprDepth <= 20 {
		span = trace.Begin(p.tracer, trace.ScopeNode, "parse_binary_expr", 0)
		span.WithExtra("depth", fmt.Sprintf("%d", p.exprDepth))
		defer func() {
			if span != nil {
				span.End("")
			}
		}()
	}

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
				"'=>' is only allowed inside parallel expressions or compare/select/race arms",
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
		if opTok.Kind == token.QuestionQuestion {
			p.emitDiagnostic(
				diag.FutNullCoalescingNotSupported,
				diag.SevError,
				opTok.Span,
				"null coalescing '??' is not supported in the language",
				nil,
			)
		}

		// Вычисляем приоритет для правой части
		nextMinPrec := prec + 1
		if isRightAssoc {
			nextMinPrec = prec
		}

		// For 'is' and 'heir' operators, the right operand is a type, not an expression.
		// Set context flag to prevent struct literal parsing (e.g., 'x is MyType {' should not
		// parse 'MyType {' as struct literal when followed by if-block).
		isTypeOperator := opTok.Kind == token.KwIs || opTok.Kind == token.KwHeir
		if isTypeOperator {
			p.inTypeOperandContext++
		}

		// Парсим правую часть
		right, ok := p.parseBinaryExpr(nextMinPrec)

		if isTypeOperator {
			p.inTypeOperandContext--
		}

		if !ok {
			if (opTok.Kind == token.DotDot || opTok.Kind == token.DotDotEq) &&
				p.rangeLiteralExprDepth != 0 &&
				p.rangeLiteralExprDepth == p.exprDepth &&
				p.at(token.RBracket) {
				p.rangeLiteralPending = true
				p.rangeLiteralStart = left
				p.rangeLiteralInclusive = opTok.Kind == token.DotDotEq
				p.rangeLiteralSpan = opTok.Span
				return left, true
			}
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

	// Check for ternary operator (right-associative, precTernary = 3)
	if p.at(token.Question) && minPrec <= precTernary {
		return p.parseTernaryExpr(left)
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
	var span *trace.Span
	iterations := 0
	if p.tracer != nil && p.tracer.Level() >= trace.LevelDebug {
		span = trace.Begin(p.tracer, trace.ScopeNode, "parse_postfix_expr", 0)
		defer func() {
			if span != nil {
				span.End(fmt.Sprintf("iterations=%d", iterations))
			}
		}()
	}

	// Парсим базовое выражение
	expr, ok := p.parsePrimaryExpr()
	if !ok {
		return ast.NoExprID, false
	}

	var pendingTypeArgs []ast.TypeID

	// Обрабатываем постфиксы в цикле
	for {
		iterations++

		// Warning if too many iterations (possible infinite loop)
		if iterations > 100 && p.tracer != nil && p.tracer.Level() >= trace.LevelDebug {
			p.tracer.Emit(&trace.Event{
				Time:   time.Now(),
				Kind:   trace.KindPoint,
				Scope:  trace.ScopeNode,
				Name:   "postfix_loop_warning",
				Detail: fmt.Sprintf("iterations=%d", iterations),
			})
		}
		if ok, ltTok := p.looksLikeBareGenericCall(expr); ok {
			calleeSpan := p.arenas.Exprs.Get(expr).Span
			builder := diag.ReportError(p.opts.Reporter, diag.SynUnexpectedToken, ltTok.Span, "generic type arguments must use '::<' syntax")
			if builder != nil && calleeSpan != (source.Span{}) {
				builder.WithFixSuggestion(fix.InsertText("insert '::' for generic call", calleeSpan.ZeroideToEnd().ZeroideToStart(), "::", "", fix.Preferred()))
				builder.Emit()
			}
			return expr, true
		}
		switch p.lx.Peek().Kind {
		case token.ColonColon:
			doubleColon := p.advance()
			// После :: может быть либо < для type arguments, либо identifier для enum variant
			switch p.lx.Peek().Kind {
			case token.Lt:
				// Type arguments: expr::<T>(...)
				typeArgs, ok := p.parseTypeArgs()
				if !ok {
					return ast.NoExprID, false
				}
				pendingTypeArgs = typeArgs
				// After type args, we can have:
				// - '(' for function call: Type::<int>(args)
				// - '::' for static member access: Type::<int>::new()
				// - '{' for struct literal: Type::<int>{ fields }
				if !p.at(token.LParen) && !p.at(token.ColonColon) && !p.at(token.LBrace) {
					p.emitDiagnostic(diag.SynUnexpectedToken, diag.SevError, p.currentErrorSpan(), "expected '(', '::' or '{' after type arguments", nil)
					return ast.NoExprID, false
				}
			case token.Ident:
				// Enum variant or static member: Type::Variant
				variantTok := p.advance()
				variantName := variantTok.Text
				variantNameID := p.arenas.StringsInterner.Intern(variantName)
				exprSpan := p.arenas.Exprs.Get(expr).Span
				finalSpan := exprSpan.Cover(doubleColon.Span).Cover(variantTok.Span)
				expr = p.arenas.Exprs.NewMember(finalSpan, expr, variantNameID)
			default:
				p.emitDiagnostic(diag.SynUnexpectedToken, diag.SevError, doubleColon.Span, "expected '<' or identifier after '::'", nil)
				return ast.NoExprID, false
			}
		case token.LParen:
			// Вызов функции: expr(args...)
			newExpr, ok := p.parseCallExpr(expr, pendingTypeArgs)
			if !ok {
				return ast.NoExprID, false
			}
			expr = newExpr
			pendingTypeArgs = nil

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
		case token.LBrace:
			// Don't parse as struct literal in type operand context (e.g., 'x is MyType')
			if p.inTypeOperandContext > 0 {
				return expr, true
			}
			if typeID, ok := p.typePathFromExpr(expr); ok {
				typeSpan := p.arenas.Types.Get(typeID).Span
				return p.parseStructLiteral(typeID, typeSpan)
			}
			return expr, true
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

		case token.FloatLit:
			tok := p.lx.Peek()
			if len(tok.Text) > 1 && tok.Text[0] == '.' {
				// This is tuple index access like .0, .1
				// Parse the index from the token text (skip the leading '.')
				idxStr := tok.Text[1:]
				var idx uint64
				var err error
				// Check if there's a decimal point in the index (like .0.5)
				hasDot := false
				for i := range len(idxStr) {
					if idxStr[i] == '.' {
						hasDot = true
						break
					}
				}
				if hasDot {
					// Not a valid tuple index, return
					return expr, true
				}
				idx, err = strconv.ParseUint(idxStr, 10, 32)
				if err != nil {
					p.err(diag.SynInvalidTupleIndex, "invalid tuple index")
					return ast.NoExprID, false
				}
				idxTok := p.advance()
				targetSpan := p.arenas.Exprs.Get(expr).Span
				finalSpan := targetSpan.Cover(idxTok.Span)
				expr = p.arenas.Exprs.NewTupleIndex(finalSpan, expr, uint32(idx))
				continue
			}
			return expr, true

		default:
			// Больше постфиксов нет
			if len(pendingTypeArgs) > 0 {
				p.emitDiagnostic(diag.SynUnexpectedToken, diag.SevError, p.currentErrorSpan(), "expected '(' after type arguments", nil)
				return ast.NoExprID, false
			}
			return expr, true
		}
	}
}

func (p *Parser) looksLikeBareGenericCall(expr ast.ExprID) (bool, token.Token) {
	if expr == ast.NoExprID || p.lx.Peek().Kind != token.Lt || p.fs == nil {
		return false, token.Token{}
	}
	node := p.arenas.Exprs.Get(expr)
	if node == nil || (node.Kind != ast.ExprIdent && node.Kind != ast.ExprMember) {
		return false, token.Token{}
	}
	ltTok := p.lx.Peek()
	file := p.fs.Get(ltTok.Span.File)
	if file == nil {
		return false, ltTok
	}
	data := file.Content
	if int(ltTok.Span.End) >= len(data) {
		return false, ltTok
	}
	i := int(ltTok.Span.End)
	// skip whitespace
	for i < len(data) {
		if data[i] != ' ' && data[i] != '\t' && data[i] != '\n' && data[i] != '\r' {
			break
		}
		i++
	}
	if i >= len(data) {
		return false, ltTok
	}
	if !isTypeStartByte(data[i]) {
		return false, ltTok
	}
	foundGt := false
	for i < len(data) {
		switch data[i] {
		case '>':
			foundGt = true
			i++
			goto afterGt
		case ';', '\n', '\r', ')', '{':
			return false, ltTok
		}
		i++
	}
afterGt:
	if !foundGt {
		return false, ltTok
	}
	for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\n' || data[i] == '\r') {
		i++
	}
	if i < len(data) && data[i] == '(' {
		return true, ltTok
	}
	return false, ltTok
}

func isTypeStartByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b == '_', b == '&', b == '*', b == '(':
		return true
	default:
		return false
	}
}

// parsePrimaryExpr парсит основные (атомарные) выражения
func (p *Parser) parsePrimaryExpr() (ast.ExprID, bool) {
	switch p.lx.Peek().Kind {
	case token.Ident, token.Underscore:
		return p.parseIdentOrStructLiteral()

	case token.IntLit, token.UintLit, token.FloatLit:
		// Числовые литералы
		return p.parseNumericLiteral()

	case token.StringLit:
		// Строковый литерал
		return p.parseStringLiteral()

	case token.FStringLit:
		return p.parseFString()

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

	case token.KwSelect:
		return p.parseSelectExpr()

	case token.KwRace:
		return p.parseRaceExpr()

	case token.KwParallel:
		return p.parseParallelExpr()

	case token.KwAsync:
		return p.parseAsyncExpr()

	case token.At:
		attrs, attrSpan, ok := p.parseAttributes()
		if !ok {
			return ast.NoExprID, false
		}
		if !p.at(token.KwAsync) {
			p.emitDiagnostic(
				diag.SynUnexpectedToken,
				diag.SevError,
				attrSpan,
				"attributes are only allowed before async blocks",
				nil,
			)
			return ast.NoExprID, false
		}
		return p.parseAsyncExprWithAttrs(attrs, attrSpan)

	case token.LBrace:
		return p.parseBraceExpr()

	default:
		// договоримся, что обрабатываем все ошибки до этого момента
		// p.err(diag.SynExpectExpression, "expected expression")
		return ast.NoExprID, false
	}
}
