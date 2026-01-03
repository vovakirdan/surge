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

	if p.at(token.RBrace) {
		// Empty braces default to struct literal.
		return p.parseStructLiteralBody(ast.NoTypeID, source.Span{}, lbraceTok)
	}

	p.suspendColonCast++
	p.allowFatArrow++
	firstExpr, ok := p.parseBinaryExpr(precNullCoalescing)
	p.allowFatArrow--
	p.suspendColonCast--
	if !ok {
		p.resyncStructLiteralField()
		return ast.NoExprID, false
	}

	if p.at(token.FatArrow) {
		return p.parseMapLiteralBody(lbraceTok, firstExpr)
	}
	return p.parseStructLiteralBodyWithFirst(ast.NoTypeID, source.Span{}, lbraceTok, firstExpr)
}

// isStatementKeyword checks if a token kind is a statement keyword
// that would indicate the start of a block expression.
func isStatementKeyword(kind token.Kind) bool {
	switch kind {
	case token.KwLet, token.KwConst, token.KwIf, token.KwWhile, token.KwFor,
		token.KwReturn, token.KwBreak, token.KwContinue, token.KwCompare, token.KwSelect, token.KwRace:
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

// parseExprOrBlockAsValue parses either an expression or a block expression.
// If a block expression is used, it is normalized to return the last expression
// (or nothing if the block has no trailing expression).
func (p *Parser) parseExprOrBlockAsValue() (ast.ExprID, bool) {
	if !p.at(token.LBrace) {
		return p.parseExpr()
	}
	openTok := p.advance()
	exprID, ok := p.parseBlockExprBody(openTok)
	if !ok {
		return ast.NoExprID, false
	}
	p.normalizeBlockExprValue(exprID)
	return exprID, true
}

// normalizeBlockExprValue ensures a block expression yields a value by
// rewriting the last statement into a return or appending a return nothing.
func (p *Parser) normalizeBlockExprValue(exprID ast.ExprID) {
	block, ok := p.arenas.Exprs.Block(exprID)
	if !ok || block == nil {
		return
	}
	if len(block.Stmts) == 0 {
		blockSpan := p.arenas.Exprs.Get(exprID).Span
		retID := p.arenas.Stmts.NewReturn(blockSpan.ZeroideToEnd(), ast.NoExprID)
		block.Stmts = append(block.Stmts, retID)
		return
	}

	lastIdx := len(block.Stmts) - 1
	lastID := block.Stmts[lastIdx]
	lastStmt := p.arenas.Stmts.Get(lastID)
	if lastStmt == nil {
		return
	}
	switch lastStmt.Kind {
	case ast.StmtReturn:
		return
	case ast.StmtExpr:
		exprStmt := p.arenas.Stmts.Expr(lastID)
		if exprStmt == nil {
			return
		}
		retID := p.arenas.Stmts.NewReturn(lastStmt.Span, exprStmt.Expr)
		block.Stmts[lastIdx] = retID
	default:
		blockSpan := p.arenas.Exprs.Get(exprID).Span
		retID := p.arenas.Stmts.NewReturn(blockSpan.ZeroideToEnd(), ast.NoExprID)
		block.Stmts = append(block.Stmts, retID)
	}
}
