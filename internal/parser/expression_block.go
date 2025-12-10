package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/token"
)

// parseBraceExpr decides whether a brace starts a block expression or struct literal.
// If the token after '{' is a statement keyword, it's a block expression.
// Otherwise, it's a struct literal.
func (p *Parser) parseBraceExpr() (ast.ExprID, bool) {
	// Peek at the token AFTER '{' without consuming '{'
	// We need to lookahead 2 tokens: '{' then the next one
	lbraceTok := p.advance() // consume '{'
	nextKind := p.lx.Peek().Kind

	if isStatementKeyword(nextKind) {
		// Parse as block expression - '{' already consumed
		return p.parseBlockExprBody(lbraceTok)
	}

	// Not a block expression - need to parse as struct literal
	// parseStructLiteral expects '{' to NOT be consumed yet,
	// so we call a variant that takes the already-consumed open token
	return p.parseStructLiteralBody(ast.NoTypeID, source.Span{}, lbraceTok)
}

// isStatementKeyword checks if a token kind is a statement keyword
// that would indicate the start of a block expression.
func isStatementKeyword(kind token.Kind) bool {
	switch kind {
	case token.KwLet, token.KwConst, token.KwIf, token.KwWhile, token.KwFor,
		token.KwReturn, token.KwBreak, token.KwContinue, token.KwCompare:
		return true
	}
	return false
}

// parseBlockExprBody parses the body of a block expression after '{' has been consumed.
// Block expressions contain statements and must end with a return statement
// (unless the expected type is 'nothing').
func (p *Parser) parseBlockExprBody(openTok token.Token) (ast.ExprID, bool) {
	var stmts []ast.StmtID

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		// Защита от бесконечного цикла: запоминаем позицию до парсинга
		before := p.lx.Peek()

		stmtID, ok := p.parseStmt()
		if !ok {
			p.resyncStatement()

			// Гарантируем прогресс: если токен не сдвинулся, принудительно продвигаемся
			if !p.at(token.EOF) && !p.at(token.RBrace) {
				after := p.lx.Peek()
				if after.Kind == before.Kind && after.Span == before.Span {
					p.advance()
				}
			}
			continue
		}
		stmts = append(stmts, stmtID)
	}

	closeTok, ok := p.expect(token.RBrace, diag.SynUnclosedBrace, "expected '}' to close block expression", nil)
	if !ok {
		return ast.NoExprID, false
	}

	span := openTok.Span.Cover(closeTok.Span)
	return p.arenas.Exprs.NewBlock(span, stmts), true
}
