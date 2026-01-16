package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

func (p *Parser) parseCompareExpr() (ast.ExprID, bool) {
	p.allowFatArrow++
	defer func() { p.allowFatArrow-- }()
	compareTok := p.advance()

	subjectExpr, ok := p.parseExpr()
	if !ok {
		return ast.NoExprID, false
	}

	insertSpan := p.lastSpan.ZeroideToEnd()
	openTok, ok := p.expect(
		token.LBrace,
		diag.SynUnexpectedToken,
		"expected '{' to start compare arms",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynUnexpectedToken, insertSpan)
			suggestion := fix.InsertText(
				"insert '{' to start compare arms",
				insertSpan,
				" {",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert '{' before compare arms")
		},
	)
	if !ok {
		return ast.NoExprID, false
	}

	var arms []ast.ExprCompareArm
	for !p.at(token.RBrace) && !p.at(token.EOF) {
		arm, armOK := p.parseCompareArm()
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
				"expected ';' after compare arm",
				nil,
			)
			if p.at(token.RBrace) {
				break
			}
		}
	}

	closeTok, ok := p.expect(
		token.RBrace,
		diag.SynUnclosedBrace,
		"expected '}' to close compare expression",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			insert := p.lastSpan.ZeroideToEnd()
			fixID := fix.MakeFixID(diag.SynUnclosedBrace, insert)
			suggestion := fix.InsertText(
				"insert '}' to close compare expression",
				insert,
				"}",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insert, "insert missing '}'")
		},
	)
	if !ok {
		return ast.NoExprID, false
	}

	exprSpan := compareTok.Span
	if subject := p.arenas.Exprs.Get(subjectExpr); subject != nil {
		exprSpan = exprSpan.Cover(subject.Span)
	}
	exprSpan = exprSpan.Cover(openTok.Span)
	exprSpan = exprSpan.Cover(closeTok.Span)
	exprID := p.arenas.Exprs.NewCompare(exprSpan, subjectExpr, arms)
	return exprID, true
}

func (p *Parser) parseCompareArm() (ast.ExprCompareArm, bool) {
	arm := ast.ExprCompareArm{}

	if p.at(token.KwFinally) {
		finallyTok := p.advance()
		arm.IsFinally = true
		arm.PatternSpan = finallyTok.Span
		if p.at(token.KwIf) {
			ifTok := p.advance()
			p.emitDiagnostic(
				diag.SynUnexpectedToken,
				diag.SevError,
				ifTok.Span,
				"'if' guard is not allowed on 'finally' arm",
				nil,
			)
			// best-effort consume guard expression to avoid cascading errors
			p.parseExpr()
		}
	} else {
		patternStart := p.lx.Peek().Span
		patternExpr, ok := p.parseExpr()
		if !ok {
			return arm, false
		}
		arm.Pattern = patternExpr
		if node := p.arenas.Exprs.Get(patternExpr); node != nil {
			arm.PatternSpan = patternStart.Cover(node.Span)
		} else {
			arm.PatternSpan = patternStart
		}

		if p.at(token.KwIf) {
			p.advance()
			guardExpr, ok := p.parseExpr()
			if !ok {
				return arm, false
			}
			arm.Guard = guardExpr
		}
	}

	if _, ok := p.expect(token.FatArrow, diag.SynUnexpectedToken, "expected '=>' after compare arm pattern"); !ok {
		return arm, false
	}

	var resultExpr ast.ExprID
	if p.at(token.KwReturn) {
		stmtID, ok := p.parseReturnStmt()
		if !ok {
			return arm, false
		}
		stmt := p.arenas.Stmts.Get(stmtID)
		span := source.Span{}
		if stmt != nil {
			span = stmt.Span
		}
		resultExpr = p.arenas.Exprs.NewBlock(span, []ast.StmtID{stmtID})
		p.normalizeBlockExprValue(resultExpr)
	} else if p.at(token.KwBreak) {
		stmtID, ok := p.parseBreakStmt()
		if !ok {
			return arm, false
		}
		stmt := p.arenas.Stmts.Get(stmtID)
		span := source.Span{}
		if stmt != nil {
			span = stmt.Span
		}
		resultExpr = p.arenas.Exprs.NewBlock(span, []ast.StmtID{stmtID})
		p.normalizeBlockExprValue(resultExpr)
	} else if p.at(token.KwContinue) {
		stmtID, ok := p.parseContinueStmt()
		if !ok {
			return arm, false
		}
		stmt := p.arenas.Stmts.Get(stmtID)
		span := source.Span{}
		if stmt != nil {
			span = stmt.Span
		}
		resultExpr = p.arenas.Exprs.NewBlock(span, []ast.StmtID{stmtID})
		p.normalizeBlockExprValue(resultExpr)
	} else {
		var ok bool
		resultExpr, ok = p.parseExprOrBlockAsValue()
		if !ok {
			return arm, false
		}
	}
	arm.Result = resultExpr
	return arm, true
}
