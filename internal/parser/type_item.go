package parser

import (
	"fmt"
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

func (p *Parser) parseTypeItem(attrs []ast.Attr, attrSpan source.Span, visibility ast.Visibility, prefixSpan source.Span, hasPrefix bool) (ast.ItemID, bool) {
	typeTok := p.advance()

	startSpan := typeTok.Span
	if attrSpan.End > attrSpan.Start {
		startSpan = attrSpan.Cover(startSpan)
	}
	if hasPrefix {
		startSpan = prefixSpan.Cover(startSpan)
	}

	nameID, ok := p.parseIdent()
	if !ok {
		p.resyncUntil(token.Semicolon, token.KwType, token.KwFn, token.KwImport, token.KwLet, token.EOF)
		return ast.NoItemID, false
	}

	generics, ok := p.parseFnGenerics()
	if !ok {
		p.resyncUntil(token.Semicolon, token.KwType, token.KwFn, token.KwImport, token.KwLet, token.EOF)
		return ast.NoItemID, false
	}

	insertSpan := p.lastSpan.ZeroideToEnd()
	if _, ok := p.expect(token.Assign, diag.SynTypeExpectEquals, "expected '=' after type name", func(b *diag.ReportBuilder) {
		if b == nil {
			return
		}
		fixID := fix.MakeFixID(diag.SynTypeExpectEquals, insertSpan)
		suggestion := fix.InsertText(
			"insert '=' after type name",
			insertSpan,
			"=",
			"",
			fix.WithID(fixID),
			fix.WithKind(diag.FixKindRefactor),
			fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
		)
		b.WithFixSuggestion(suggestion)
		b.WithNote(insertSpan, "insert missing '='")
	}); !ok {
		p.resyncUntil(token.Semicolon, token.KwType, token.KwFn, token.KwImport, token.KwLet, token.EOF)
		return ast.NoItemID, false
	}

	if p.at(token.Semicolon) {
		p.emitDiagnostic(diag.SynTypeExpectBody, diag.SevError, p.lx.Peek().Span, "expected type body after '='", nil)
		return ast.NoItemID, false
	}

	switch p.lx.Peek().Kind {
	case token.LBrace:
		fields, bodySpan, ok := p.parseTypeStructBody()
		if !ok {
			return ast.NoItemID, false
		}
		itemSpan := startSpan.Cover(bodySpan)
		if p.at(token.Semicolon) {
			semiTok := p.advance()
			itemSpan = itemSpan.Cover(semiTok.Span)
		}
		itemID := p.arenas.NewTypeStruct(nameID, generics, attrs, visibility, ast.NoTypeID, fields, itemSpan)
		return itemID, true
	default:
		firstType, ok := p.parseTypePrefix()
		if !ok {
			p.resyncUntil(token.Semicolon, token.KwType, token.KwFn, token.KwImport, token.KwLet, token.EOF)
			return ast.NoItemID, false
		}
		firstTypeSpan := p.arenas.Types.Get(firstType).Span

		if p.at(token.Colon) {
			colonTok := p.advance()
			if !p.at(token.LBrace) {
				p.emitDiagnostic(diag.SynTypeExpectBody, diag.SevError, colonTok.Span.ZeroideToEnd(), "expected '{' to start struct body", nil)
				p.resyncUntil(token.Semicolon, token.KwType, token.KwFn, token.KwImport, token.KwLet, token.EOF)
				return ast.NoItemID, false
			}
			var fields []ast.TypeStructFieldSpec
			var bodySpan source.Span
			fields, bodySpan, ok = p.parseTypeStructBody()
			if !ok {
				return ast.NoItemID, false
			}
			itemSpan := startSpan.Cover(bodySpan)
			if p.at(token.Semicolon) {
				semiTok := p.advance()
				itemSpan = itemSpan.Cover(semiTok.Span)
			}
			itemID := p.arenas.NewTypeStruct(nameID, generics, attrs, visibility, firstType, fields, itemSpan)
			return itemID, true
		}

		if p.at(token.LParen) {
			var tagSpec ast.TypeUnionMemberSpec
			var tagSpan source.Span
			tagSpec, tagSpan, ok = p.parseUnionTagFromType(firstType)
			if !ok {
				return ast.NoItemID, false
			}
			members := []ast.TypeUnionMemberSpec{tagSpec}
			unionSpan := tagSpan
			members, _, ok = p.parseAdditionalUnionMembers(members, unionSpan)
			if !ok {
				return ast.NoItemID, false
			}
			var semiTok token.Token
			semiTok, ok = p.expect(token.Semicolon, diag.SynExpectSemicolon, "expected ';' after type declaration", func(b *diag.ReportBuilder) {
				if b == nil {
					return
				}
				insert := p.lastSpan.ZeroideToEnd()
				fixID := fix.MakeFixID(diag.SynExpectSemicolon, insert)
				suggestion := fix.InsertText(
					"insert ';' after type declaration",
					insert,
					";",
					"",
					fix.WithID(fixID),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
				)
				b.WithFixSuggestion(suggestion)
				b.WithNote(insert, "insert missing semicolon")
			})
			if !ok {
				return ast.NoItemID, false
			}
			itemSpan := startSpan.Cover(unionSpan)
			itemSpan = itemSpan.Cover(semiTok.Span)
			itemID := p.arenas.NewTypeUnion(nameID, generics, attrs, visibility, members, itemSpan)
			return itemID, true
		}

		if p.at(token.Pipe) {
			members := []ast.TypeUnionMemberSpec{{
				Kind: ast.TypeUnionMemberType,
				Type: firstType,
				Span: firstTypeSpan,
			}}
			unionSpan := firstTypeSpan
			members, unionSpan, ok = p.parseAdditionalUnionMembers(members, unionSpan)
			if !ok {
				return ast.NoItemID, false
			}
			var semiTok token.Token
			semiTok, ok = p.expect(token.Semicolon, diag.SynExpectSemicolon, "expected ';' after type declaration", func(b *diag.ReportBuilder) {
				if b == nil {
					return
				}
				insert := p.lastSpan.ZeroideToEnd()
				fixID := fix.MakeFixID(diag.SynExpectSemicolon, insert)
				suggestion := fix.InsertText(
					"insert ';' after type declaration",
					insert,
					";",
					"",
					fix.WithID(fixID),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
				)
				b.WithFixSuggestion(suggestion)
				b.WithNote(insert, "insert missing semicolon")
			})
			if !ok {
				return ast.NoItemID, false
			}
			itemSpan := startSpan.Cover(unionSpan)
			itemSpan = itemSpan.Cover(semiTok.Span)
			itemID := p.arenas.NewTypeUnion(nameID, generics, attrs, visibility, members, itemSpan)
			return itemID, true
		}

		// Alias
		if p.at(token.LBrace) {
			gapSpan := p.lastSpan.ZeroideToEnd().Cover(p.lx.Peek().Span.ZeroideToStart())
			p.emitDiagnostic(
				diag.SynExpectColon,
				diag.SevError,
				gapSpan,
				"expected ':' after type name to make inheritance",
				func(b *diag.ReportBuilder) {
					if b == nil {
						return
					}
					fixID := fix.MakeFixID(diag.SynExpectColon, gapSpan)
					suggestion := fix.ReplaceSpan(
						"insert ':' to make inheritance",
						gapSpan,
						" : ",
						"",
						fix.WithID(fixID),
						fix.WithKind(diag.FixKindRefactor),
						fix.WithApplicability(diag.FixApplicabilitySafeWithHeuristics),
					)
					b.WithFixSuggestion(suggestion)
					b.WithNote(gapSpan, "you can use a colon to make inheritance")
				},
			)
			p.resyncTop()
			return ast.NoItemID, false
		}
		semiTok, ok := p.expect(token.Semicolon, diag.SynExpectSemicolon, "expected ';' after type declaration", func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			insert := p.lastSpan.ZeroideToEnd()
			fixID := fix.MakeFixID(diag.SynExpectSemicolon, insert)
			suggestion := fix.InsertText(
				"insert ';' after type declaration",
				insert,
				";",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insert, "insert missing semicolon")
		})
		if !ok {
			return ast.NoItemID, false
		}
		itemSpan := startSpan.Cover(semiTok.Span)
		itemID := p.arenas.NewTypeAlias(nameID, generics, attrs, visibility, firstType, itemSpan)
		return itemID, true
	}
}

