package parser

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
)

func (p *Parser) parseSpawnExprWithAttrs(attrs []ast.Attr, attrSpan source.Span) (ast.ExprID, bool) {
	p.validateSpawnAttrs(attrs, attrSpan)
	spawnTok := p.advance()

	expr, ok := p.parsePostfixExpr()
	if !ok {
		return ast.NoExprID, false
	}

	span := spawnTok.Span
	if attrSpan.End > attrSpan.Start {
		span = attrSpan.Cover(span)
	}
	if node := p.arenas.Exprs.Get(expr); node != nil {
		span = span.Cover(node.Span)
	}
	attrStart, attrCount := p.arenas.Items.AllocateAttrs(attrs)
	return p.arenas.Exprs.NewSpawn(span, expr, attrStart, attrCount), true
}

func (p *Parser) validateSpawnAttrs(attrs []ast.Attr, attrSpan source.Span) bool {
	if len(attrs) == 0 {
		return true
	}
	if p.arenas == nil || p.arenas.StringsInterner == nil {
		return true
	}
	ok := true
	for _, attr := range attrs {
		name := p.arenas.StringsInterner.MustLookup(attr.Name)
		if !strings.EqualFold(name, "local") {
			p.emitDiagnostic(
				diag.SynAttributeNotAllowed,
				diag.SevError,
				attr.Span,
				"attribute '@"+name+"' is not allowed on spawn expressions",
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
				"'@local' does not accept arguments",
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
			"only '@local' is supported on spawn expressions",
			nil,
		)
	}
	return ok
}
