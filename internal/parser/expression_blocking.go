package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/token"
)

func (p *Parser) parseBlockingExpr() (ast.ExprID, bool) {
	blockingTok := p.advance()

	if !p.at(token.LBrace) {
		p.err(diag.SynUnexpectedToken, "expected '{' after 'blocking'")
		return ast.NoExprID, false
	}

	bodyID, ok := p.parseBlock()
	if !ok {
		return ast.NoExprID, false
	}

	span := blockingTok.Span
	if stmt := p.arenas.Stmts.Get(bodyID); stmt != nil {
		span = span.Cover(stmt.Span)
	}
	return p.arenas.Exprs.NewBlocking(span, bodyID), true
}
