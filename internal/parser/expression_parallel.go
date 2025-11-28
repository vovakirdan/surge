package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/token"
)

func (p *Parser) parseParallelExpr() (ast.ExprID, bool) {
	p.allowFatArrow++
	defer func() { p.allowFatArrow-- }()
	parallelTok := p.advance()

	var modeTok token.Token
	switch p.lx.Peek().Kind {
	case token.KwMap:
		modeTok = p.advance()
	case token.KwReduce:
		modeTok = p.advance()
	default:
		p.emitDiagnostic(
			diag.SynUnexpectedToken,
			diag.SevError,
			p.lx.Peek().Span,
			"expected 'map' or 'reduce' after 'parallel'",
			nil,
		)
		return ast.NoExprID, false
	}

	iterableExpr, ok := p.parseExpr()
	if !ok {
		return ast.NoExprID, false
	}

	withTok, ok := p.expect(token.KwWith, diag.SynUnexpectedToken, "expected 'with' after parallel iterable")
	if !ok {
		return ast.NoExprID, false
	}

	var (
		initExpr ast.ExprID
		commaTok token.Token
	)

	if modeTok.Kind == token.KwReduce {
		initExpr, ok = p.parseExpr()
		if !ok {
			return ast.NoExprID, false
		}
		commaTok, ok = p.expect(token.Comma, diag.SynUnexpectedToken, "expected ',' between reduce initializer and argument list")
		if !ok {
			return ast.NoExprID, false
		}
	}

	args, argsSpan, ok := p.parseParallelArgList()
	if !ok {
		return ast.NoExprID, false
	}

	arrowTok, ok := p.expect(token.FatArrow, diag.SynUnexpectedToken, "expected '=>' after parallel argument list")
	if !ok {
		return ast.NoExprID, false
	}

	bodyExpr, ok := p.parseExpr()
	if !ok {
		return ast.NoExprID, false
	}

	span := parallelTok.Span.Cover(modeTok.Span)
	if node := p.arenas.Exprs.Get(iterableExpr); node != nil {
		span = span.Cover(node.Span)
	}
	span = span.Cover(withTok.Span)
	if initExpr.IsValid() {
		if node := p.arenas.Exprs.Get(initExpr); node != nil {
			span = span.Cover(node.Span)
		}
		if commaTok.Kind != token.Invalid {
			span = span.Cover(commaTok.Span)
		}
	}
	span = span.Cover(argsSpan)
	span = span.Cover(arrowTok.Span)
	if node := p.arenas.Exprs.Get(bodyExpr); node != nil {
		span = span.Cover(node.Span)
	}

	if modeTok.Kind == token.KwMap {
		exprID := p.arenas.Exprs.NewParallelMap(span, iterableExpr, args, bodyExpr)
		return exprID, true
	}

	exprID := p.arenas.Exprs.NewParallelReduce(span, iterableExpr, initExpr, args, bodyExpr)
	return exprID, true
}

func (p *Parser) parseParallelArgList() ([]ast.ExprID, source.Span, bool) {
	openTok, ok := p.expect(token.LParen, diag.SynUnexpectedToken, "expected '(' to start parallel argument list")
	if !ok {
		return nil, source.Span{}, false
	}

	args := make([]ast.ExprID, 0, 2)
	listSpan := openTok.Span

	if p.at(token.RParen) {
		closeTok := p.advance()
		listSpan = listSpan.Cover(closeTok.Span)
		return args, listSpan, true
	}

	for {
		argExpr, exprOK := p.parseExpr()
		if !exprOK {
			return nil, source.Span{}, false
		}
		args = append(args, argExpr)
		if node := p.arenas.Exprs.Get(argExpr); node != nil {
			listSpan = listSpan.Cover(node.Span)
		}

		if p.at(token.Comma) {
			commaTok := p.advance()
			listSpan = listSpan.Cover(commaTok.Span)
			if p.at(token.RParen) {
				closeTok := p.advance()
				listSpan = listSpan.Cover(closeTok.Span)
				return args, listSpan, true
			}
			continue
		}

		break
	}

	closeTok, ok := p.expect(token.RParen, diag.SynUnclosedParen, "expected ')' to close parallel argument list")
	if !ok {
		return nil, source.Span{}, false
	}
	listSpan = listSpan.Cover(closeTok.Span)
	return args, listSpan, true
}
