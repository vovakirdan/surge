package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/token"
)

func (p *Parser) parseAsyncExpr() (ast.ExprID, bool) {
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
	if stmt := p.arenas.Stmts.Get(bodyID); stmt != nil {
		span = span.Cover(stmt.Span)
	}
	return p.arenas.Exprs.NewAsync(span, bodyID), true
}
