package parser

import (
	"fmt"
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/token"
)

// parseLetBinding парсит биндинг let: [mut] name : Type? = Expr?
// Этот метод переиспользуется в let items, параметрах функций, полях структур
func (p *Parser) parseLetBinding() (LetBinding, bool) {
	startSpan := p.lx.Peek().Span

	// Парсим модификатор mut (если есть)
	var isMut bool
	if p.at(token.KwMut) {
		isMut = true
		p.advance()
	}

	// Парсим имя переменной
	nameText, ok := p.parseIdent()
	if !ok {
		return LetBinding{}, false
	}
	nameID := p.arenas.StringsInterner.Intern(nameText)

	// Парсим тип (если есть двоеточие)
	typeID, ok := p.parseTypeExpr()
	if !ok {
		return LetBinding{}, false
	}

	// Парсим инициализацию (если есть =)
	var valueID ast.ExprID = ast.NoExprID
	if p.at(token.Assign) {
		tokAssign := p.advance() // съедаем '='
		var ok bool
		valueID, ok = p.parseExpr()
		if !ok {
			// p.err(diag.SynExpectExpression, "expected expression after '='")
			// todo: попробуем так же посмотреть вокруг, если там пробелы - забираем их тоже
			p.emitDiagnostic(
				diag.SynExpectExpression,
				diag.SevError,
				tokAssign.Span,
				"expected expression after '='",
				func(b *diag.ReportBuilder) {
					if b == nil {
						return
					}
					fixID := fmt.Sprintf("%s-%d-%d", diag.SynExpectExpression.ID(), tokAssign.Span.File, tokAssign.Span.Start)
					suggestion := fix.DeleteSpan(
						"remove '=' to simplify the let binding",
						tokAssign.Span,
						"",
						fix.WithID(fixID),
						fix.WithKind(diag.FixKindRefactor),
						fix.WithApplicability(diag.FixApplicabilityAlwaysSafe), // todo подумать безопасно ли это
					)
					b.WithFixSuggestion(suggestion)
					b.WithNote(tokAssign.Span, "remove '=' to simplify the let binding")
				},
			)
			return LetBinding{}, false
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
				fixIDInsertColon := fmt.Sprintf(
					"%s-%d-%d",
					diag.SynExpectColon.ID(),
					spanWhereShouldBeColon.File,
					spanWhereShouldBeColon.Start,
				)
				suggestionInsertColon := fix.InsertText(
					"insert colon to add type annotation",
					spanWhereShouldBeColon,
					":",
					"",
					fix.WithID(fixIDInsertColon),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
					fix.Preferred(),
				)
				b.WithFixSuggestion(suggestionInsertColon)
				fixIDDeleteIdent := fmt.Sprintf(
					"%s-%d-%d",
					diag.SynExpectType.ID(),
					spanWhereUnexpectedIdent.File,
					spanWhereUnexpectedIdent.Start,
				)
				// и уже вторым фиксом предлагаем удалить ident
				suggestionDeleteIdent := fix.DeleteSpan(
					"remove ident to simplify the let binding",
					spanWhereUnexpectedIdent,
					"",
					fix.WithID(fixIDDeleteIdent),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
				)
				b.WithFixSuggestion(suggestionDeleteIdent)
				b.WithNote(spanWhereUnexpectedIdent, "insert colon to add type annotation or remove ident to simplify the let binding")
			},
		)
		return LetBinding{}, false
	}

	binding := LetBinding{
		Name:  nameID,
		Type:  typeID,
		Value: valueID,
		IsMut: isMut,
		Span:  startSpan.Cover(p.lastSpan),
	}

	return binding, true
}

// parseLetItem распознаёт let items верхнего уровня:
//
//	let [mut] name: Type = Expr;
//	let [mut] name: Type;
//	let [mut] name = Expr;
func (p *Parser) parseLetItem() (ast.ItemID, bool) {
	letTok := p.advance() // съедаем KwLet

	// Парсим биндинг
	binding, ok := p.parseLetBinding()
	if !ok {
		return ast.NoItemID, false
	}

	insertPos := p.lastSpan.ZeroideToEnd()

	semiTok, ok := p.expect(token.Semicolon, diag.SynExpectSemicolon, "expected semicolon after let item", func(b *diag.ReportBuilder) {
		if b == nil {
			return
		}
		fixID := fmt.Sprintf("%s-%d-%d", diag.SynExpectSemicolon.ID(), insertPos.File, insertPos.Start)
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
	})
	if !ok {
		p.resyncTop()
		return ast.NoItemID, false
	}

	// Создаем LetItem в AST
	finalSpan := letTok.Span.Cover(semiTok.Span)
	itemID := p.arenas.Items.NewLet(
		binding.Name,
		binding.Type,
		binding.Value,
		binding.IsMut,
		finalSpan,
	)

	return itemID, true
}
