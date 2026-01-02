package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/token"
)

func (p *Parser) parseSelectExpr() (ast.ExprID, bool) {
	return p.parseSelectOrRaceExpr(false)
}

func (p *Parser) parseRaceExpr() (ast.ExprID, bool) {
	return p.parseSelectOrRaceExpr(true)
}

func (p *Parser) parseSelectOrRaceExpr(isRace bool) (ast.ExprID, bool) {
	p.allowFatArrow++
	defer func() { p.allowFatArrow-- }()

	kwTok := p.advance()
	kindLabel := "select"
	if isRace {
		kindLabel = "race"
	}

	openTok, ok := p.expect(token.LBrace, diag.SynUnexpectedToken, "expected '{' to start "+kindLabel+" arms", nil)
	if !ok {
		return ast.NoExprID, false
	}

	var arms []ast.ExprSelectArm
	for !p.at(token.RBrace) && !p.at(token.EOF) {
		arm, armOK := p.parseSelectArm()
		if !armOK {
			p.resyncUntil(token.Semicolon, token.RBrace, token.EOF)
			if p.at(token.Semicolon) {
				p.advance()
			}
			if p.at(token.RBrace) {
				break
			}
			continue
		}
		arms = append(arms, arm)

		if p.at(token.Semicolon) {
			p.advance()
			if p.at(token.RBrace) {
				break
			}
		}
		if p.at(token.Comma) {
			commaTok := p.advance()
			p.emitDiagnostic(
				diag.SynExpectSemicolon,
				diag.SevError,
				commaTok.Span,
				"expected ';' after "+kindLabel+" arm",
				nil,
			)
			if p.at(token.RBrace) {
				break
			}
		}
	}

	closeTok, ok := p.expect(token.RBrace, diag.SynUnclosedBrace, "expected '}' to close "+kindLabel+" expression", nil)
	if !ok {
		return ast.NoExprID, false
	}

	exprSpan := kwTok.Span.Cover(openTok.Span)
	for _, arm := range arms {
		if arm.Span.File != 0 {
			exprSpan = exprSpan.Cover(arm.Span)
		}
	}
	exprSpan = exprSpan.Cover(closeTok.Span)

	if isRace {
		return p.arenas.Exprs.NewRace(exprSpan, arms), true
	}
	return p.arenas.Exprs.NewSelect(exprSpan, arms), true
}

func (p *Parser) parseSelectArm() (ast.ExprSelectArm, bool) {
	arm := ast.ExprSelectArm{}

	var armStart source.Span
	if p.at(token.Ident) && p.lx.Peek().Text == "default" {
		defaultTok := p.advance()
		arm.IsDefault = true
		armStart = defaultTok.Span
	} else {
		awaitExpr, ok := p.parseExprOrBlockAsValue()
		if !ok {
			return arm, false
		}
		arm.Await = awaitExpr
		if node := p.arenas.Exprs.Get(awaitExpr); node != nil {
			armStart = node.Span
		}
	}

	if _, ok := p.expect(token.FatArrow, diag.SynUnexpectedToken, "expected '=>' after select arm", nil); !ok {
		return arm, false
	}

	resultExpr, ok := p.parseExprOrBlockAsValue()
	if !ok {
		return arm, false
	}
	arm.Result = resultExpr

	resultSpan := source.Span{}
	if node := p.arenas.Exprs.Get(resultExpr); node != nil {
		resultSpan = node.Span
	}
	if armStart.File != 0 {
		if resultSpan.File != 0 {
			arm.Span = armStart.Cover(resultSpan)
		} else {
			arm.Span = armStart
		}
	} else {
		arm.Span = resultSpan
	}
	return arm, true
}
