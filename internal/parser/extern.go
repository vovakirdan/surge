package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

func (p *Parser) parseExternItem(attrs []ast.Attr, attrSpan source.Span) (ast.ItemID, bool) {
	externTok := p.advance()

	startSpan := externTok.Span
	if attrSpan.End > attrSpan.Start {
		startSpan = attrSpan.Cover(startSpan)
	}

	if _, ok := p.expect(token.Lt, diag.SynUnexpectedToken, "expected '<' after 'extern'"); !ok {
		p.resyncUntil(token.RBrace, token.KwExtern, token.KwFn, token.KwField)
		return ast.NoItemID, false
	}

	targetType, ok := p.parseTypePrefix()
	if !ok {
		p.resyncUntil(token.Gt, token.RBrace, token.KwFn, token.KwField)
		if p.at(token.Gt) {
			p.advance()
		}
		if !p.at(token.LBrace) {
			return ast.NoItemID, false
		}
	}

	if _, ok = p.expect(token.Gt, diag.SynUnexpectedToken, "expected '>' after extern target type"); !ok {
		p.resyncUntil(token.LBrace, token.RBrace, token.KwFn, token.KwField)
		if !p.at(token.LBrace) {
			return ast.NoItemID, false
		}
	}

	if _, ok = p.expect(token.LBrace, diag.SynUnexpectedToken, "expected '{' to start extern block"); !ok {
		p.resyncUntil(token.RBrace, token.KwExtern)
		return ast.NoItemID, false
	}

	members, okMembers := p.parseExternMembers()

	closeTok, ok := p.expect(
		token.RBrace,
		diag.SynUnclosedBrace,
		"expected '}' to close extern block",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			insertSpan := p.lastSpan.ZeroideToEnd()
			fixID := fix.MakeFixID(diag.SynUnclosedBrace, insertSpan)
			suggestion := fix.InsertText(
				"insert '}' to close extern block",
				insertSpan,
				"}",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert missing closing brace for extern block")
		},
	)
	if !ok {
		return ast.NoItemID, false
	}

	if !okMembers {
		return ast.NoItemID, false
	}

	itemSpan := startSpan.Cover(closeTok.Span)
	itemID := p.arenas.NewExtern(targetType, attrs, members, itemSpan)
	return itemID, true
}

func (p *Parser) parseExternMembers() ([]ast.ExternMemberSpec, bool) {
	members := make([]ast.ExternMemberSpec, 0)
	hasFatalError := false

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		memberAttrs, attrSpan, ok := p.parseAttributes()
		if !ok {
			hasFatalError = true
			p.resyncExternMember()
			continue
		}

		mods := p.parseFnModifiers()
		switch {
		case p.at(token.KwFn):
			fnData, ok := p.parseFnDefinition(attrSpan, mods)
			if !ok {
				hasFatalError = true
				p.resyncExternMember()
				continue
			}

			if p.hasOverrideWithoutPub(memberAttrs, fnData.flags) {
				p.emitDiagnostic(
					diag.SynVisibilityReduction,
					diag.SevError,
					fnData.span,
					"@override methods must preserve public visibility; add 'pub'",
					nil,
				)
			}

			fnPayload := p.arenas.NewExternFn(
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
				memberAttrs,
				fnData.span,
			)
			members = append(members, ast.ExternMemberSpec{
				Kind: ast.ExternMemberFn,
				Fn:   fnPayload,
				Span: fnData.span,
			})
		case p.at(token.KwField):
			if mods.flags != 0 {
				span := mods.span
				if !mods.hasSpan {
					span = p.lx.Peek().Span
				}
				p.emitDiagnostic(
					diag.SynModifierNotAllowed,
					diag.SevError,
					span,
					"modifiers are not allowed before 'field' in an extern block",
					nil,
				)
				hasFatalError = true
				p.resyncExternMember()
				continue
			}
			fieldSpec, ok := p.parseExternField(memberAttrs, attrSpan)
			if !ok {
				hasFatalError = true
				p.resyncExternMember()
				continue
			}
			members = append(members, fieldSpec)
		default:
			tok := p.lx.Peek()
			msg := "only 'fn' or 'field' members are allowed inside extern blocks"
			if len(memberAttrs) > 0 && attrSpan.End > attrSpan.Start {
				msg = "attributes must precede 'field' or 'fn' inside extern blocks"
			}
			p.emitDiagnostic(
				diag.SynIllegalItemInExtern,
				diag.SevError,
				tok.Span,
				msg,
				nil,
			)
			hasFatalError = true
			if !p.at(token.EOF) {
				p.advance()
			}
			p.resyncExternMember()
		}
	}

	return members, !hasFatalError
}

