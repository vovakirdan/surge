package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/token"
)

func (p *Parser) parseStructLiteral(typeID ast.TypeID, typeSpan source.Span) (ast.ExprID, bool) {
	openTok := p.advance()
	return p.parseStructLiteralBody(typeID, typeSpan, openTok)
}

// parseStructLiteralBody parses struct literal content after '{' has been consumed.
// This variant is used when the caller has already consumed the opening brace
// (e.g., when distinguishing between block expressions and struct literals).
func (p *Parser) parseStructLiteralBody(typeID ast.TypeID, typeSpan source.Span, openTok token.Token) (ast.ExprID, bool) {
	fields := make([]ast.ExprStructField, 0)
	var commas []source.Span
	trailing := false
	positional := false

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		p.suspendColonCast++
		// Parse a potential field key without consuming assignment operators, so we can
		// recognize `field = value` (and legacy `field: value`) in struct literals.
		fieldExpr, ok := p.parseBinaryExpr(precNullCoalescing)
		p.suspendColonCast--
		if !ok {
			p.resyncStructLiteralField()
			continue
		}

		if !positional && (p.at(token.Assign) || p.at(token.Colon)) {
			if ident, ok := p.arenas.Exprs.Ident(fieldExpr); ok && ident != nil {
				p.advance()
				valueExpr, valueOK := p.parseExpr()
				if !valueOK {
					p.resyncStructLiteralField()
					continue
				}
				fields = append(fields, ast.ExprStructField{
					Name:  ident.Name,
					Value: valueExpr,
				})
				goto handleComma
			}
			p.err(diag.SynExpectIdentifier, "expected identifier before ':' in struct literal")
			p.resyncStructLiteralField()
			continue
		}

		positional = true
		fields = append(fields, ast.ExprStructField{
			Name:  source.NoStringID,
			Value: fieldExpr,
		})

	handleComma:
		if p.at(token.Comma) {
			commaTok := p.advance()
			commas = append(commas, commaTok.Span)
			if p.at(token.RBrace) {
				trailing = true
				break
			}
			continue
		}

		break
	}

	closeTok, ok := p.expect(token.RBrace, diag.SynUnclosedBrace, "expected '}' to close struct literal", nil)
	if !ok {
		return ast.NoExprID, false
	}

	span := openTok.Span.Cover(closeTok.Span)
	if typeSpan != (source.Span{}) {
		span = typeSpan.Cover(span)
	}
	exprID := p.arenas.Exprs.NewStruct(span, typeID, fields, commas, trailing, positional)
	return exprID, true
}

// parseStructLiteralBodyWithFirst parses a struct literal body when the first field
// expression has already been parsed.
func (p *Parser) parseStructLiteralBodyWithFirst(typeID ast.TypeID, typeSpan source.Span, openTok token.Token, firstExpr ast.ExprID) (ast.ExprID, bool) {
	fields := make([]ast.ExprStructField, 0)
	var commas []source.Span
	trailing := false
	positional := false

	first := true
	for !p.at(token.RBrace) && !p.at(token.EOF) {
		var fieldExpr ast.ExprID
		if first {
			fieldExpr = firstExpr
			first = false
		} else {
			p.suspendColonCast++
			var ok bool
			fieldExpr, ok = p.parseBinaryExpr(precNullCoalescing)
			p.suspendColonCast--
			if !ok {
				p.resyncStructLiteralField()
				continue
			}
		}

		if !positional && (p.at(token.Assign) || p.at(token.Colon)) {
			if ident, ok := p.arenas.Exprs.Ident(fieldExpr); ok && ident != nil {
				p.advance()
				valueExpr, valueOK := p.parseExpr()
				if !valueOK {
					p.resyncStructLiteralField()
					continue
				}
				fields = append(fields, ast.ExprStructField{
					Name:  ident.Name,
					Value: valueExpr,
				})
				goto handleComma
			}
			p.err(diag.SynExpectIdentifier, "expected identifier before ':' in struct literal")
			p.resyncStructLiteralField()
			continue
		}

		positional = true
		fields = append(fields, ast.ExprStructField{
			Name:  source.NoStringID,
			Value: fieldExpr,
		})

	handleComma:
		if p.at(token.Comma) {
			commaTok := p.advance()
			commas = append(commas, commaTok.Span)
			if p.at(token.RBrace) {
				trailing = true
				break
			}
			continue
		}

		break
	}

	closeTok, ok := p.expect(token.RBrace, diag.SynUnclosedBrace, "expected '}' to close struct literal", nil)
	if !ok {
		return ast.NoExprID, false
	}

	span := openTok.Span.Cover(closeTok.Span)
	if typeSpan != (source.Span{}) {
		span = typeSpan.Cover(span)
	}
	exprID := p.arenas.Exprs.NewStruct(span, typeID, fields, commas, trailing, positional)
	return exprID, true
}

func (p *Parser) resyncStructLiteralField() {
	p.resyncUntil(token.Comma, token.RBrace, token.Semicolon, token.EOF)
	if p.at(token.Comma) || p.at(token.Semicolon) {
		p.advance()
	}
}
