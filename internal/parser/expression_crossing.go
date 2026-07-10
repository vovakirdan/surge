package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
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

	// `on blocking { ... }` is a postponed destination. Reject it with
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

// parseSpawnOnRemote parses a `spawn on <dst> { ... }` remote-spawn expression
// This parses the remote-spawn form. The caller has consumed the `spawn` keyword (passing its
// span as spawnSpan) and verified via atSpawnOnRemoteHead that the current token
// is the contextual `on` keyword followed by a destination head or `{`. The
// result reuses the ExprOn node with the Spawn flag set; it evaluates to
// `far Task<T>` (enforced by sema). Parse mirrors parseOnCrossing: struct-literal
// recognition is suppressed on the destination and the body is a `ret`-producing
// value block. Missing destination and missing block are reported as SYN2033 and
// SYN2032 respectively, with recovery kept deterministic. attrStart/attrCount
// carry any leading attributes (e.g. `@local`) onto the node for sema.
func (p *Parser) parseSpawnOnRemote(spawnSpan source.Span, attrStart ast.AttrID, attrCount uint32) (ast.ExprID, bool) {
	onTok := p.advance() // consume contextual `on`

	// `spawn on { ... }`: the destination was omitted. Report SYN2033 and still
	// consume the trailing block so parsing recovers cleanly.
	if p.at(token.LBrace) {
		p.emitDiagnostic(
			diag.SynSpawnOnMissingDestination,
			diag.SevError,
			spawnSpan.Cover(onTok.Span),
			"`spawn on` requires a `Placement` destination",
			nil,
		)
		bodyID, _ := p.parseBlock()
		return p.arenas.Exprs.NewSpawnOn(p.spawnOnSpan(spawnSpan, onTok.Span, bodyID), ast.NoExprID, bodyID, attrStart, attrCount), true
	}

	// `spawn on blocking { ... }` is a postponed destination. Reject it
	// with a deterministic diagnostic (mirroring Block 2's `on blocking`) and still
	// consume the trailing block for recovery.
	if p.at(token.KwBlocking) {
		blkTok := p.advance()
		p.emitDiagnostic(
			diag.FutSpawnOnDestBlocking,
			diag.SevError,
			spawnSpan.Cover(blkTok.Span),
			"`spawn on blocking` is not a valid placement destination",
			nil,
		)
		var bodyID ast.StmtID
		if p.at(token.LBrace) {
			bodyID, _ = p.parseBlock()
		}
		return p.arenas.Exprs.NewSpawnOn(p.spawnOnSpan(spawnSpan, blkTok.Span, bodyID), ast.NoExprID, bodyID, attrStart, attrCount), true
	}

	// Suppress `TypeName {` struct-literal recognition so the destination's
	// trailing '{' opens the spawn body (e.g. `spawn on Job { ... }` = dst `Job` +
	// body, which sema then rejects as a type-name destination).
	p.noStructLiteral++
	destExpr, ok := p.parseExpr()
	p.noStructLiteral--
	if !ok {
		return ast.NoExprID, false
	}

	// `spawn on <dst>` without a block (e.g. `spawn on distributed;`): report
	// SYN2032 and return the node so the enclosing statement recovers on its
	// terminator.
	if !p.at(token.LBrace) {
		destSpan := spawnSpan.Cover(onTok.Span)
		if node := p.arenas.Exprs.Get(destExpr); node != nil {
			destSpan = destSpan.Cover(node.Span)
		}
		p.emitDiagnostic(
			diag.SynSpawnOnMissingBlock,
			diag.SevError,
			destSpan,
			"`spawn on` requires a `{ ret expr; }` block",
			nil,
		)
		return p.arenas.Exprs.NewSpawnOn(destSpan, destExpr, ast.NoStmtID, attrStart, attrCount), true
	}

	bodyID, ok := p.parseBlock()
	if !ok {
		return ast.NoExprID, false
	}
	return p.arenas.Exprs.NewSpawnOn(p.spawnOnSpan(spawnSpan, onTok.Span, bodyID), destExpr, bodyID, attrStart, attrCount), true
}

// spawnOnSpan builds the span for a `spawn on ...` node, covering the `spawn`
// keyword through the parsed body (or the last consumed head token when the body
// is absent).
func (p *Parser) spawnOnSpan(spawnSpan, headSpan source.Span, body ast.StmtID) source.Span {
	span := spawnSpan.Cover(headSpan)
	if stmt := p.arenas.Stmts.Get(body); stmt != nil {
		span = span.Cover(stmt.Span)
	}
	return span
}
