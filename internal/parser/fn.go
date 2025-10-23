package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
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

	fnNameID, ok := p.parseIdent()
	if !ok {
		return ast.NoItemID, false
	}

	generics, ok := p.parseFnGenerics()
	if !ok {
		return ast.NoItemID, false
	}

	if _, ok := p.expect(token.LParen, diag.SynUnexpectedToken, "expected '(' after function name"); !ok {
		p.resyncUntil(token.LBrace, token.Semicolon)
		return ast.NoItemID, false
	}

	params, ok := p.parseFnParams()
	if !ok {
		return ast.NoItemID, false
	}

	var returnType ast.TypeID
	if p.at(token.Arrow) {
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
			p.resyncUntil(token.LBrace, token.Semicolon)
			return ast.NoItemID, false
		}
		returnType, ok = p.parseTypePrefix()
		if !ok {
			p.resyncUntil(token.LBrace, token.Semicolon)
			return ast.NoItemID, false
		}
	}

	if returnType == ast.NoTypeID {
		returnType = p.makeBuiltinType("nothing", p.lastSpan.ZeroideToEnd())
	}

	var bodyStmtID ast.StmtID
	if p.at(token.LBrace) {
		bodyStmtID, ok = p.parseBlock()
		if !ok {
			return ast.NoItemID, false
		}
	} else if p.at(token.Semicolon) {
		p.advance()
	} else {
		_, ok := p.expect(token.Semicolon, diag.SynExpectSemicolon, "expected ';' after function signature", func(b *diag.ReportBuilder) {
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
		if !ok {
			return ast.NoItemID, false
		}
	}

	fnItemID := p.arenas.NewFn(fnNameID, generics, params, returnType, bodyStmtID, 0, nil, fnTok.Span.Cover(p.lastSpan))
	return fnItemID, true
}

func (p *Parser) parseFnParam() (ast.FnParam, bool) {
	param := ast.FnParam{}

	nameID, ok := p.parseIdent()
	if !ok {
		return param, false
	}
	param.Name = nameID

	if _, ok := p.expect(token.Colon, diag.SynExpectColon, "expected ':' after parameter name"); !ok {
		p.resyncUntil(token.Comma, token.RParen, token.Semicolon)
		return param, false
	}

	typeID, ok := p.parseTypePrefix()
	if !ok {
		return param, false
	}
	param.Type = typeID

	if p.at(token.Assign) {
		p.advance()
		defaultExprID, ok := p.parseExpr()
		if !ok {
			p.resyncUntil(token.Comma, token.RParen, token.Semicolon)
			return param, false
		}
		param.Default = defaultExprID
	}

	return param, true
}

func (p *Parser) parseFnParams() ([]ast.FnParam, bool) {
	params := make([]ast.FnParam, 0)

	if p.at(token.RParen) {
		p.advance()
		return params, true
	}

	for {
		param, ok := p.parseFnParam()
		if !ok {
			p.resyncUntil(token.RParen, token.Semicolon)
			if p.at(token.RParen) {
				p.advance()
			}
			return nil, false
		}
		params = append(params, param)

		if p.at(token.Comma) {
			p.advance()
			if p.at(token.RParen) {
				p.advance()
				break
			}
			continue
		}

		if _, ok := p.expect(token.RParen, diag.SynUnclosedParen, "expected ')' after function parameters", func(b *diag.ReportBuilder) {
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
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert ')' to close the parameter list")
		}); !ok {
			p.resyncUntil(token.Semicolon)
			return params, false
		}
		break
	}

	return params, true
}

func (p *Parser) parseFnGenerics() ([]source.StringID, bool) {
	if !p.at(token.Lt) {
		return nil, true
	}

	p.advance()

	generics := make([]source.StringID, 0, 2)

	for {
		nameID, ok := p.parseIdent()
		if !ok {
			p.resyncUntil(token.Gt, token.LParen, token.Semicolon)
			if p.at(token.Gt) {
				p.advance()
			}
			return nil, false
		}

		generics = append(generics, nameID)

		if p.at(token.Comma) {
			p.advance()
			if p.at(token.Gt) {
				p.advance()
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
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert '>' to close the generic parameter list")
		}); !ok {
			p.resyncUntil(token.LParen, token.Semicolon)
			return generics, false
		}
		break
	}

	return generics, true
}
