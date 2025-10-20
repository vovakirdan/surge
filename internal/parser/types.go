package parser

import (
	"fmt"
	"strconv"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

// LetBinding представляет парсированный биндинг let
type LetBinding struct {
	Name  source.StringID
	Type  ast.TypeID
	Value ast.ExprID
	IsMut bool
	Span  source.Span
}

// parseTypeExpr распознаёт полные типовые выражения:
//
//	own Type, &Type, &mut Type, *Type (префиксы)
//	Type[] (постфиксы)
//	Type[Expr] (постфиксы)
//	простые типы
func (p *Parser) parseTypeExpr() (ast.TypeID, bool) {
	// Если нет двоеточия, тип не указан
	if !p.at(token.Colon) {
		return ast.NoTypeID, true
	}
	p.advance()

	return p.parseTypePrefix()
}

// parseTypePrefix обрабатывает цепочки префиксов: own, &, &mut, *
// Поддерживает множественные префиксы типа **int, &&mut Payload, own &T
func (p *Parser) parseTypePrefix() (ast.TypeID, bool) {
	type prefixInfo struct {
		op   ast.TypeUnaryOp
		span source.Span
	}

	var prefixes []prefixInfo

prefixLoop:
	for {
		switch p.lx.Peek().Kind {
		case token.KwOwn:
			start := p.lx.Peek().Span
			p.advance()
			prefixes = append(prefixes, prefixInfo{
				op:   ast.TypeUnaryOwn,
				span: start.Cover(p.lastSpan),
			})
		case token.Amp:
			start := p.lx.Peek().Span
			p.advance()
			end := p.lastSpan
			if p.at(token.KwMut) {
				p.advance()
				end = p.lastSpan
				prefixes = append(prefixes, prefixInfo{
					op:   ast.TypeUnaryRefMut,
					span: start.Cover(end),
				})
			} else {
				prefixes = append(prefixes, prefixInfo{
					op:   ast.TypeUnaryRef,
					span: start.Cover(end),
				})
			}
		case token.AndAnd:
			start := p.lx.Peek().Span
			p.advance()
			end := p.lastSpan
			if p.at(token.KwMut) {
				// &&mut = & + &mut
				prefixes = append(prefixes,
					prefixInfo{op: ast.TypeUnaryRef, span: start.Cover(end)},
				)
				p.advance()
				end = p.lastSpan
				prefixes = append(prefixes,
					prefixInfo{op: ast.TypeUnaryRefMut, span: start.Cover(end)},
				)
			} else {
				// && = & + &
				prefixes = append(prefixes,
					prefixInfo{op: ast.TypeUnaryRef, span: start.Cover(end)},
					prefixInfo{op: ast.TypeUnaryRef, span: start.Cover(end)},
				)
			}
		case token.Star:
			start := p.lx.Peek().Span
			p.advance()
			prefixes = append(prefixes, prefixInfo{
				op:   ast.TypeUnaryPointer,
				span: start.Cover(p.lastSpan),
			})
		default:
			// Больше префиксов нет, выходим из цикла
			break prefixLoop
		}
	}

	// Парсим базовый тип
	baseType, ok := p.parseTypePrimary()
	if !ok {
		return ast.NoTypeID, false
	}

	// Применяем префиксы справа налево (последний префикс - ближе к базовому типу)
	currentType := baseType
	for i := len(prefixes) - 1; i >= 0; i-- {
		// Получаем span текущего типа для правильного объединения
		currentSpan := p.arenas.Types.Get(currentType).Span
		finalSpan := prefixes[i].span.Cover(currentSpan)
		currentType = p.arenas.Types.NewUnary(finalSpan, prefixes[i].op, currentType)
	}

	return currentType, true
}

// parseTypePrimary обрабатывает базовые формы типов:
//
//	идентификатор/квалифицированный.путь
//	nothing
//	( tuple )
//	fn ( сигнатура ) -> ...
func (p *Parser) parseTypePrimary() (ast.TypeID, bool) {
	startSpan := p.lx.Peek().Span

	switch p.lx.Peek().Kind {
	case token.Ident:
		// Квалифицированный путь к типу: Ident ( "." Ident )*
		var segments []ast.TypePathSegment

		// Парсим первый идентификатор
		identText, ok := p.parseIdent()
		if !ok {
			return ast.NoTypeID, false
		}
		identID := p.arenas.StringsInterner.Intern(identText)
		segments = append(segments, ast.TypePathSegment{
			Name:     identID,
			Generics: nil, // пока без generic аргументов
		})

		// Парсим дополнительные сегменты через точку
		for p.at(token.Dot) {
			p.advance() // съедаем '.'

			// После точки должен быть идентификатор
			if !p.at(token.Ident) {
				p.err(diag.SynExpectIdentifier, "expected identifier after '.'")
				return ast.NoTypeID, false
			}

			identText, ok := p.parseIdent()
			if !ok {
				return ast.NoTypeID, false
			}
			identID := p.arenas.StringsInterner.Intern(identText)
			segments = append(segments, ast.TypePathSegment{
				Name:     identID,
				Generics: nil, // пока без generic аргументов
			})
		}

		baseType := p.arenas.Types.NewPath(startSpan.Cover(p.lastSpan), segments)
		return p.parseTypeSuffix(baseType)

	case token.NothingLit:
		nothingTok := p.advance()
		return p.parseTypeSuffix(p.makeBuiltinType("nothing", nothingTok.Span))

	case token.LParen:
		openTok := p.advance()

		if p.at(token.RParen) {
			closeTok := p.advance()
			tupleType := p.arenas.Types.NewTuple(openTok.Span.Cover(closeTok.Span), nil)
			return p.parseTypeSuffix(tupleType)
		}

		firstElem, ok := p.parseTypePrefix()
		if !ok {
			return ast.NoTypeID, false
		}
		elements := []ast.TypeID{firstElem}
		sawComma := false

		for p.at(token.Comma) {
			sawComma = true
			p.advance() // съедаем ','

			if p.at(token.RParen) {
				break // допускаем завершающую запятую
			}

			elem, ok := p.parseTypePrefix()
			if !ok {
				return ast.NoTypeID, false
			}
			elements = append(elements, elem)
		}

		closeTok, ok := p.expect(token.RParen, diag.SynUnclosedParen, "expected ')' to close tuple type", nil)
		if !ok {
			return ast.NoTypeID, false
		}

		if len(elements) == 1 && !sawComma {
			// скобки — просто группировка
			return p.parseTypeSuffix(elements[0])
		}

		tupleType := p.arenas.Types.NewTuple(openTok.Span.Cover(closeTok.Span), elements)
		return p.parseTypeSuffix(tupleType)

	case token.KwFn:
		fnTok := p.advance()
		if _, ok := p.expect(token.LParen, diag.SynUnexpectedToken, "expected '(' after 'fn' in function type", nil); !ok {
			return ast.NoTypeID, false
		}

		var params []ast.TypeFnParam
		var sawVariadic bool

		if !p.at(token.RParen) {
			for {
				if p.at(token.DotDotDot) {
					if sawVariadic {
						p.err(diag.SynUnexpectedToken, "multiple variadic parameters are not allowed")
						return ast.NoTypeID, false
					}
					p.advance()
					elemType, ok := p.parseTypePrefix()
					if !ok {
						return ast.NoTypeID, false
					}
					params = append(params, ast.TypeFnParam{
						Type:     elemType,
						Name:     source.NoStringID,
						Variadic: true,
					})
					sawVariadic = true
					if p.at(token.Comma) {
						p.err(diag.SynUnexpectedToken, "variadic parameter must be last in function type")
						p.advance()
					}
					break
				}

				elemType, ok := p.parseTypePrefix()
				if !ok {
					return ast.NoTypeID, false
				}
				params = append(params, ast.TypeFnParam{
					Type:     elemType,
					Name:     source.NoStringID,
					Variadic: false,
				})

				if !p.at(token.Comma) {
					break
				}
				p.advance()

				if p.at(token.RParen) {
					break
				}
				if sawVariadic {
					p.err(diag.SynUnexpectedToken, "parameters cannot follow a variadic parameter")
					break
				}
			}
		}

		closeTok, ok := p.expect(token.RParen, diag.SynUnclosedParen, "expected ')' to close function type parameters", nil)
		if !ok {
			return ast.NoTypeID, false
		}

		fnSpan := fnTok.Span.Cover(closeTok.Span)
		var returnType ast.TypeID

		if p.at(token.Arrow) {
			arrowTok := p.advance()
			retType, ok := p.parseTypePrefix()
			if !ok {
				return ast.NoTypeID, false
			}
			returnType = retType
			retSpan := p.arenas.Types.Get(returnType).Span
			fnSpan = fnSpan.Cover(arrowTok.Span.Cover(retSpan))
		} else {
			retSpan := source.Span{
				File:  closeTok.Span.File,
				Start: closeTok.Span.End,
				End:   closeTok.Span.End,
			}
			returnType = p.makeBuiltinType("nothing", retSpan)
		}

		fnType := p.arenas.Types.NewFn(fnSpan, params, returnType)
		return p.parseTypeSuffix(fnType)

	default:
		// p.err(diag.SynExpectType, "expected type")
		// так как := это токен, то мы можем уверенно сдвигаться
		spanColon := startSpan.ShiftLeft(2)
		p.emitDiagnostic(
			diag.SynExpectType,
			diag.SevError,
			spanColon,
			"expected type",
			func(b *diag.ReportBuilder) {
				if b == nil {
					return
				}
				fixID := fix.MakeFixID(diag.SynExpectType, spanColon)
				suggestion := fix.DeleteSpan(
					"remove type to simplify the type expression",
					spanColon,
					"",
					fix.WithID(fixID),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityAlwaysSafe), // todo подумать безопасно ли это
				)
				b.WithFixSuggestion(suggestion)
				b.WithNote(startSpan, "remove type to simplify the type expression")
			},
		)
		return ast.NoTypeID, false
	}
}

