package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

func (p *Parser) parseContractItem(attrs []ast.Attr, attrSpan source.Span, visibility ast.Visibility, prefixSpan source.Span, hasPrefix bool) (ast.ItemID, bool) {
	contractTok := p.advance()
	startSpan := contractTok.Span
	if attrSpan.End > attrSpan.Start {
		startSpan = attrSpan.Cover(startSpan)
	}
	if hasPrefix {
		startSpan = prefixSpan.Cover(startSpan)
	}

	nameID, ok := p.parseIdent()
	if !ok {
		p.resyncUntil(token.LParen, token.KwContract, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwType, token.KwTag)
		return ast.NoItemID, false
	}
	nameSpan := p.lastSpan

	generics, genericCommas, genericsTrailing, genericsSpan, ok := p.parseFnGenerics()
	if !ok {
		p.resyncUntil(token.LParen, token.KwContract, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwType, token.KwTag)
		return ast.NoItemID, false
	}

	openTok, ok := p.expect(token.LParen, diag.SynUnexpectedToken, "expected '(' to start contract body")
	if !ok {
		p.resyncUntil(token.RParen, token.KwContract, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwType, token.KwTag)
		return ast.NoItemID, false
	}

	members, okMembers := p.parseContractMembers()

	closeTok, ok := p.expect(
		token.RParen,
		diag.SynUnclosedParen,
		"expected ')' to close contract body",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			insertSpan := p.lastSpan.ZeroideToEnd()
			fixID := fix.MakeFixID(diag.SynUnclosedParen, insertSpan)
			suggestion := fix.InsertText(
				"insert ')' to close contract body",
				insertSpan,
				")",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "missing ')' after contract items")
		},
	)
	if !ok {
		return ast.NoItemID, false
	}

	if !okMembers {
		return ast.NoItemID, false
	}

	bodySpan := openTok.Span.Cover(closeTok.Span)
	itemSpan := startSpan.Cover(closeTok.Span)
	if p.at(token.Semicolon) {
		semiTok := p.advance()
		itemSpan = itemSpan.Cover(semiTok.Span)
	}

	itemID := p.arenas.NewContract(
		nameID,
		nameSpan,
		generics,
		genericCommas,
		genericsTrailing,
		genericsSpan,
		contractTok.Span,
		bodySpan,
		attrs,
		members,
		visibility,
		itemSpan,
	)
	return itemID, true
}

func (p *Parser) parseContractMembers() ([]ast.ContractItemSpec, bool) {
	items := make([]ast.ContractItemSpec, 0)
	hasFatalError := false

	for !p.at(token.RParen) && !p.at(token.EOF) {
		attrs, attrSpan, ok := p.parseAttributes()
		if !ok {
			hasFatalError = true
			p.resyncContractMember()
			continue
		}

		mods := p.parseFnModifiers()
		tok := p.lx.Peek()

		switch tok.Kind {
		case token.KwField:
			if mods.hasSpan {
				span := mods.span
				p.emitDiagnostic(
					diag.SynUnexpectedModifier,
					diag.SevError,
					span,
					"modifiers are not allowed before 'field' in a contract",
					nil,
				)
			}
			spec, parsed := p.parseContractField(attrs, attrSpan)
			if !parsed {
				hasFatalError = true
				p.resyncContractMember()
				continue
			}
			items = append(items, spec)
		case token.KwFn:
			spec, parsed := p.parseContractFn(attrs, attrSpan, mods)
			if !parsed {
				hasFatalError = true
				p.resyncContractMember()
				continue
			}
			items = append(items, spec)
		default:
			switch {
			case mods.flags != 0:
				span := mods.span
				if !mods.hasSpan {
					span = tok.Span
				}
				p.emitDiagnostic(
					diag.SynUnexpectedToken,
					diag.SevError,
					span,
					"expected 'fn' after function modifiers",
					nil,
				)
			case len(attrs) > 0 && attrSpan.End > attrSpan.Start:
				p.emitDiagnostic(
					diag.SynUnexpectedToken,
					diag.SevError,
					attrSpan,
					"attributes must precede 'field' or 'fn' inside contracts",
					nil,
				)
			default:
				p.emitDiagnostic(
					diag.SynUnexpectedToken,
					diag.SevError,
					tok.Span,
					"expected 'field' or 'fn' inside contract body",
					nil,
				)
			}
			hasFatalError = true
			if !p.at(token.EOF) {
				p.advance()
			}
			p.resyncContractMember()
		}
	}

	return items, !hasFatalError
}