func (p *Parser) resyncExternMember() {
	p.resyncUntil(token.RBrace, token.KwFn, token.KwField, token.KwPub, token.KwAsync, token.At)
}

func (p *Parser) hasOverrideWithoutPub(attrs []ast.Attr, flags ast.FnModifier) bool {
	if flags&ast.FnModifierPublic != 0 {
		return false
	}

	for _, attr := range attrs {
		name := p.arenas.StringsInterner.MustLookup(attr.Name)
		if name == "override" {
			return true
		}
	}
	return false
}

func (p *Parser) parseExternField(attrs []ast.Attr, attrSpan source.Span) (ast.ExternMemberSpec, bool) {
	fieldTok := p.advance()
	startSpan := fieldTok.Span
	if attrSpan.End > attrSpan.Start {
		startSpan = attrSpan.Cover(startSpan)
	}

	nameID, ok := p.parseIdent()
	if !ok {
		p.resyncUntil(token.Semicolon, token.RBrace, token.KwFn, token.KwField)
		return ast.ExternMemberSpec{}, false
	}
	nameSpan := p.lastSpan

	colonTok, ok := p.expect(token.Colon, diag.SynExpectColon, "expected ':' after extern field name")
	if !ok {
		p.resyncUntil(token.Semicolon, token.RBrace, token.KwFn, token.KwField)
		return ast.ExternMemberSpec{}, false
	}

	fieldType, ok := p.parseTypePrefix()
	if !ok {
		p.resyncUntil(token.Semicolon, token.RBrace, token.KwFn, token.KwField)
		return ast.ExternMemberSpec{}, false
	}

	semiTok, ok := p.expect(token.Semicolon, diag.SynExpectSemicolon, "expected ';' after extern field declaration", func(b *diag.ReportBuilder) {
		if b == nil {
			return
		}
		insertSpan := p.lastSpan.ZeroideToEnd()
		fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertSpan)
		suggestion := fix.InsertText(
			"insert ';' after extern field declaration",
			insertSpan,
			";",
			"",
			fix.WithID(fixID),
			fix.WithKind(diag.FixKindRefactor),
			fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
		)
		b.WithFixSuggestion(suggestion)
		b.WithNote(insertSpan, "extern fields must end with ';'")
	})
	if !ok {
		p.resyncUntil(token.Semicolon, token.RBrace, token.KwFn, token.KwField)
		return ast.ExternMemberSpec{}, false
	}

	fieldSpan := startSpan.Cover(semiTok.Span)
	if typeExpr := p.arenas.Types.Get(fieldType); typeExpr != nil {
		fieldSpan = fieldSpan.Cover(typeExpr.Span)
	}
	if attrSpan.End > attrSpan.Start {
		fieldSpan = attrSpan.Cover(fieldSpan)
	}

	fieldID := p.arenas.NewExternField(
		nameID,
		nameSpan,
		fieldType,
		fieldTok.Span,
		colonTok.Span,
		semiTok.Span,
		attrs,
		fieldSpan,
	)

	return ast.ExternMemberSpec{
		Kind:  ast.ExternMemberField,
		Field: fieldID,
		Span:  fieldSpan,
	}, true
}
