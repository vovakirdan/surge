package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/token"
)

// parseOnCrossing parses an `on <dst> { ... }` placement-crossing expression.
// The caller has verified via atOnCrossingHead that the current token is the
// contextual `on` keyword followed by a destination head. The destination is an
// arbitrary expression (its kind is validated by sema); the body is a value
// block whose result is produced by `ret`. The crossing evaluates to
// `TaskResult<T>` (enforced by sema).
func (p *Parser) parseOnCrossing() (ast.ExprID, bool) {
	onTok := p.advance() // consume contextual `on`

	// `on blocking { ... }` is a postponed destination in Epic 11. Reject it with
	// a deterministic diagnostic instead of parsing `blocking` as a value, and
	// still consume the trailing block so parsing recovers cleanly.
	if p.at(token.KwBlocking) {
		blkTok := p.advance()
		p.emitDiagnostic(
			diag.FutOnDestBlocking,
			diag.SevError,
			onTok.Span.Cover(blkTok.Span),
			"`on blocking` is not a valid crossing destination",
			nil,
		)
		var bodyID ast.StmtID
		if p.at(token.LBrace) {
			bodyID, _ = p.parseBlock()
		}
		span := onTok.Span.Cover(blkTok.Span)
		if stmt := p.arenas.Stmts.Get(bodyID); stmt != nil {
			span = span.Cover(stmt.Span)
		}
		return p.arenas.Exprs.NewOn(span, ast.NoExprID, bodyID), true
	}

	// Suppress `TypeName {` struct-literal recognition so the destination's
	// trailing '{' opens the crossing body (e.g. `on Job { ... }` = dst `Job` +
	// body, which sema then rejects as a type-name destination). Struct literals
	// remain available inside parentheses within the destination.
	p.noStructLiteral++
	destExpr, ok := p.parseExpr()
	p.noStructLiteral--
	if !ok {
		return ast.NoExprID, false
	}

	if !p.at(token.LBrace) {
		p.err(diag.SynUnexpectedToken, "expected '{' to start the `on` crossing body")
		return ast.NoExprID, false
	}

	bodyID, ok := p.parseBlock()
	if !ok {
		return ast.NoExprID, false
	}

	span := onTok.Span
	if stmt := p.arenas.Stmts.Get(bodyID); stmt != nil {
		span = span.Cover(stmt.Span)
	}
	return p.arenas.Exprs.NewOn(span, destExpr, bodyID), true
}
