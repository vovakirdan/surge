package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

type parsedFn struct {
	name             source.StringID
	nameSpan         source.Span
	generics         []source.StringID
	genericCommas    []source.Span
	genericsSpan     source.Span
	genericsTrailing bool
	typeParams       []ast.TypeParamSpec
	params           []ast.FnParam
	paramCommas      []source.Span
	paramsTrailing   bool
	fnKwSpan         source.Span
	paramsSpan       source.Span
	returnSpan       source.Span
	semicolonSpan    source.Span
	returnType       ast.TypeID
	body             ast.StmtID
	flags            ast.FnModifier
	span             source.Span
}

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
// fn func<T, U>(param: T, ...params: U) { ... } // с параметрами и вариативными параметрами и телом с generic параметрами
// fn @attr fn func() { ... } // с атрибутами и телом
// modifier fn func() { ... } // с модификаторами и телом
func (p *Parser) parseFnItem(attrs []ast.Attr, attrSpan source.Span, mods fnModifiers) (ast.ItemID, bool) {
	fnData, ok := p.parseFnDefinition(attrSpan, mods)
	if !ok {
		return ast.NoItemID, false
	}
	fnItemID := p.arenas.NewFn(
		fnData.name,
		fnData.nameSpan,
		fnData.generics,
		fnData.genericCommas,
		fnData.genericsTrailing,
		fnData.genericsSpan,
		fnData.typeParams,
		fnData.params,
		fnData.paramCommas,
		fnData.paramsTrailing,
		fnData.fnKwSpan,
		fnData.paramsSpan,
		fnData.returnSpan,
		fnData.semicolonSpan,
		fnData.returnType,
		fnData.body,
		fnData.flags,
		attrs,
		fnData.span,
	)
	return fnItemID, true
}

func (p *Parser) parseFnDefinition(attrSpan source.Span, mods fnModifiers) (parsedFn, bool) {
	result := parsedFn{}

	fnTok := p.advance() // съедаем KwFn; если мы здесь, то это точно KwFn
	result.fnKwSpan = fnTok.Span

	startSpan := fnTok.Span
	if attrSpan.End > attrSpan.Start {
		startSpan = attrSpan.Cover(startSpan)
	}
	if mods.hasSpan {
		startSpan = mods.span.Cover(startSpan)
	}

	flags := mods.flags

	// Допускаем Rust-подобный синтаксис: fn <T, U> name(...)
	preTypeParams, preGenerics, preCommas, preTrailing, preSpan, ok := p.parseFnGenerics()
	if !ok {
		p.resyncUntil(token.Ident, token.Semicolon, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract)
		return parsedFn{}, false
	}

	fnNameID, ok := p.parseIdent()
	if !ok {
		return parsedFn{}, false
	}
	fnNameSpan := p.lastSpan

	generics := preGenerics
	genericCommas := preCommas
	genericsTrailing := preTrailing
	genericsSpan := preSpan
	typeParams := preTypeParams

	if len(generics) > 0 {
		// Если generics уже были до имени, запрещаем второе объявление.
		if p.at(token.Lt) {
			dupSpan := p.lx.Peek().Span
			p.emitDiagnostic(
				diag.SynUnexpectedToken,
				diag.SevError,
				dupSpan,
				"duplicate generic parameter list for function",
				nil,
			)
			// Пробуем съесть второе объявление, чтобы не застрять.
			if _, _, _, _, _, ok = p.parseFnGenerics(); !ok {
				p.resyncUntil(token.LParen, token.Semicolon, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract)
				return parsedFn{}, false
			}
		}
	} else {
		typeParams, generics, genericCommas, genericsTrailing, genericsSpan, ok = p.parseFnGenerics()
		if !ok {
			p.resyncUntil(token.LParen, token.Semicolon, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract)
			return parsedFn{}, false
		}
	}

	openParen, ok := p.expect(token.LParen, diag.SynUnexpectedToken, "expected '(' after function name")
	if !ok {
		p.resyncUntil(token.LBrace, token.Semicolon, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract)
		return parsedFn{}, false
	}

	params, commas, trailing, closeParenSpan, ok := p.parseFnParams()
	if !ok {
		p.resyncUntil(token.Semicolon, token.LBrace, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract)
		return parsedFn{}, false
	}
	result.paramsSpan = openParen.Span.Cover(closeParenSpan)

	var returnType ast.TypeID
	if p.at(token.Arrow) {
		prevSpan := p.lastSpan.ZeroideToEnd()
		arrowTok := p.advance()
		if p.at(token.LBrace) {
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
					suggestion := fix.ReplaceSpan(
						"remove '->' to simplify the function signature",
						prevSpan.Cover(arrowTok.Span).ExtendRight(p.lx.Peek().Span), // что бы взять всё вокруг
						" ",
						"",
						fix.WithID(fixID),
						fix.WithKind(diag.FixKindRefactor),
						fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
					)
					b.WithFixSuggestion(suggestion)
					b.WithNote(arrowTok.Span, "remove '->' to simplify the function signature")
				},
			)
			p.resyncUntil(token.LBrace, token.Semicolon, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract)
			return parsedFn{}, false
		}
		returnType, ok = p.parseTypePrefix()
		if !ok {
			p.resyncUntil(token.LBrace, token.Semicolon, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract)
			return parsedFn{}, false
		}
		typeSpan := p.arenas.Types.Get(returnType).Span
		result.returnSpan = arrowTok.Span.Cover(typeSpan)
	}

	if returnType == ast.NoTypeID {
		returnType = p.makeNothingType(p.lastSpan.ZeroideToEnd())
	}

	var bodyStmtID ast.StmtID
	switch p.lx.Peek().Kind {
	case token.LBrace:
		bodyStmtID, ok = p.parseBlock()
		if !ok {
			return parsedFn{}, false
		}
	case token.Semicolon:
		semiTok := p.advance()
		result.semicolonSpan = semiTok.Span
	default:
		semiTok, okSemicolon := p.expect(token.Semicolon, diag.SynExpectSemicolon, "expected ';' after function signature", func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			insertSpan := p.lastSpan.ZeroideToEnd()
			fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertSpan)
			suggestion := fix.InsertText(
				"insert ';' after function signature",
				insertSpan,
				";",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert ';' after function signature")
		})
		if !okSemicolon {
			return parsedFn{}, false
		}
		result.semicolonSpan = semiTok.Span
	}

	result.name = fnNameID
	result.nameSpan = fnNameSpan
	result.generics = generics
	result.genericCommas = genericCommas
	result.genericsTrailing = genericsTrailing
	result.genericsSpan = genericsSpan
	result.typeParams = typeParams
	result.params = params
	result.paramCommas = commas
	result.paramsTrailing = trailing
	result.returnType = returnType
	result.body = bodyStmtID
	result.flags = flags
	result.span = startSpan.Cover(p.lastSpan)

	return result, true
}
