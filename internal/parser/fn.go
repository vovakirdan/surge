package parser

import (
	"surge/internal/ast"
	"surge/internal/token"
	"surge/internal/diag"
	"surge/internal/fix"
)

// parseFnItem - парсит функцию
//
// fn Ident GenericParams? ParamList RetType? Block
// fn func(); // без параметров и возвращаемого значения
// fn func() -> Type; // с возвращаемым значением
// fn func() -> Type { ... } // с телом
// fn func(param: Type) { ... } // с параметрами и телом
// fn func(...params: Type) { ... } // с вариативными параметрами и телом
// fn func(param: Type, ...params: Type) { ... } // с параметрами и вариативными параметрами и телом
// fn func<T>(param: T) { ... } // с параметрами и телом с generic параметрами
func (p *Parser) parseFnItem() (ast.ItemID, bool) {
	// todo парсить атрибуты, хотя они перед fn...
	fnTok := p.advance() // съедаем KwFn; если мы здесь, то это точно KwFn

	// Парсим имя функции
	fnNameID, ok := p.parseIdent()
	if !ok {
		return ast.NoItemID, false
	}

	// todo: парсить generic параметры func<T>(param: T) и решить, что с ними делать

	// Парсим параметры функции
	p.expect(token.LParen, diag.SynUnexpectedToken, "expected '(' after function name", nil)
	params, ok := p.parseFnParams()
	if !ok {
		return ast.NoItemID, false
	}

	// По умолчанию возвращаем nothing
	var returnType ast.TypeID
	// Если есть -> то ожидаем тип возвращаемого значения
	if p.at(token.Arrow) {
		arrowTok := p.advance()
		if p.at(token.LBrace) {
			// Это мы попали на тело функции - явно предложим убрать ->
			p.emitDiagnostic(
				diag.SynUnexpectedToken,
				diag.SevError,
				arrowTok.Span,
				"expected type after '->' in function signature",
				func(b *diag.ReportBuilder) {
					if b == nil {
						return
					}
					fixID := fix.MakeFixID(diag.SynUnexpectedToken, arrowTok.Span)
					suggestion := fix.DeleteSpan(
						"remove '->' to simplify the function signature",
						arrowTok.Span,
						"",
						fix.WithID(fixID),
					)
					b.WithFixSuggestion(suggestion)
					b.WithNote(arrowTok.Span, "remove '->' to simplify the function signature")
				},
			)
			return ast.NoItemID, false
		}
		returnType, ok = p.parseTypePrefix() // надеюсь, он парсит весь тип
		if !ok {
			return ast.NoItemID, false
		}
	}
	if returnType == ast.NoTypeID { // зачем makeBuiltinType требует span?
		returnType = p.makeBuiltinType("nothing", p.lx.Peek().Span.Cover(p.lastSpan))
	}

	var bodyStmtID ast.StmtID
	if p.at(token.LBrace) {
		// Парсим тело функции
		bodyStmtID, ok = p.parseBlock()
		if !ok {
			return ast.NoItemID, false
		}
	} else if p.at(token.Semicolon) { // функция без тела
		p.advance()
	} else {
		p.expect(token.Semicolon, diag.SynExpectSemicolon, "expected ';' after function signature", func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynExpectSemicolon, p.lx.Peek().Span.Cover(p.lastSpan))
			suggestion := fix.InsertText(
				"insert ';' after function signature",
				p.lx.Peek().Span.Cover(p.lastSpan),
				";",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(p.lx.Peek().Span.Cover(p.lastSpan), "insert ';' after function signature")
		})
	}

	// Создаем функцию
	fnItemID := p.arenas.NewFn(fnNameID, params, returnType, bodyStmtID, 0, fnTok.Span.Cover(p.lastSpan))
	return fnItemID, true
}

func (p *Parser) parseFnParam() (ast.FnParamID, bool) {
	nameID, ok := p.parseIdent()
	if !ok {
		return ast.NoFnParamID, false
	}

	typeID, ok := p.parseTypePrefix() // а обязательно ли надо указывать тип?
	if !ok {
		return ast.NoFnParamID, false
	}

	defaultExprID := ast.NoExprID
	if p.at(token.Assign) {
		p.advance()
		defaultExprID, ok = p.parseExpr()
		if !ok {
			return ast.NoFnParamID, false
		}
	}

	return p.arenas.NewFnParam(nameID, typeID, defaultExprID), true
}

func (p *Parser) parseFnParams() ([]ast.FnParam, bool) {
	params := make([]ast.FnParam, 0)
	for !p.at(token.RParen) {
		paramID, ok := p.parseFnParam()
		if !ok {
			return nil, false
		}
		param := p.arenas.Items.FnParams.Get(uint32(paramID))
		params = append(params, *param)
		if !p.at(token.Comma) {
			break
		}
		p.advance()
	}
	return params, true
}