func (p *Parser) parseTypeStructBody() ([]ast.TypeStructFieldSpec, source.Span, bool) {
	openTok, ok := p.expect(token.LBrace, diag.SynTypeExpectBody, "expected '{' to start struct body", nil)
	if !ok {
		return nil, source.Span{}, false
	}

	fields := make([]ast.TypeStructFieldSpec, 0)
	fieldNames := make(map[source.StringID]source.Span)

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		var fieldAttrs []ast.Attr
		var fieldAttrSpan source.Span
		fieldAttrs, fieldAttrSpan, ok = p.parseAttributes()
		if !ok {
			p.resyncTypeStructField()
			continue
		}

		var nameID source.StringID
		nameID, ok = p.parseIdent()
		if !ok {
			p.resyncTypeStructField()
			continue
		}
		nameSpan := p.lastSpan

		if prevSpan, exists := fieldNames[nameID]; exists {
			fieldName := p.arenas.StringsInterner.MustLookup(nameID)
			p.emitDiagnostic(
				diag.SynTypeFieldConflict,
				diag.SevError,
				nameSpan,
				fmt.Sprintf("duplicate field '%s'", fieldName),
				func(b *diag.ReportBuilder) {
					if b == nil {
						return
					}
					b.WithNote(prevSpan, "previous declaration here")
				},
			)
		} else {
			fieldNames[nameID] = nameSpan
		}

		if _, ok = p.expect(token.Colon, diag.SynExpectColon, "expected ':' after field name", nil); !ok {
			p.resyncTypeStructField()
			continue
		}

		var fieldType ast.TypeID
		fieldType, ok = p.parseTypePrefix()
		if !ok {
			p.resyncTypeStructField()
			continue
		}
		fieldSpan := nameSpan.Cover(p.arenas.Types.Get(fieldType).Span)

		defaultExpr := ast.NoExprID
		if p.at(token.Assign) {
			assignTok := p.advance()
			var exprID ast.ExprID
			exprID, ok = p.parseExpr()
			if !ok {
				p.resyncTypeStructField()
				continue
			}
			defaultExpr = exprID
			exprSpan := p.arenas.Exprs.Get(exprID).Span
			fieldSpan = fieldSpan.Cover(assignTok.Span).Cover(exprSpan)
		}

		if fieldAttrSpan.End > fieldAttrSpan.Start {
			fieldSpan = fieldAttrSpan.Cover(fieldSpan)
		}

		fields = append(fields, ast.TypeStructFieldSpec{
			Name:    nameID,
			Type:    fieldType,
			Default: defaultExpr,
			Attrs:   fieldAttrs,
			Span:    fieldSpan,
		})

		if p.at(token.Comma) {
			p.advance()
			continue
		}

		if p.at(token.RBrace) {
			break
		}

		p.emitDiagnostic(diag.SynUnexpectedToken, diag.SevError, p.lx.Peek().Span, "expected ',' or '}' in struct body", nil)
		p.resyncTypeStructField()
	}

	closeTok, ok := p.expect(token.RBrace, diag.SynUnclosedBrace, "expected '}' to close struct body", nil)
	if !ok {
		return fields, openTok.Span, false
	}

	bodySpan := openTok.Span.Cover(closeTok.Span)
	return fields, bodySpan, true
}

