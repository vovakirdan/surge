package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/token"
)

// parseEnumItem parses enum declarations:
//
//	enum Color { Red, Green, Blue }
//	enum Status: uint8 { Unknown = 0, Started, Done }
func (p *Parser) parseEnumItem(attrs []ast.Attr, attrSpan source.Span, visibility ast.Visibility, prefixSpan source.Span, hasPrefix bool) (ast.ItemID, bool) {
	enumTok := p.advance()
	enumKwSpan := enumTok.Span

	startSpan := enumKwSpan
	if attrSpan.End > attrSpan.Start {
		startSpan = attrSpan.Cover(startSpan)
	}
	if hasPrefix {
		startSpan = prefixSpan.Cover(startSpan)
	}

	nameID, ok := p.parseIdent()
	if !ok {
		p.resyncUntil(token.Semicolon, token.KwType, token.KwEnum, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract, token.EOF)
		return ast.NoItemID, false
	}

	typeParams, generics, genericCommas, genericsTrailing, genericsSpan, ok := p.parseFnGenerics()
	if !ok {
		p.resyncUntil(token.Semicolon, token.KwType, token.KwEnum, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract, token.EOF)
		return ast.NoItemID, false
	}

	// Parse optional base type after ':'
	var baseType ast.TypeID
	var baseTypeSpan source.Span
	var colonSpan source.Span
	if p.at(token.Colon) {
		colonTok := p.advance()
		colonSpan = colonTok.Span

		baseType, ok = p.parseTypePrefix()
		if !ok {
			p.resyncUntil(token.Semicolon, token.KwType, token.KwEnum, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract, token.EOF)
			return ast.NoItemID, false
		}
		baseTypeSpan = p.lastSpan
	}

	// Expect '=' after enum name (and optional base type)
	assignTok, ok := p.expect(token.Assign, diag.SynTypeExpectEquals, "expected '=' after enum name", nil)
	if !ok {
		p.resyncUntil(token.Semicolon, token.KwType, token.KwEnum, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract, token.EOF)
		return ast.NoItemID, false
	}
	assignSpan := assignTok.Span

	// Expect '{' for enum body
	lbraceTok, ok := p.expect(token.LBrace, diag.SynEnumExpectBody, "expected '{' for enum body", nil)
	if !ok {
		p.resyncUntil(token.Semicolon, token.KwType, token.KwEnum, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract, token.EOF)
		return ast.NoItemID, false
	}
	bodyStart := lbraceTok.Span

	// Parse enum variants
	variants, variantCommas, hasTrailing, ok := p.parseEnumVariants()
	if !ok {
		p.resyncUntil(token.Semicolon, token.KwType, token.KwEnum, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract, token.EOF)
		return ast.NoItemID, false
	}

	// Expect '}'
	rbraceTok, ok := p.expect(token.RBrace, diag.SynEnumExpectRBrace, "expected '}' after enum body", nil)
	if !ok {
		p.resyncUntil(token.Semicolon, token.KwType, token.KwEnum, token.KwFn, token.KwImport, token.KwLet, token.KwConst, token.KwContract, token.EOF)
		return ast.NoItemID, false
	}
	bodySpan := bodyStart.Cover(rbraceTok.Span)

	itemSpan := startSpan.Cover(rbraceTok.Span)
	semiSpan := source.Span{}
	if p.at(token.Semicolon) {
		semiTok := p.advance()
		itemSpan = itemSpan.Cover(semiTok.Span)
		semiSpan = semiTok.Span
	}

	itemID := p.arenas.NewTypeEnum(
		nameID,
		generics,
		genericCommas,
		genericsTrailing,
		genericsSpan,
		typeParams,
		enumKwSpan,
		assignSpan,
		semiSpan,
		attrs,
		visibility,
		baseType,
		baseTypeSpan,
		colonSpan,
		variants,
		variantCommas,
		hasTrailing,
		bodySpan,
		itemSpan,
	)
	return itemID, true
}

// parseEnumVariants parses comma-separated list of enum variants:
//
//	Red, Green = 1, Blue
func (p *Parser) parseEnumVariants() ([]ast.EnumVariantSpec, []source.Span, bool, bool) {
	var variants []ast.EnumVariantSpec
	var commas []source.Span
	hasTrailing := false

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		variantStart := p.lx.Peek().Span

		// Parse variant name
		nameID, ok := p.parseIdent()
		if !ok {
			// Try to recover by finding next comma or }
			if p.at(token.Comma) {
				p.advance()
				continue
			}
			return nil, nil, false, false
		}
		nameSpan := p.lastSpan

		// Parse optional = value
		var value ast.ExprID
		var assignSpan source.Span
		if p.at(token.Assign) {
			assignTok := p.advance()
			assignSpan = assignTok.Span

			value, ok = p.parseExpr()
			if !ok {
				return nil, nil, false, false
			}
		}

		variantSpan := variantStart.Cover(p.lastSpan)
		variants = append(variants, ast.EnumVariantSpec{
			Name:       nameID,
			NameSpan:   nameSpan,
			Value:      value,
			AssignSpan: assignSpan,
			Span:       variantSpan,
		})

		// Parse comma separator
		if p.at(token.Comma) {
			commaTok := p.advance()
			commas = append(commas, commaTok.Span)

			// Check for trailing comma
			if p.at(token.RBrace) {
				hasTrailing = true
				break
			}
		} else {
			// No comma, expect }
			break
		}
	}

	return variants, commas, hasTrailing, true
}
