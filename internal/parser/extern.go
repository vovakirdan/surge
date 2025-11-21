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
		p.resyncUntil(token.RBrace, token.KwExtern, token.KwFn)
		return ast.NoItemID, false
	}

	targetType, ok := p.parseTypePrefix()
	if !ok {
		p.resyncUntil(token.Gt, token.RBrace, token.KwFn)
		if p.at(token.Gt) {
			p.advance()
		}
		if !p.at(token.LBrace) {
			return ast.NoItemID, false
		}
	}

	if _, ok = p.expect(token.Gt, diag.SynUnexpectedToken, "expected '>' after extern target type"); !ok {
		p.resyncUntil(token.LBrace, token.RBrace, token.KwFn)
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
		if !p.at(token.KwFn) {
			tok := p.lx.Peek()
			p.emitDiagnostic(
				diag.SynIllegalItemInExtern,
				diag.SevError,
				tok.Span,
				"only function declarations are allowed inside extern blocks",
				nil,
			)
			hasFatalError = true
			if !p.at(token.EOF) {
				p.advance()
			}
			p.resyncExternMember()
			continue
		}

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
	}

	return members, !hasFatalError
}

func (p *Parser) resyncExternMember() {
	p.resyncUntil(token.RBrace, token.KwFn, token.KwPub, token.KwAsync, token.At)
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
