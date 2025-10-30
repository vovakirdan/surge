package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

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

		errSpan := p.currentErrorSpan()
		primarySpan := errSpan.ShiftLeft(1)
		if primarySpan.Start >= primarySpan.End || (primarySpan.Start == errSpan.Start && primarySpan.End == errSpan.End) {
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
		p.resyncTop()
		if p.at(token.RBracket) {
			p.advance()
		}
		return ast.NoTypeID, false
	}

	return currentType, true
}
