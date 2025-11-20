package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/token"
)

func (p *Parser) parseBreakStmt() (ast.StmtID, bool) {
	breakTok := p.advance()

	insertSpan := p.lastSpan.ZeroideToEnd()
	semiTok, ok := p.expect(
		token.Semicolon,
		diag.SynExpectSemicolon,
		"expected ';' after break statement",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertSpan)
			suggestion := fix.InsertText(
				"insert ';' after break",
				insertSpan,
				";",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert missing semicolon")
		},
	)
	if !ok {
		return ast.NoStmtID, false
	}

	stmtSpan := breakTok.Span.Cover(semiTok.Span)
	return p.arenas.Stmts.NewBreak(stmtSpan), true
}

func (p *Parser) parseContinueStmt() (ast.StmtID, bool) {
	continueTok := p.advance()

	insertSpan := p.lastSpan.ZeroideToEnd()
	semiTok, ok := p.expect(
		token.Semicolon,
		diag.SynExpectSemicolon,
		"expected ';' after continue statement",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertSpan)
			suggestion := fix.InsertText(
				"insert ';' after continue",
				insertSpan,
				";",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert missing semicolon")
		},
	)
	if !ok {
		return ast.NoStmtID, false
	}

	stmtSpan := continueTok.Span.Cover(semiTok.Span)
	return p.arenas.Stmts.NewContinue(stmtSpan), true
}

func (p *Parser) parseIfStmt() (ast.StmtID, bool) {
	ifTok := p.advance()

	useParens := p.at(token.LParen)
	if useParens {
		p.advance()
	}

	condExpr, ok := p.parseExpr()
	if !ok {
		if !useParens {
			p.err(diag.SynExpectExpression, "expected condition expression after 'if'")
		}
		return ast.NoStmtID, false
	}

	var closeTok token.Token
	if useParens {
		var expectOK bool
		closeTok, expectOK = p.expect(
			token.RParen,
			diag.SynUnclosedParen,
			"expected ')' to close if condition",
			func(b *diag.ReportBuilder) {
				if b == nil {
					return
				}
				insertSpan := p.lastSpan.ZeroideToEnd()
				fixID := fix.MakeFixID(diag.SynUnclosedParen, insertSpan)
				suggestion := fix.InsertText(
					"insert ')' to close if condition",
					insertSpan,
					")",
					"",
					fix.WithID(fixID),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
				)
				b.WithFixSuggestion(suggestion)
				b.WithNote(insertSpan, "insert missing ')'")
			},
		)
		if !expectOK {
			return ast.NoStmtID, false
		}
	} else {
		closeTok = p.lx.Peek()
	}

	if !p.at(token.LBrace) {
		p.emitDiagnostic(
			diag.SynUnexpectedToken,
			diag.SevError,
			p.lx.Peek().Span,
			"expected '{' to start if body",
			nil,
		)
		return ast.NoStmtID, false
	}

	thenStmt, ok := p.parseBlock()
	if !ok {
		return ast.NoStmtID, false
	}

	stmtSpan := ifTok.Span
	if useParens {
		stmtSpan = stmtSpan.Cover(closeTok.Span)
	} else if cond := p.arenas.Exprs.Get(condExpr); cond != nil {
		stmtSpan = stmtSpan.Cover(cond.Span)
	}
	if thenNode := p.arenas.Stmts.Get(thenStmt); thenNode != nil {
		stmtSpan = stmtSpan.Cover(thenNode.Span)
	}

	elseStmt := ast.NoStmtID
	var elseTok token.Token
	if p.at(token.KwElse) {
		elseTok = p.advance()
		switch p.lx.Peek().Kind {
		case token.KwIf:
			var ok bool
			elseStmt, ok = p.parseIfStmt()
			if !ok {
				return ast.NoStmtID, false
			}
		case token.LBrace:
			var ok bool
			elseStmt, ok = p.parseBlock()
			if !ok {
				return ast.NoStmtID, false
			}
		default:
			p.emitDiagnostic(
				diag.SynUnexpectedToken,
				diag.SevError,
				p.lx.Peek().Span,
				"expected 'if' or block after 'else'",
				nil,
			)
			return ast.NoStmtID, false
		}

		stmtSpan = stmtSpan.Cover(elseTok.Span)
		if elseNode := p.arenas.Stmts.Get(elseStmt); elseNode != nil {
			stmtSpan = stmtSpan.Cover(elseNode.Span)
		}
	}

	return p.arenas.Stmts.NewIf(stmtSpan, condExpr, thenStmt, elseStmt), true
}