// parseTypeSuffix обрабатывает постфиксы: [], [Expr]
func (p *Parser) parseTypeSuffix(baseType ast.TypeID) (ast.TypeID, bool) {
	currentType := baseType

	// Обрабатываем массивы в цикле для поддержки вложенных массивов
	for p.at(token.LBracket) {
		p.advance()

		if p.at(token.RBracket) {
			// Динамический массив []Type
			closeTok := p.advance()
			currentTypeSpan := p.arenas.Types.Get(currentType).Span
			finalSpan := currentTypeSpan.Cover(closeTok.Span)

			currentType = p.arenas.Types.NewArray(
				finalSpan,
				currentType,
				ast.ArraySlice,
				ast.NoExprID,
				false,
				0,
			)
			continue
		}

		if p.at(token.IntLit) || p.at(token.UintLit) {
			sizeTok := p.advance()
			lengthValue, ok := p.parseArraySizeLiteral(sizeTok)
			if !ok {
				// ошибка уже зарепорчена
				p.resyncUntil(token.RBracket, token.Semicolon, token.Comma)
				if p.at(token.RBracket) {
					p.advance()
				}
				return ast.NoTypeID, false
			}

			if !p.at(token.RBracket) {
				// p.err(diag.SynExpectRightBracket, "expected ']' after array size")
				rightBracketSpan := p.currentErrorSpan().ZeroideToStart()
				p.emitDiagnostic(
					diag.SynExpectRightBracket,
					diag.SevError,
					p.currentErrorSpan(),
					"expected ']' after array size",
					func(b *diag.ReportBuilder) {
						if b == nil {
							return
						}
						// как же не хватает макросов сейчас...
						fixID := fix.MakeFixID(diag.SynExpectRightBracket, rightBracketSpan)
						suggestion := fix.InsertText(
							"insert ']' after array size",
							rightBracketSpan,
							"]",
							"",
							fix.WithID(fixID),
							fix.WithKind(diag.FixKindRefactor),
							fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
						)
						b.WithFixSuggestion(suggestion)
						b.WithNote(rightBracketSpan, "insert ']' after array size")
					},
				)
				return ast.NoTypeID, false
			}
			closeTok := p.advance()

			currentTypeSpan := p.arenas.Types.Get(currentType).Span
			finalSpan := currentTypeSpan.Cover(closeTok.Span)

			currentType = p.arenas.Types.NewArray(
				finalSpan,
				currentType,
				ast.ArraySized,
				ast.NoExprID,
				true,
				lengthValue,
			)
			continue
		}

		// p.err(diag.SynExpectRightBracket, "expected ']' or array size")
		errSpan := p.currentErrorSpan()
		primarySpan := errSpan.ShiftLeft(1)
		if primarySpan.Start == primarySpan.End && primarySpan.Start == errSpan.Start {
			primarySpan = errSpan
		}
		insertSpan := errSpan.ZeroideToStart()

		p.emitDiagnostic(
			diag.SynExpectRightBracket,
			diag.SevError,
			primarySpan,
			"expected ']' or array size",
			func(b *diag.ReportBuilder) {
				if b == nil {
					return
				}
				fixID := fix.MakeFixID(diag.SynExpectRightBracket, insertSpan)
				suggestion := fix.InsertText(
					"insert ']' to close array type",
					insertSpan,
					"]",
					"",
					fix.WithID(fixID),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
					fix.Preferred(),
				)
				b.WithFixSuggestion(suggestion)
				b.WithNote(insertSpan, "insert ']' to close array type")
			},
		)
		// p.resyncUntil(token.RBracket, token.Semicolon, token.Comma)
		p.resyncTop()
		if p.at(token.RBracket) {
			p.advance()
		}
		return ast.NoTypeID, false
	}

	return currentType, true
}

