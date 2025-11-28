package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

//nolint:gocritic // returning multiple values keeps the parser flow and diagnostics handling explicit.
func (p *Parser) parseFnGenerics() (params []ast.TypeParamSpec, names []source.StringID, commas []source.Span, trailing bool, span source.Span, ok bool) {
	if !p.at(token.Lt) {
		return nil, nil, nil, false, source.Span{}, true
	}

	ltTok := p.advance()

	params = make([]ast.TypeParamSpec, 0, 2)
	names = make([]source.StringID, 0, 2)
	commas = make([]source.Span, 0, 2)

	parseBounds := func() ([]ast.TypeParamBoundSpec, []source.Span, source.Span, bool) {
		bounds := make([]ast.TypeParamBoundSpec, 0, 2)
		plusSpans := make([]source.Span, 0, 1)
		var boundsSpan source.Span

		parseOne := func() (ast.TypeParamBoundSpec, bool) {
			bound := ast.TypeParamBoundSpec{}
			typ, ok := p.parseTypePrefix()
			if !ok || typ == ast.NoTypeID {
				return bound, false
			}
			bound.Type = typ
			if path, okPath := p.arenas.Types.Path(typ); okPath && path != nil && len(path.Segments) > 0 {
				last := path.Segments[len(path.Segments)-1]
				bound.Name = last.Name
				bound.TypeArgs = append(bound.TypeArgs, last.Generics...)
			}
			bound.Span = p.arenas.Types.Get(typ).Span

			if p.at(token.Lt) {
				argsLtTok := p.advance()
				typeArgs := make([]ast.TypeID, 0, 2)
				argCommas := make([]source.Span, 0, 2)
				var argsSpan source.Span
				for {
					argTyp, ok := p.parseTypePrefix()
					if !ok {
						p.resyncUntil(token.Comma, token.Gt, token.Plus, token.KwFn, token.KwLet, token.KwConst, token.KwType, token.KwTag, token.KwImport, token.KwContract)
						if p.at(token.Gt) {
							p.advance()
						}
						return bound, false
					}
					typeArgs = append(typeArgs, argTyp)
					if argsSpan == (source.Span{}) {
						argsSpan = p.arenas.Types.Get(argTyp).Span
					} else {
						argsSpan = argsSpan.Cover(p.arenas.Types.Get(argTyp).Span)
					}

					if p.at(token.Comma) {
						commaTok := p.advance()
						argCommas = append(argCommas, commaTok.Span)
						continue
					}

					if closeTok, ok := p.consumeTypeArgClose(); ok {
						argsSpan = argsLtTok.Span.Cover(closeTok.Span)
						break
					}

					// If the closing '>' was already consumed by the type parser, accept common follower tokens.
					switch p.lx.Peek().Kind {
					case token.Plus, token.Comma, token.RParen, token.Semicolon, token.EOF:
						argsSpan = argsLtTok.Span.Cover(p.lastSpan)
					default:
						p.emitDiagnostic(
							diag.SynUnclosedAngleBracket,
							diag.SevError,
							p.lx.Peek().Span,
							"expected '>' after contract type arguments",
							nil,
						)
						p.resyncUntil(token.Plus, token.Comma, token.Gt, token.KwFn, token.KwLet, token.KwConst, token.KwType, token.KwTag, token.KwImport, token.KwContract)
						return bound, false
					}
					break
				}
				bound.TypeArgs = typeArgs
				bound.ArgCommas = argCommas
				bound.ArgsSpan = argsSpan
				bound.Span = bound.Span.Cover(argsSpan)
			}

			return bound, true
		}

		firstBound, ok := parseOne()
		if !ok {
			return nil, nil, source.Span{}, false
		}
		bounds = append(bounds, firstBound)
		boundsSpan = firstBound.Span

		for p.at(token.Plus) {
			plusTok := p.advance()
			plusSpans = append(plusSpans, plusTok.Span)
			next, boundOK := parseOne()
			if !boundOK {
				p.resyncUntil(token.Comma, token.Gt, token.KwFn, token.KwLet, token.KwConst, token.KwType, token.KwTag, token.KwImport, token.KwContract)
				return nil, nil, source.Span{}, false
			}
			bounds = append(bounds, next)
			boundsSpan = boundsSpan.Cover(next.Span)
		}

		return bounds, plusSpans, boundsSpan, true
	}

	for {
		paramSpec := ast.TypeParamSpec{}
		if p.at(token.KwConst) {
			constTok := p.advance()
			paramSpec.IsConst = true
			nameID, okName := p.parseIdent()
			if !okName {
				p.resyncUntil(token.Gt, token.LParen, token.Semicolon, token.KwFn, token.KwLet, token.KwConst, token.KwType, token.KwTag, token.KwImport, token.KwContract)
				if p.at(token.Gt) {
					p.advance()
				}
				return nil, nil, nil, false, source.Span{}, false
			}
			paramSpec.Name = nameID
			paramSpec.NameSpan = p.lastSpan
			paramSpec.Span = constTok.Span.Cover(paramSpec.NameSpan)
			if colonTok, okColon := p.expect(token.Colon, diag.SynUnexpectedToken, "expected ':' after const generic name", nil); okColon {
				paramSpec.ColonSpan = colonTok.Span
				if typ, okType := p.parseTypePrefix(); okType {
					paramSpec.ConstType = typ
					if texpr := p.arenas.Types.Get(typ); texpr != nil {
						paramSpec.Span = paramSpec.Span.Cover(texpr.Span)
					}
				} else {
					return nil, nil, nil, false, source.Span{}, false
				}
			} else {
				return nil, nil, nil, false, source.Span{}, false
			}
		} else {
			nameID, okName := p.parseIdent()
			if !okName {
				p.resyncUntil(token.Gt, token.LParen, token.Semicolon, token.KwFn, token.KwLet, token.KwConst, token.KwType, token.KwTag, token.KwImport, token.KwContract)
				if p.at(token.Gt) {
					p.advance()
				}
				return nil, nil, nil, false, source.Span{}, false
			}

			paramSpec.Name = nameID
			paramSpec.NameSpan = p.lastSpan
			paramSpec.Span = paramSpec.NameSpan

			if p.at(token.Colon) {
				colonTok := p.advance()
				paramSpec.ColonSpan = colonTok.Span
				bounds, plusSpans, boundsSpan, okBounds := parseBounds()
				if !okBounds {
					return nil, nil, nil, false, source.Span{}, false
				}
				paramSpec.Bounds = bounds
				paramSpec.PlusSpans = plusSpans
				paramSpec.BoundsSpan = boundsSpan
				paramSpec.Span = paramSpec.Span.Cover(boundsSpan)
			}
		}

		names = append(names, paramSpec.Name)

		params = append(params, paramSpec)

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
			p.resyncUntil(token.LParen, token.Semicolon, token.KwFn, token.KwLet, token.KwConst, token.KwType, token.KwTag, token.KwImport, token.KwContract)
			return params, names, commas, trailing, source.Span{}, false
		}
		break
	}

	span = ltTok.Span.Cover(p.lastSpan)
	return params, names, commas, trailing, span, true
}