func (p *Parser) parseContractField(attrs []ast.Attr, attrSpan source.Span) (ast.ContractItemSpec, bool) {
	fieldTok := p.advance()
	startSpan := fieldTok.Span
	if attrSpan.End > attrSpan.Start {
		startSpan = attrSpan.Cover(startSpan)
	}

	nameID, ok := p.parseIdent()
	if !ok {
		p.resyncUntil(token.Semicolon, token.RParen, token.KwFn, token.KwField)
		return ast.ContractItemSpec{}, false
	}
	nameSpan := p.lastSpan

	colonTok, ok := p.expect(token.Colon, diag.SynExpectColon, "expected ':' after contract field name")
	if !ok {
		p.resyncUntil(token.Semicolon, token.RParen, token.KwFn, token.KwField)
		return ast.ContractItemSpec{}, false
	}

	fieldType, ok := p.parseTypePrefix()
	if !ok {
		p.resyncUntil(token.Semicolon, token.RParen, token.KwFn, token.KwField)
		return ast.ContractItemSpec{}, false
	}

	semiTok, ok := p.expect(token.Semicolon, diag.SynExpectSemicolon, "expected ';' after contract field requirement", func(b *diag.ReportBuilder) {
		if b == nil {
			return
		}
		insertSpan := p.lastSpan.ZeroideToEnd()
		fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertSpan)
		suggestion := fix.InsertText(
			"insert ';' after contract field requirement",
			insertSpan,
			";",
			"",
			fix.WithID(fixID),
			fix.WithKind(diag.FixKindRefactor),
			fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
		)
		b.WithFixSuggestion(suggestion)
		b.WithNote(insertSpan, "contract field requirements must end with ';'")
	})
	if !ok {
		p.resyncUntil(token.Semicolon, token.RParen, token.KwFn, token.KwField)
		return ast.ContractItemSpec{}, false
	}

	fieldSpan := startSpan.Cover(semiTok.Span)
	payload := p.arenas.NewContractField(
		nameID,
		nameSpan,
		fieldType,
		fieldTok.Span,
		colonTok.Span,
		semiTok.Span,
		attrs,
		fieldSpan,
	)

	return ast.ContractItemSpec{
		Kind:    ast.ContractItemField,
		Payload: payload,
		Span:    fieldSpan,
	}, true
}

func (p *Parser) parseContractFn(attrs []ast.Attr, attrSpan source.Span, mods fnModifiers) (ast.ContractItemSpec, bool) {
	fnData, ok := p.parseFnDefinition(attrSpan, mods)
	if !ok {
		return ast.ContractItemSpec{}, false
	}

	if fnData.body.IsValid() {
		bodySpan := fnData.span
		if stmt := p.arenas.Stmts.Get(fnData.body); stmt != nil {
			bodySpan = stmt.Span
		}
		p.emitDiagnostic(
			diag.SynUnexpectedToken,
			diag.SevError,
			bodySpan,
			"functions inside contracts cannot have a body; use ';' to terminate the signature",
			nil,
		)
		return ast.ContractItemSpec{}, false
	}

	payload := p.arenas.NewContractFn(
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
		fnData.flags,
		attrs,
		fnData.span,
	)

	return ast.ContractItemSpec{
		Kind:    ast.ContractItemFn,
		Payload: payload,
		Span:    fnData.span,
	}, true
}

func (p *Parser) resyncContractMember() {
	p.resyncUntil(token.Semicolon, token.RParen, token.KwFn, token.KwField, token.KwPub, token.KwAsync, token.At)
	if p.at(token.Semicolon) {
		p.advance()
	}
}