func (p *Parser) makeBuiltinType(name string, span source.Span) ast.TypeID {
	nameID := p.arenas.StringsInterner.Intern(name)
	segments := []ast.TypePathSegment{{
		Name:     nameID,
		Generics: nil,
	}}
	return p.arenas.Types.NewPath(span, segments)
}

func (p *Parser) parseArraySizeLiteral(tok token.Token) (uint64, bool) {
	clean := strings.ReplaceAll(tok.Text, "_", "")
	if clean == "" {
		p.err(diag.SynUnexpectedToken, "array size literal is empty")
		return 0, false
	}
	if strings.HasPrefix(clean, "+") || strings.HasPrefix(clean, "-") {
		p.err(diag.SynUnexpectedToken, "array size literal must be a non-negative integer")
		return 0, false
	}

	body, suffix, err := splitNumericLiteral(clean)
	if err != nil {
		p.err(diag.SynUnexpectedToken, fmt.Sprintf("invalid array size literal %q: %v", tok.Text, err))
		return 0, false
	}
	if suffix != "" && !isValidIntegerSuffix(suffix) {
		p.err(diag.SynUnexpectedToken, fmt.Sprintf("invalid array size suffix %q", suffix))
		return 0, false
	}

	value, err := strconv.ParseUint(body, 0, 64)
	if err != nil {
		p.err(diag.SynUnexpectedToken, fmt.Sprintf("array size literal %q is out of range", tok.Text))
		return 0, false
	}
	return value, true
}
