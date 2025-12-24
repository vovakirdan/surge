package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

func (p *Parser) parseFnParam() (ast.FnParam, bool) {
	param := ast.FnParam{}
	attrs, attrSpan, ok := p.parseAttributes()
	if !ok {
		return param, false
	}
	if len(attrs) > 0 {
		param.AttrStart, param.AttrCount = p.arenas.Items.AllocateAttrs(attrs)
	}
	variadic := false

	startSpan := source.Span{}
	if attrSpan.End > attrSpan.Start {
		startSpan = attrSpan
	}
	if p.at(token.DotDotDot) {
		variadic = true
		dotsTok := p.advance()
		if startSpan.End > startSpan.Start {
			startSpan = startSpan.Cover(dotsTok.Span)
		} else {
			startSpan = dotsTok.Span
		}
	}

	nameID, ok := p.parseIdent()
	if !ok {
		return param, false
	}
	param.Name = nameID
	param.Variadic = variadic
	nameSpan := p.lastSpan
	if startSpan.End > startSpan.Start {
		startSpan = startSpan.Cover(nameSpan)
	} else {
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
			p.resyncUntil(token.RParen, token.Semicolon, token.LBrace, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract)
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
				p.resyncUntil(token.RParen, token.Semicolon, token.LBrace, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract)
				if p.at(token.RParen) {
					closeTok := p.advance()
					closeSpan = closeTok.Span
				}
				return
			}
			continue
		}

		if !expectClosing() {
			p.resyncUntil(token.Semicolon, token.LBrace, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract)
			return
		}
		break
	}

	isOk = true
	return
}
