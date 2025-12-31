package parser

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/token"
)

func (p *Parser) parseAsyncExpr() (ast.ExprID, bool) {
	return p.parseAsyncExprWithAttrs(nil, source.Span{})
}

func (p *Parser) parseAsyncExprWithAttrs(attrs []ast.Attr, attrSpan source.Span) (ast.ExprID, bool) {
	p.validateAsyncAttrs(attrs, attrSpan)
	asyncTok := p.advance()

	if !p.at(token.LBrace) {
		p.err(diag.SynUnexpectedToken, "expected '{' after 'async'")
		return ast.NoExprID, false
	}

	bodyID, ok := p.parseBlock()
	if !ok {
		return ast.NoExprID, false
	}

	span := asyncTok.Span
	if attrSpan.End > attrSpan.Start {
		span = attrSpan.Cover(span)
	}
	if stmt := p.arenas.Stmts.Get(bodyID); stmt != nil {
		span = span.Cover(stmt.Span)
	}
	attrStart, attrCount := p.arenas.Items.AllocateAttrs(attrs)
	return p.arenas.Exprs.NewAsync(span, bodyID, attrStart, attrCount), true
}

func (p *Parser) validateAsyncAttrs(attrs []ast.Attr, attrSpan source.Span) bool {
	if len(attrs) == 0 {
		return true
	}
	if p.arenas == nil || p.arenas.StringsInterner == nil {
		return true
	}
	ok := true
	for _, attr := range attrs {
		name := p.arenas.StringsInterner.MustLookup(attr.Name)
		if !strings.EqualFold(name, "failfast") {
			p.emitDiagnostic(
				diag.SynAttributeNotAllowed,
				diag.SevError,
				attr.Span,
				"attribute '@"+name+"' is not allowed on async blocks",
				nil,
			)
			ok = false
			continue
		}
		if len(attr.Args) > 0 {
			p.emitDiagnostic(
				diag.SynUnexpectedToken,
				diag.SevError,
				attr.Span,
				"'@failfast' does not accept arguments",
				nil,
			)
			ok = false
		}
	}
	if !ok && attrSpan.End > attrSpan.Start {
		p.emitDiagnostic(
			diag.SynAttributeNotAllowed,
			diag.SevError,
			attrSpan,
			"only '@failfast' is supported on async blocks",
			nil,
		)
	}
	return ok
}