func (p *Parser) parseAdditionalUnionMembers(initial []ast.TypeUnionMemberSpec, span source.Span) ([]ast.TypeUnionMemberSpec, source.Span, bool) {
	members := initial
	currentSpan := span

	for p.at(token.Pipe) {
		p.advance()
		member, memberSpan, ok := p.parseUnionMember()
		if !ok {
			p.emitDiagnostic(diag.SynTypeExpectUnionMember, diag.SevError, p.lx.Peek().Span, "expected union member after '|'", nil)
			p.resyncUnionMember()
			continue
		}
		members = append(members, member)
		currentSpan = currentSpan.Cover(memberSpan)
	}

	if len(members) == 0 {
		p.emitDiagnostic(diag.SynTypeExpectUnionMember, diag.SevError, p.lx.Peek().Span, "union must have at least one member", nil)
		return nil, currentSpan, false
	}

	return members, currentSpan, true
}

func (p *Parser) parseUnionMember() (ast.TypeUnionMemberSpec, source.Span, bool) {
	if p.at(token.NothingLit) {
		tok := p.advance()
		typeID := p.makeBuiltinType("nothing", tok.Span)
		return ast.TypeUnionMemberSpec{Kind: ast.TypeUnionMemberType, Type: typeID, Span: tok.Span}, tok.Span, true
	}

	typeID, ok := p.parseTypePrefix()
	if !ok {
		return ast.TypeUnionMemberSpec{}, source.Span{}, false
	}

	typeSpan := p.arenas.Types.Get(typeID).Span

	if p.at(token.LParen) {
		tagSpec, tagSpan, ok := p.parseUnionTagFromType(typeID)
		if !ok {
			return ast.TypeUnionMemberSpec{}, source.Span{}, false
		}
		return tagSpec, tagSpan, true
	}

	return ast.TypeUnionMemberSpec{Kind: ast.TypeUnionMemberType, Type: typeID, Span: typeSpan}, typeSpan, true
}