func (p *Parser) parseWhileStmt() (ast.StmtID, bool) {
	whileTok := p.advance()

	useParens := p.at(token.LParen)
	if useParens {
		p.advance()
	}

	condExpr, ok := p.parseExpr()
	if !ok {
		if !useParens {
			p.err(diag.SynExpectExpression, "expected condition expression after 'while'")
		}
		return ast.NoStmtID, false
	}

	var closeTok token.Token
	if useParens {
		var expectOK bool
		closeTok, expectOK = p.expect(
			token.RParen,
			diag.SynUnclosedParen,
			"expected ')' to close while condition",
			func(b *diag.ReportBuilder) {
				if b == nil {
					return
				}
				insertSpan := p.lastSpan.ZeroideToEnd()
				fixID := fix.MakeFixID(diag.SynUnclosedParen, insertSpan)
				suggestion := fix.InsertText(
					"insert ')' to close while condition",
					insertSpan,
					")",
					"",
					fix.WithID(fixID),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
				)
				b.WithFixSuggestion(suggestion)
				b.WithNote(insertSpan, "insert missing ')'")
			},
		)
		if !expectOK {
			return ast.NoStmtID, false
		}
	} else {
		closeTok = p.lx.Peek()
	}

	if !p.at(token.LBrace) {
		p.emitDiagnostic(
			diag.SynUnexpectedToken,
			diag.SevError,
			p.lx.Peek().Span,
			"expected '{' to start while body",
			nil,
		)
		return ast.NoStmtID, false
	}

	bodyStmt, ok := p.parseBlock()
	if !ok {
		return ast.NoStmtID, false
	}

	stmtSpan := whileTok.Span
	if useParens {
		stmtSpan = stmtSpan.Cover(closeTok.Span)
	} else if cond := p.arenas.Exprs.Get(condExpr); cond != nil {
		stmtSpan = stmtSpan.Cover(cond.Span)
	}
	if body := p.arenas.Stmts.Get(bodyStmt); body != nil {
		stmtSpan = stmtSpan.Cover(body.Span)
	}

	return p.arenas.Stmts.NewWhile(stmtSpan, condExpr, bodyStmt), true
}

func (p *Parser) parseForStmt() (ast.StmtID, bool) {
	forTok := p.advance()
	if p.at(token.LParen) {
		return p.parseForClassic(forTok)
	}
	return p.parseForIn(forTok)
}

func (p *Parser) parseForClassic(forTok token.Token) (ast.StmtID, bool) {
	openTok := p.advance()
	_ = openTok

	initStmt := ast.NoStmtID
	condExpr := ast.NoExprID
	postExpr := ast.NoExprID

	if !p.at(token.Semicolon) {
		var ok bool
		initStmt, ok = p.parseForInitializer()
		if !ok {
			return ast.NoStmtID, false
		}
	}

	insertSpan := p.lastSpan.ZeroideToEnd()
	if _, ok := p.expect(
		token.Semicolon,
		diag.SynForBadHeader,
		"expected ';' after for initializer",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynForBadHeader, insertSpan)
			suggestion := fix.InsertText(
				"insert ';' after initializer",
				insertSpan,
				";",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert missing ';'")
		},
	); !ok {
		return ast.NoStmtID, false
	}

	if !p.at(token.Semicolon) {
		var ok bool
		condExpr, ok = p.parseExpr()
		if !ok {
			return ast.NoStmtID, false
		}
	}

	insertSpan = p.lastSpan.ZeroideToEnd()
	if _, ok := p.expect(
		token.Semicolon,
		diag.SynForBadHeader,
		"expected ';' before for update clause",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynForBadHeader, insertSpan)
			suggestion := fix.InsertText(
				"insert ';' before update clause",
				insertSpan,
				";",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert missing ';'")
		},
	); !ok {
		return ast.NoStmtID, false
	}

	if !p.at(token.RParen) {
		var ok bool
		postExpr, ok = p.parseExpr()
		if !ok {
			return ast.NoStmtID, false
		}
	}

	closeTok, ok := p.expect(
		token.RParen,
		diag.SynUnclosedParen,
		"expected ')' to close for header",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			insertSpan := p.lastSpan.ZeroideToEnd()
			fixID := fix.MakeFixID(diag.SynUnclosedParen, insertSpan)
			suggestion := fix.InsertText(
				"insert ')' to close for header",
				insertSpan,
				")",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert missing ')'")
		},
	)
	if !ok {
		return ast.NoStmtID, false
	}

	if !p.at(token.LBrace) {
		p.emitDiagnostic(
			diag.SynUnexpectedToken,
			diag.SevError,
			p.lx.Peek().Span,
			"expected '{' to start for body",
			nil,
		)
		return ast.NoStmtID, false
	}

	bodyStmt, ok := p.parseBlock()
	if !ok {
		return ast.NoStmtID, false
	}

	stmtSpan := forTok.Span.Cover(closeTok.Span)
	if body := p.arenas.Stmts.Get(bodyStmt); body != nil {
		stmtSpan = stmtSpan.Cover(body.Span)
	}

	return p.arenas.Stmts.NewForClassic(stmtSpan, initStmt, condExpr, postExpr, bodyStmt), true
}

