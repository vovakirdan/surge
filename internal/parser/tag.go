package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

func (p *Parser) parseTagItem(
	attrs []ast.Attr,
	attrSpan source.Span,
	visibility ast.Visibility,
	prefixSpan source.Span,
	hasPrefix bool,
) (ast.ItemID, bool) {
	tagTok := p.advance()

	startSpan := tagTok.Span
	if attrSpan.End > attrSpan.Start {
		startSpan = attrSpan.Cover(startSpan)
	}
	if hasPrefix {
		startSpan = prefixSpan.Cover(startSpan)
	}

	nameID, ok := p.parseIdent()
	if !ok {
		return ast.NoItemID, false
	}

	generics, ok := p.parseFnGenerics()
	if !ok {
		p.resyncUntil(token.LParen, token.Semicolon, token.KwFn, token.KwLet, token.KwType, token.KwTag, token.KwImport)
		return ast.NoItemID, false
	}

	if _, ok := p.expect(token.LParen, diag.SynUnexpectedToken, "expected '(' after tag name"); !ok {
		p.resyncUntil(token.Semicolon, token.KwFn, token.KwLet, token.KwType, token.KwTag, token.KwImport)
		return ast.NoItemID, false
	}

	payload := make([]ast.TypeID, 0, 2)

	if !p.at(token.RParen) {
		for {
			argType, ok := p.parseTypePrefix()
			if !ok {
				p.resyncUntil(token.Comma, token.RParen, token.Semicolon, token.KwFn, token.KwLet, token.KwType, token.KwTag, token.KwImport)
				if p.at(token.RParen) {
					p.advance()
				}
				return ast.NoItemID, false
			}
			payload = append(payload, argType)

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

	if _, ok := p.expect(token.RParen, diag.SynUnclosedParen, "expected ')' to close tag payload list", nil); !ok {
		p.resyncUntil(token.Semicolon, token.KwFn, token.KwLet, token.KwType, token.KwTag, token.KwImport)
		return ast.NoItemID, false
	}

	insertSpan := p.lastSpan.ZeroideToEnd()
	if _, ok := p.expect(token.Semicolon, diag.SynExpectSemicolon, "expected ';' after tag declaration", func(b *diag.ReportBuilder) {
		if b == nil {
			return
		}
		fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertSpan)
		suggestion := fix.InsertText(
			"insert ';' after the tag declaration",
			insertSpan,
			";",
			"",
			fix.WithID(fixID),
			fix.WithKind(diag.FixKindRefactor),
			fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
		)
		b.WithFixSuggestion(suggestion)
		b.WithNote(insertSpan, "insert ';' to terminate the tag declaration")
	}); !ok {
		return ast.NoItemID, false
	}

	itemSpan := startSpan.Cover(p.lastSpan)
	tagID := p.arenas.NewTag(nameID, generics, payload, attrs, visibility, itemSpan)
	return tagID, true
}
