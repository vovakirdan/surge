package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

type fnModifiers struct {
	flags     ast.FnModifier
	span      source.Span
	hasSpan   bool
	seenPub   bool
	seenAsync bool
}

func (m *fnModifiers) extend(sp source.Span) {
	if !m.hasSpan {
		m.span = sp
		m.hasSpan = true
		return
	}
	m.span = m.span.Cover(sp)
}

type parsedFn struct {
	name             source.StringID
	nameSpan         source.Span
	generics         []source.StringID
	genericCommas    []source.Span
	genericsSpan     source.Span
	genericsTrailing bool
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

func (p *Parser) parseFnModifiers() (fnModifiers, bool) {
	mods := fnModifiers{}

	for {
		tok := p.lx.Peek()
		switch tok.Kind {
		case token.KwFn:
			return mods, true
		case token.KwPub:
			tok = p.advance()
			if mods.seenPub {
				p.emitDiagnostic(
					diag.SynUnexpectedToken,
					diag.SevError,
					tok.Span,
					"duplicate 'pub' modifier",
					nil,
				)
			} else {
				mods.seenPub = true
				mods.flags |= ast.FnModifierPublic
			}
			mods.extend(tok.Span)

		case token.KwAsync:
			tok = p.advance()
			if mods.seenAsync {
				p.emitDiagnostic(
					diag.SynUnexpectedToken,
					diag.SevError,
					tok.Span,
					"duplicate 'async' modifier",
					func(b *diag.ReportBuilder) {
						if b == nil {
							return
						}
						fixID := fix.MakeFixID(diag.SynUnexpectedToken, tok.Span)
						suggestion := fix.DeleteSpan(
							"remove the duplicate 'async' modifier",
							tok.Span.ExtendRight(p.lx.Peek().Span),
							"",
							fix.WithID(fixID),
							fix.WithKind(diag.FixKindRefactor),
							fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
						)
						b.WithFixSuggestion(suggestion)
						b.WithNote(tok.Span, "Async modifier can be only once")
					},
				)
			} else {
				mods.seenAsync = true
				mods.flags |= ast.FnModifierAsync
			}
			mods.extend(tok.Span)

		case token.KwExtern:
			tok = p.advance()
			p.emitDiagnostic(
				diag.SynUnexpectedToken,
				diag.SevError,
				tok.Span,
				"'extern' cannot be used as a function modifier",
				nil,
			)
			mods.extend(tok.Span)
			continue
		case token.Ident:
			tok = p.advance()
			msg := "unknown function modifier"
			note := "Possible fn modifier: pub, async"
			if tok.Text == "unsafe" {
				msg = "'unsafe' must be specified via attribute"
				note = "'unsafe' should be declared via attribute before the function"
			} else if tok.Text != "" {
				msg = "unknown function modifier '" + tok.Text + "'"
			}
			p.emitDiagnostic(
				diag.SynUnexpectedModifier,
				diag.SevError,
				tok.Span,
				msg,
				func(b *diag.ReportBuilder) {
					if b == nil {
						return
					}
					fixID := fix.MakeFixID(diag.SynUnexpectedModifier, tok.Span)
					suggestion := fix.DeleteSpan(
						"remove the unknown function modifier",
						tok.Span.ExtendRight(p.lx.Peek().Span),
						"",
						fix.WithID(fixID),
						fix.WithKind(diag.FixKindRefactor),
						fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
					)
					b.WithFixSuggestion(suggestion)
					b.WithNote(tok.Span, note)
				},
			)
			mods.extend(tok.Span)
			continue
		case token.EOF:
			return mods, false
		default:
			if isTopLevelStarter(tok.Kind) || tok.Kind == token.Semicolon {
				return mods, false
			}
			return mods, false
		}
	}
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
	fnData, ok := p.parseFnDefinition(attrs, attrSpan, mods)
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

func (p *Parser) parseFnDefinition(attrs []ast.Attr, attrSpan source.Span, mods fnModifiers) (parsedFn, bool) {
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
	preGenerics, preCommas, preTrailing, preSpan, ok := p.parseFnGenerics()
	if !ok {
		p.resyncUntil(token.Ident, token.Semicolon, token.KwFn, token.KwImport, token.KwLet)
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
			if _, _, _, _, ok = p.parseFnGenerics(); !ok {
				p.resyncUntil(token.LParen, token.Semicolon, token.KwFn, token.KwImport, token.KwLet)
				return parsedFn{}, false
			}
		}
	} else {
		generics, genericCommas, genericsTrailing, genericsSpan, ok = p.parseFnGenerics()
		if !ok {
			p.resyncUntil(token.LParen, token.Semicolon, token.KwFn, token.KwImport, token.KwLet)
			return parsedFn{}, false
		}
	}

	openParen, ok := p.expect(token.LParen, diag.SynUnexpectedToken, "expected '(' after function name")
	if !ok {
		p.resyncUntil(token.LBrace, token.Semicolon, token.KwFn, token.KwImport, token.KwLet)
		return parsedFn{}, false
	}

	params, commas, trailing, closeParenSpan, ok := p.parseFnParams()
	if !ok {
		p.resyncUntil(token.Semicolon, token.LBrace, token.KwFn, token.KwImport, token.KwLet)
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
			p.resyncUntil(token.LBrace, token.Semicolon, token.KwFn, token.KwImport, token.KwLet)
			return parsedFn{}, false
		}
		returnType, ok = p.parseTypePrefix()
		if !ok {
			p.resyncUntil(token.LBrace, token.Semicolon, token.KwFn, token.KwImport, token.KwLet)
			return parsedFn{}, false
		}
		typeSpan := p.arenas.Types.Get(returnType).Span
		result.returnSpan = arrowTok.Span.Cover(typeSpan)
	}

	if returnType == ast.NoTypeID {
		returnType = p.makeBuiltinType("nothing", p.lastSpan.ZeroideToEnd())
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
	result.params = params
	result.paramCommas = commas
	result.paramsTrailing = trailing
	result.returnType = returnType
	result.body = bodyStmtID
	result.flags = flags
	result.span = startSpan.Cover(p.lastSpan)

	return result, true
}

func (p *Parser) parseFnParam() (ast.FnParam, bool) {
	param := ast.FnParam{}
	variadic := false

	startSpan := source.Span{}
	if p.at(token.DotDotDot) {
		variadic = true
		dotsTok := p.advance()
		startSpan = dotsTok.Span
	}

	nameID, ok := p.parseIdent()
	if !ok {
		return param, false
	}
	param.Name = nameID
	param.Variadic = variadic
	nameSpan := p.lastSpan
	if !variadic {
		startSpan = nameSpan
	}

	colonTok, colonOK := p.expect(token.Colon, diag.SynExpectColon, "expected ':' after parameter name")
	if !colonOK {
		p.resyncUntil(token.Comma, token.RParen, token.Semicolon)
		return param, false
	}

	var typeID ast.TypeID
	typeID, ok = p.parseTypePrefix()
	if !ok {
		return param, false
	}
	param.Type = typeID

	currentSpan := startSpan.Cover(colonTok.Span)
	typeSpan := p.arenas.Types.Get(typeID).Span
	currentSpan = currentSpan.Cover(typeSpan)

	if p.at(token.Assign) {
		assignTok := p.advance()
		var defaultExprID ast.ExprID
		defaultExprID, ok = p.parseExpr()
		if !ok {
			p.resyncUntil(token.Comma, token.RParen, token.Semicolon)
			return param, false
		}
		param.Default = defaultExprID
		currentSpan = currentSpan.Cover(assignTok.Span)
		if expr := p.arenas.Exprs.Get(defaultExprID); expr != nil {
			currentSpan = currentSpan.Cover(expr.Span)
		}
	}

	param.Span = currentSpan
	return param, true
}

func (p *Parser) parseFnParams() (params []ast.FnParam, commas []source.Span, trailing bool, closeSpan source.Span, isOk bool) {
	params = make([]ast.FnParam, 0)
	commas = make([]source.Span, 0, 2)
	var sawVariadic bool

	// если нет параметров, но забыли скобку
	if p.atOr(token.LBrace, token.Arrow, token.Semicolon) {
		// забыли закрыть скобку с пустыми аргами
		p.emitDiagnostic(
			diag.SynUnclosedParen,
			diag.SevError,
			p.lastSpan,
			"expected ')' after function parameters",
			func(b *diag.ReportBuilder) {
				if b == nil {
					return
				}
				insertSpan := p.lastSpan.ZeroideToEnd()
				fixID := fix.MakeFixID(diag.SynUnclosedParen, insertSpan)
				suggestion := fix.InsertText(
					"insert ')' to close the parameter list",
					insertSpan,
					")",
					"",
					fix.WithID(fixID),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
				)
				b.WithFixSuggestion(suggestion)
				b.WithNote(insertSpan, "insert ')' to close the parameter list")
			},
		)
		return
	}

	if p.at(token.RParen) {
		closeTok := p.advance()
		closeSpan = closeTok.Span
		isOk = true
		return
	}

	expectClosing := func() bool {
		closeTok, ok := p.expect(token.RParen, diag.SynUnclosedParen, "expected ')' after function parameters", func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			insertSpan := p.lastSpan.ZeroideToEnd()
			fixID := fix.MakeFixID(diag.SynUnclosedParen, insertSpan)
			suggestion := fix.InsertText(
				"insert ')' to close the parameter list",
				insertSpan,
				")",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert ')' to close the parameter list")
		})
		if ok {
			closeSpan = closeTok.Span
		}
		return ok
	}

	for {
		param, paramOK := p.parseFnParam()
		if !paramOK {
			p.resyncUntil(token.RParen, token.Semicolon, token.LBrace, token.KwFn, token.KwImport, token.KwLet)
			if p.at(token.RParen) {
				p.advance()
			}
			params = nil
			commas = nil
			return
		}
		params = append(params, param)
		if param.Variadic && sawVariadic {
			p.err(diag.SynUnexpectedToken, "multiple variadic parameters are not allowed")
		}
		if param.Variadic {
			sawVariadic = true
		}

		if p.at(token.Comma) {
			commaTok := p.advance()
			commas = append(commas, commaTok.Span)
			if p.at(token.RParen) {
				closeTok := p.advance()
				closeSpan = closeTok.Span
				trailing = true
				break
			}
			if sawVariadic {
				p.emitDiagnostic(
					diag.SynUnexpectedToken,
					diag.SevError,
					commaTok.Span,
					"variadic parameter must be the last parameter in the list",
					nil,
				)
				p.resyncUntil(token.RParen, token.Semicolon, token.LBrace, token.KwFn, token.KwImport, token.KwLet)
				if p.at(token.RParen) {
					closeTok := p.advance()
					closeSpan = closeTok.Span
				}
				return
			}
			continue
		}

		if !expectClosing() {
			p.resyncUntil(token.Semicolon, token.LBrace, token.KwFn, token.KwImport, token.KwLet)
			return
		}
		break
	}

	isOk = true
	return
}

func (p *Parser) parseFnGenerics() (generics []source.StringID, commas []source.Span, trailing bool, span source.Span, ok bool) {
	if !p.at(token.Lt) {
		return nil, nil, false, source.Span{}, true
	}

	ltTok := p.advance()

	generics = make([]source.StringID, 0, 2)
	commas = make([]source.Span, 0, 2)

	for {
		nameID, ok := p.parseIdent()
		if !ok {
			p.resyncUntil(token.Gt, token.LParen, token.Semicolon, token.KwFn, token.KwLet, token.KwType, token.KwTag, token.KwImport)
			if p.at(token.Gt) {
				p.advance()
			}
			return nil, nil, false, source.Span{}, false
		}

		generics = append(generics, nameID)

		if p.at(token.Comma) {
			commaTok := p.advance()
			commas = append(commas, commaTok.Span)
			if p.at(token.Gt) {
				p.advance()
				trailing = true
				break
			}
			continue
		}

		if _, ok := p.expect(token.Gt, diag.SynUnclosedAngleBracket, "expected '>' after generic parameter list", func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			insertSpan := p.lastSpan.ZeroideToEnd()
			fixID := fix.MakeFixID(diag.SynUnclosedAngleBracket, insertSpan)
			suggestion := fix.InsertText(
				"insert '>' to close the generic parameter list",
				insertSpan,
				">",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert '>' to close the generic parameter list")
		}); !ok {
			p.resyncUntil(token.LParen, token.Semicolon, token.KwFn, token.KwLet, token.KwType, token.KwTag, token.KwImport)
			return generics, commas, trailing, source.Span{}, false
		}
		break
	}

	span = ltTok.Span.Cover(p.lastSpan)
	return generics, commas, trailing, span, true
}