func (p *Parser) parseUnionTagFromType(typeID ast.TypeID) (ast.TypeUnionMemberSpec, source.Span, bool) {
	nameID, ok := p.extractTagName(typeID)
	if !ok {
		p.emitDiagnostic(diag.SynUnexpectedToken, diag.SevError, p.lx.Peek().Span, "invalid tag name in union member", nil)
		return ast.TypeUnionMemberSpec{}, source.Span{}, false
	}

	if _, ok = p.expect(token.LParen, diag.SynUnexpectedToken, "expected '(' after tag name", nil); !ok {
		return ast.TypeUnionMemberSpec{}, source.Span{}, false
	}

	args := make([]ast.TypeID, 0)

	if !p.at(token.RParen) {
		for {
			var argType ast.TypeID
			argType, ok = p.parseTypePrefix()
			if !ok {
				p.resyncUntil(token.Comma, token.RParen, token.Pipe, token.Semicolon, token.EOF)
				if p.at(token.Comma) {
					p.advance()
				}
				if p.at(token.RParen) {
					p.advance()
				}
				return ast.TypeUnionMemberSpec{}, source.Span{}, false
			}
			args = append(args, argType)

			if p.at(token.Comma) {
				p.advance()
				if p.at(token.RParen) {
					break
				}
				continue
			}
			break
		}
	}

	closeTok, ok := p.expect(token.RParen, diag.SynUnclosedParen, "expected ')' to close tag arguments", nil)
	if !ok {
		return ast.TypeUnionMemberSpec{}, source.Span{}, false
	}

	memberSpan := p.arenas.Types.Get(typeID).Span.Cover(closeTok.Span)

	return ast.TypeUnionMemberSpec{
		Kind:    ast.TypeUnionMemberTag,
		TagName: nameID,
		TagArgs: args,
		Span:    memberSpan,
	}, memberSpan, true
}

func (p *Parser) extractTagName(typeID ast.TypeID) (source.StringID, bool) {
	typ := p.arenas.Types.Get(typeID)
	if typ == nil || typ.Kind != ast.TypeExprPath {
		return source.NoStringID, false
	}
	path, ok := p.arenas.Types.Path(typeID)
	if !ok || path == nil || len(path.Segments) == 0 {
		return source.NoStringID, false
	}
	segment := path.Segments[len(path.Segments)-1]
	if len(segment.Generics) != 0 {
		return source.NoStringID, false
	}
	return segment.Name, true
}

func (p *Parser) resyncTypeStructField() {
	p.resyncUntil(token.Comma, token.RBrace, token.Semicolon, token.KwType, token.KwFn, token.KwImport, token.KwLet, token.EOF)
	if p.at(token.Comma) {
		p.advance()
	}
}

func (p *Parser) resyncUnionMember() {
	p.resyncUntil(token.Pipe, token.Semicolon, token.KwType, token.KwFn, token.KwImport, token.KwLet, token.EOF)
	if p.at(token.Pipe) {
		p.advance()
	}
}
