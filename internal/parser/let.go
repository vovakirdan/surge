package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

// parseLetBinding парсит биндинг let: [mut] name : Type? = Expr?
// Этот метод переиспользуется в let items, параметрах функций, полях структур
func (p *Parser) parseLetBinding() (LetBinding, bool) {
	startSpan := p.lx.Peek().Span

	// Парсим модификатор mut (если есть)
	var isMut bool
	var mutSpan source.Span
	if p.at(token.KwMut) {
		isMut = true
		mutTok := p.advance()
		mutSpan = mutTok.Span
	}

	// Парсим имя переменной
	nameID, ok := p.parseIdent()
	if !ok {
		return LetBinding{}, false
	}
	nameSpan := p.lastSpan

	// Парсим тип (если есть двоеточие)
	var colonSpan source.Span
	typeID, ok := func() (ast.TypeID, bool) {
		if p.at(token.Colon) {
			colonSpan = p.lx.Peek().Span
		}
		return p.parseTypeExpr()
	}()
	if !ok {
		return LetBinding{}, false
	}
	var typeSpan source.Span
	if typeID.IsValid() {
		if typ := p.arenas.Types.Get(typeID); typ != nil {
			typeSpan = typ.Span
		}
	}

	// Парсим инициализацию (если есть =)
	valueID := ast.NoExprID
	var assignSpan source.Span
	var valueSpan source.Span
	if p.at(token.Assign) {
		tokAssign := p.advance() // съедаем '='
		assignSpan = tokAssign.Span
		var ok bool
		beforeErrors := p.opts.CurrentErrors
		valueID, ok = p.parseExpr()
		if !ok {
			if p.opts.CurrentErrors == beforeErrors {
				p.emitDiagnostic(
					diag.SynExpectExpression,
					diag.SevError,
					tokAssign.Span,
					"expected expression after '='",
					func(b *diag.ReportBuilder) {
						if b == nil {
							return
						}
						fixID := fix.MakeFixID(diag.SynExpectExpression, tokAssign.Span)
						suggestion := fix.DeleteSpan(
							"remove '=' to simplify the let binding",
							tokAssign.Span,
							"",
							fix.WithID(fixID),
							fix.WithKind(diag.FixKindRefactor),
							fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
						)
						b.WithFixSuggestion(suggestion)
						b.WithNote(tokAssign.Span, "remove '=' to simplify the let binding")
					},
				)
			}
			return LetBinding{}, false
		}
		if expr := p.arenas.Exprs.Get(valueID); expr != nil {
			valueSpan = expr.Span
		}
	}

	// Проверяем, что хотя бы тип или значение указано
	if typeID == ast.NoTypeID && valueID == ast.NoExprID {
		// p.err(diag.SynExpectType, "let binding must have either type annotation or initializer")
		// здесь мы если не нашли тип и значение, то мы должны предложить два фикса:
		// либо убрать ident, либо добавить ":"
		spanWhereShouldBeColon := p.lastSpan.ZeroideToEnd()
		spanWhereUnexpectedIdent := p.currentErrorSpan()
		combinedSpan := spanWhereShouldBeColon.Cover(spanWhereUnexpectedIdent)
		p.emitDiagnostic(
			diag.SynExpectColon,
			diag.SevError,
			combinedSpan,
			"let binding must have either type annotation or initializer",
			func(b *diag.ReportBuilder) {
				if b == nil {
					return
				}
				// проверить тип ли это мы сможем только на семантике, так что предложим сначала ":"
				fixIDInsertColon := fix.MakeFixID(diag.SynExpectColon, spanWhereShouldBeColon)
				suggestionInsertColon := fix.InsertText(
					"insert colon to add type annotation",
					spanWhereShouldBeColon,
					":",
					"",
					fix.WithID(fixIDInsertColon),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityManualReview),
					fix.Preferred(),
				)
				b.WithFixSuggestion(suggestionInsertColon)
				fixIDDeleteIdent := fix.MakeFixID(diag.SynExpectType, spanWhereUnexpectedIdent)
				// и уже вторым фиксом предлагаем удалить ident
				suggestionDeleteIdent := fix.DeleteSpan(
					"remove ident to simplify the let binding",
					spanWhereUnexpectedIdent,
					"",
					fix.WithID(fixIDDeleteIdent),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityManualReview),
				)
				b.WithFixSuggestion(suggestionDeleteIdent)
				b.WithNote(spanWhereUnexpectedIdent, "insert colon to add type annotation or remove ident to simplify the let binding")
			},
		)
		return LetBinding{}, false
	}

	binding := LetBinding{
		Name:       nameID,
		Type:       typeID,
		Value:      valueID,
		IsMut:      isMut,
		Span:       startSpan.Cover(p.lastSpan),
		MutSpan:    mutSpan,
		NameSpan:   nameSpan,
		ColonSpan:  colonSpan,
		TypeSpan:   typeSpan,
		AssignSpan: assignSpan,
		ValueSpan:  valueSpan,
	}

	return binding, true
}