func (p *Parser) parseForInitializer() (ast.StmtID, bool) {
	if p.at(token.KwConst) {
		constTok := p.advance()
		binding, ok := p.parseConstBinding()
		if !ok {
			return ast.NoStmtID, false
		}
		stmtSpan := coverOptional(constTok.Span, binding.Span)
		stmtID := p.arenas.Stmts.NewConst(stmtSpan, binding.Name, binding.Type, binding.Value)
		return stmtID, true
	}
	if p.at(token.KwLet) {
		letTok := p.advance()
		binding, ok := p.parseLetBinding()
		if !ok {
			return ast.NoStmtID, false
		}
		stmtSpan := coverOptional(letTok.Span, binding.Span)
		stmtID := p.arenas.Stmts.NewLet(stmtSpan, binding.Name, binding.Type, binding.Value, binding.IsMut)
		return stmtID, true
	}

	exprID, ok := p.parseExpr()
	if !ok {
		return ast.NoStmtID, false
	}
	exprSpan := p.arenas.Exprs.Get(exprID).Span
	stmtID := p.arenas.Stmts.NewExpr(exprSpan, exprID)
	return stmtID, true
}

func (p *Parser) parseForIn(forTok token.Token) (ast.StmtID, bool) {
	nameTok := p.lx.Peek()
	nameID, ok := p.parseIdent()
	if !ok {
		return ast.NoStmtID, false
	}
	patternSpan := coverOptional(nameTok.Span, p.lastSpan)

	typeID, ok := p.parseTypeExpr()
	if !ok {
		return ast.NoStmtID, false
	}
	if typeID.IsValid() {
		if typ := p.arenas.Types.Get(typeID); typ != nil {
			patternSpan = coverOptional(patternSpan, typ.Span)
		}
	}

	insertSpan := p.lastSpan.ZeroideToEnd()
	inTok, ok := p.expect(
		token.KwIn,
		diag.SynForMissingIn,
		"expected 'in' in for-in loop header",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynForMissingIn, insertSpan)
			suggestion := fix.InsertText(
				"insert ' in ' between loop variable and iterable",
				insertSpan,
				" in ",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert missing 'in'")
		},
	)
	if !ok {
		return ast.NoStmtID, false
	}

	iterExpr, ok := p.parseExpr()
	if !ok {
		return ast.NoStmtID, false
	}

	if !p.at(token.LBrace) {
		p.emitDiagnostic(
			diag.SynUnexpectedToken,
			diag.SevError,
			p.lx.Peek().Span,
			"expected '{' to start for body",
			nil,
		)
		return ast.NoStmtID, false
	}

	bodyStmt, ok := p.parseBlock()
	if !ok {
		return ast.NoStmtID, false
	}

	stmtSpan := forTok.Span
	stmtSpan = stmtSpan.Cover(patternSpan)
	stmtSpan = stmtSpan.Cover(inTok.Span)
	if iter := p.arenas.Exprs.Get(iterExpr); iter != nil {
		stmtSpan = stmtSpan.Cover(iter.Span)
	}
	if body := p.arenas.Stmts.Get(bodyStmt); body != nil {
		stmtSpan = stmtSpan.Cover(body.Span)
	}

	return p.arenas.Stmts.NewForIn(stmtSpan, nameID, patternSpan, typeID, iterExpr, bodyStmt), true
}
