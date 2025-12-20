package parser

import (
	"unicode"
	"unicode/utf8"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

// parseIdentOrStructLiteral parses either a plain identifier expression or a typed struct literal.
func (p *Parser) parseIdentOrStructLiteral() (ast.ExprID, bool) {
	tok := p.advance()
	if tok.Kind != token.Ident && tok.Kind != token.Underscore {
		p.err(diag.SynExpectIdentifier, "expected identifier")
		return ast.NoExprID, false
	}
	nameID := p.arenas.StringsInterner.Intern(tok.Text)
	// Don't parse as struct literal in type operand context (e.g., 'x is MyType')
	// to avoid treating 'MyType {' as struct literal when followed by if-block
	if p.inTypeOperandContext == 0 && tok.Kind == token.Ident && p.isTypeLiteralName(tok.Text) && p.at(token.LBrace) {
		segments := []ast.TypePathSegment{{
			Name:     nameID,
			Generics: nil,
		}}
		typeID := p.arenas.Types.NewPath(tok.Span, segments)
		return p.parseStructLiteral(typeID, tok.Span)
	}
	return p.arenas.Exprs.NewIdent(tok.Span, nameID), true
}

func (p *Parser) isTypeLiteralName(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	if r == utf8.RuneError {
		return false
	}
	return unicode.IsUpper(r)
}

func (p *Parser) typePathFromExpr(expr ast.ExprID) (ast.TypeID, bool) {
	if expr == ast.NoExprID || p.arenas == nil {
		return ast.NoTypeID, false
	}
	var segments []ast.TypePathSegment
	current := expr
	for {
		node := p.arenas.Exprs.Get(current)
		if node == nil {
			return ast.NoTypeID, false
		}
		switch node.Kind {
		case ast.ExprIdent:
			if ident, ok := p.arenas.Exprs.Ident(current); ok && ident != nil {
				segments = append(segments, ast.TypePathSegment{Name: ident.Name})
				goto done
			}
			return ast.NoTypeID, false
		case ast.ExprMember:
			mem, ok := p.arenas.Exprs.Member(current)
			if !ok || mem == nil {
				return ast.NoTypeID, false
			}
			segments = append(segments, ast.TypePathSegment{Name: mem.Field})
			current = mem.Target
		default:
			return ast.NoTypeID, false
		}
	}

done:
	// reverse segments to restore left-to-right order
	for i, j := 0, len(segments)-1; i < j; i, j = i+1, j-1 {
		segments[i], segments[j] = segments[j], segments[i]
	}
	if len(segments) == 0 {
		return ast.NoTypeID, false
	}
	// last segment must look like a type
	lastName := p.arenas.StringsInterner.MustLookup(segments[len(segments)-1].Name)
	if !p.isTypeLiteralName(lastName) {
		return ast.NoTypeID, false
	}
	typeID := p.arenas.Types.NewPath(p.arenas.Exprs.Get(expr).Span, segments)
	return typeID, true
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

	prevRangeLiteralExprDepth := p.rangeLiteralExprDepth
	prevRangeLiteralPending := p.rangeLiteralPending
	prevRangeLiteralStart := p.rangeLiteralStart
	prevRangeLiteralInclusive := p.rangeLiteralInclusive
	prevRangeLiteralSpan := p.rangeLiteralSpan
	defer func() {
		p.rangeLiteralExprDepth = prevRangeLiteralExprDepth
		p.rangeLiteralPending = prevRangeLiteralPending
		p.rangeLiteralStart = prevRangeLiteralStart
		p.rangeLiteralInclusive = prevRangeLiteralInclusive
		p.rangeLiteralSpan = prevRangeLiteralSpan
	}()

	// проверяем на пустой массив

	if p.at(token.RBracket) {
		closeTok := p.advance()
		finalSpan := openTok.Span.Cover(closeTok.Span)
		return p.arenas.Exprs.NewArray(finalSpan, []ast.ExprID{}, nil, false), true
	}

	if p.at(token.DotDot) || p.at(token.DotDotEq) {
		opTok := p.advance()
		inclusive := opTok.Kind == token.DotDotEq
		if p.at(token.RBracket) {
			closeTok := p.advance()
			if inclusive {
				p.emitDiagnostic(
					diag.SynExpectExpression,
					diag.SevError,
					opTok.Span,
					"inclusive end requires an end bound",
					nil,
				)
				return ast.NoExprID, false
			}
			finalSpan := openTok.Span.Cover(closeTok.Span)
			return p.arenas.Exprs.NewRangeLit(finalSpan, ast.NoExprID, ast.NoExprID, false), true
		}
		beforeErrors := p.opts.CurrentErrors
		end, ok := p.parseExpr()
		if !ok {
			if p.opts.CurrentErrors == beforeErrors {
				errSpan := p.currentErrorSpan()
				p.emitDiagnostic(
					diag.SynExpectExpression,
					diag.SevError,
					errSpan,
					"expected expression after range operator",
					nil,
				)
			}
			p.resyncUntil(token.RBracket, token.Semicolon)
			return ast.NoExprID, false
		}
		closeTok, closeOK := p.expect(token.RBracket, diag.SynUnclosedSquareBracket, "expected ']' after range literal", nil)
		if !closeOK {
			return ast.NoExprID, false
		}
		finalSpan := openTok.Span.Cover(closeTok.Span)
		return p.arenas.Exprs.NewRangeLit(finalSpan, ast.NoExprID, end, inclusive), true
	}

	// Парсим первое выражение
	beforeErrors := p.opts.CurrentErrors
	p.rangeLiteralExprDepth = p.exprDepth + 1
	p.rangeLiteralPending = false
	p.rangeLiteralStart = ast.NoExprID
	p.rangeLiteralInclusive = false
	p.rangeLiteralSpan = source.Span{}
	first, ok := p.parseExpr()
	p.rangeLiteralExprDepth = 0
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

	if p.rangeLiteralPending {
		start := p.rangeLiteralStart
		inclusive := p.rangeLiteralInclusive
		opSpan := p.rangeLiteralSpan
		p.rangeLiteralPending = false
		p.rangeLiteralStart = ast.NoExprID
		if inclusive {
			p.emitDiagnostic(
				diag.SynExpectExpression,
				diag.SevError,
				opSpan,
				"inclusive end requires an end bound",
				nil,
			)
			if p.at(token.RBracket) {
				p.advance()
			}
			return ast.NoExprID, false
		}
		closeTok, closeOK := p.expect(token.RBracket, diag.SynUnclosedSquareBracket, "expected ']' after range literal", nil)
		if !closeOK {
			return ast.NoExprID, false
		}
		finalSpan := openTok.Span.Cover(closeTok.Span)
		return p.arenas.Exprs.NewRangeLit(finalSpan, start, ast.NoExprID, false), true
	}

	if bin, binOK := p.arenas.Exprs.Binary(first); binOK && bin != nil {
		if (bin.Op == ast.ExprBinaryRange || bin.Op == ast.ExprBinaryRangeInclusive) && !p.at(token.Comma) {
			closeTok, closeOK := p.expect(token.RBracket, diag.SynUnclosedSquareBracket, "expected ']' after range literal", nil)
			if !closeOK {
				return ast.NoExprID, false
			}
			finalSpan := openTok.Span.Cover(closeTok.Span)
			inclusive := bin.Op == ast.ExprBinaryRangeInclusive
			return p.arenas.Exprs.NewRangeLit(finalSpan, bin.Left, bin.Right, inclusive), true
		}
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