// parseLetItem распознаёт let items верхнего уровня:
//
//	let [mut] name: Type = Expr;
//	let [mut] name: Type;
//	let [mut] name = Expr;
func (p *Parser) parseLetItem() (ast.ItemID, bool) {
	return p.parseLetItemWithVisibility(nil, source.Span{}, ast.VisPrivate, source.Span{}, false)
}

func (p *Parser) parseLetItemWithVisibility(attrs []ast.Attr, attrSpan source.Span, visibility ast.Visibility, prefixSpan source.Span, hasPrefix bool) (ast.ItemID, bool) {
	letTok := p.advance() // съедаем KwLet

	// Парсим биндинг
	binding, ok := p.parseLetBinding()
	if !ok {
		insertPos := p.lastSpan.ZeroideToEnd()
		p.resyncUntil(token.Semicolon, token.RBrace, token.EOF)
		if p.at(token.Semicolon) {
			p.advance()
		} else {
			p.emitDiagnostic(
				diag.SynExpectSemicolon,
				diag.SevError,
				insertPos,
				"expected semicolon after let item",
				func(b *diag.ReportBuilder) {
					if b == nil {
						return
					}
					fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertPos)
					suggestion := fix.InsertText(
						"insert semicolon after let item",
						insertPos,
						";",
						"",
						fix.WithID(fixID),
						fix.WithKind(diag.FixKindRefactor),
						fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
						fix.Preferred(),
					)
					b.WithFixSuggestion(suggestion)
					b.WithNote(insertPos, "insert missing semicolon")
				},
			)
		}
		p.resyncTop()
		return ast.NoItemID, false
	}

	insertPos := p.lastSpan.ZeroideToEnd()

	if !p.at(token.Semicolon) {
		p.emitDiagnostic(
			diag.SynExpectSemicolon,
			diag.SevError,
			insertPos,
			"expected semicolon after let item",
			func(b *diag.ReportBuilder) {
				if b == nil {
					return
				}
				fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertPos)
				suggestion := fix.InsertText(
					"insert semicolon after let item",
					insertPos,
					";",
					"",
					fix.WithID(fixID),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
					fix.Preferred(),
				)
				b.WithFixSuggestion(suggestion)
				b.WithNote(insertPos, "insert missing semicolon")
			},
		)
		p.resyncTop()
		return ast.NoItemID, false
	}
	semiTok := p.advance()

	// Создаем LetItem в AST
	finalSpan := letTok.Span.Cover(semiTok.Span)
	if attrSpan.End > attrSpan.Start {
		finalSpan = attrSpan.Cover(finalSpan)
	}
	if hasPrefix {
		finalSpan = prefixSpan.Cover(finalSpan)
	}
	itemID := p.arenas.Items.NewLet(
		binding.Name,
		binding.Type,
		binding.Value,
		binding.IsMut,
		visibility,
		attrs,
		letTok.Span,
		binding.MutSpan,
		binding.NameSpan,
		binding.ColonSpan,
		binding.AssignSpan,
		semiTok.Span,
		finalSpan,
	)

	return itemID, true
}
