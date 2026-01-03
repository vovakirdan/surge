package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/token"
)

func (p *Parser) parseMapLiteralBody(openTok token.Token, firstKey ast.ExprID) (ast.ExprID, bool) {
	entries := make([]ast.ExprMapEntry, 0)
	var commas []source.Span
	trailing := false

	keyExpr := firstKey
	for !p.at(token.RBrace) && !p.at(token.EOF) {
		if _, ok := p.expect(token.FatArrow, diag.SynExpectExpression, "expected '=>' after map key", nil); !ok {
			p.resyncMapLiteralEntry()
			return ast.NoExprID, false
		}
		valueExpr, valueOK := p.parseExpr()
		if !valueOK {
			p.resyncMapLiteralEntry()
			return ast.NoExprID, false
		}
		entries = append(entries, ast.ExprMapEntry{Key: keyExpr, Value: valueExpr})

		if p.at(token.Comma) {
			commaTok := p.advance()
			commas = append(commas, commaTok.Span)
			if p.at(token.RBrace) {
				trailing = true
				break
			}
			p.suspendColonCast++
			var ok bool
			keyExpr, ok = p.parseBinaryExpr(precNullCoalescing)
			p.suspendColonCast--
			if !ok {
				p.resyncMapLiteralEntry()
				continue
			}
			continue
		}
		break
	}

	closeTok, ok := p.expect(token.RBrace, diag.SynUnclosedBrace, "expected '}' to close map literal", nil)
	if !ok {
		return ast.NoExprID, false
	}

	span := openTok.Span.Cover(closeTok.Span)
	exprID := p.arenas.Exprs.NewMap(span, entries, commas, trailing)
	return exprID, true
}

func (p *Parser) resyncMapLiteralEntry() {
	p.resyncUntil(token.Comma, token.RBrace, token.Semicolon, token.EOF)
	if p.at(token.Comma) || p.at(token.Semicolon) {
		p.advance()
	}
}
