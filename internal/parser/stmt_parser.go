package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

func (p *Parser) parseBlock() (ast.StmtID, bool) {
	if !p.at(token.LBrace) {
		return ast.NoStmtID, false
	}

	openTok := p.advance()
	var stmtIDs []ast.StmtID

	for !p.at(token.EOF) && !p.at(token.RBrace) {
		if isBlockRecoveryToken(p.lx.Peek().Kind) {
			break
		}

		stmtID, ok := p.parseStmt()
		if ok {
			stmtIDs = append(stmtIDs, stmtID)
			continue
		}

		// ошибка при парсинге statement — восстанавливаемся до следующего statement
		p.resyncStatement()
		if p.at(token.Semicolon) {
			p.advance()
		}
		if p.at(token.RBrace) || p.at(token.EOF) || isBlockRecoveryToken(p.lx.Peek().Kind) {
			break
		}
	}

	closeTok, ok := p.expect(token.RBrace, diag.SynUnclosedBrace, "expected '}' to close block", func(b *diag.ReportBuilder) {
		if b == nil {
			return
		}
		insertSpan := p.lastSpan.ZeroideToEnd()
		fixID := fix.MakeFixID(diag.SynUnclosedBrace, insertSpan)
		suggestion := fix.InsertText(
			"insert '}' to close block",
			insertSpan,
			"}",
			"",
			fix.WithID(fixID),
			fix.WithKind(diag.FixKindRefactor),
			fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
		)
		b.WithFixSuggestion(suggestion)
		b.WithNote(insertSpan, "insert missing closing brace")
	})
	closeSpan := closeTok.Span
	if !ok {
		closeSpan = p.currentErrorSpan()
	}

	blockSpan := openTok.Span.Cover(closeSpan)
	blockID := p.arenas.Stmts.NewBlock(blockSpan, stmtIDs)
	return blockID, true
}

func (p *Parser) parseSignalStmt() (ast.StmtID, bool) {
	signalTok := p.advance()

	nameID, ok := p.parseIdent()
	if !ok {
		return ast.NoStmtID, false
	}

	assignTok, ok := p.expect(token.ColonAssign, diag.SynUnexpectedToken, "expected ':=' after signal target")
	if !ok {
		return ast.NoStmtID, false
	}

	valueExpr, ok := p.parseExpr()
	if !ok {
		return ast.NoStmtID, false
	}

	insertSpan := p.lastSpan.ZeroideToEnd()
	semiTok, semiOK := p.expect(
		token.Semicolon,
		diag.SynExpectSemicolon,
		"expected ';' after signal statement",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertSpan)
			suggestion := fix.InsertText(
				"insert ';' after signal statement",
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
	)
	if !semiOK {
		return ast.NoStmtID, false
	}

	stmtSpan := signalTok.Span
	if assignTok.Kind != token.Invalid {
		stmtSpan = stmtSpan.Cover(assignTok.Span)
	}
	if node := p.arenas.Exprs.Get(valueExpr); node != nil {
		stmtSpan = stmtSpan.Cover(node.Span)
	}
	if semiTok.Kind != token.Invalid {
		stmtSpan = stmtSpan.Cover(semiTok.Span)
	}

	stmtID := p.arenas.Stmts.NewSignal(stmtSpan, nameID, valueExpr)
	return stmtID, true
}

func (p *Parser) parseStmt() (ast.StmtID, bool) {
	switch p.lx.Peek().Kind {
	case token.LBrace:
		return p.parseBlock()
	case token.KwPub:
		pubTok := p.advance()
		p.emitDiagnostic(
			diag.SynModifierNotAllowed,
			diag.SevError,
			pubTok.Span,
			"'pub' is only allowed for top-level declarations",
			func(b *diag.ReportBuilder) {
				if b == nil {
					return
				}
				fixID := fix.MakeFixID(diag.SynModifierNotAllowed, pubTok.Span)
				suggestion := fix.DeleteSpan(
					"remove 'pub' modifier",
					pubTok.Span,
					"",
					fix.WithID(fixID),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
				)
				b.WithFixSuggestion(suggestion)
				b.WithNote(pubTok.Span, "'pub' modifiers are only valid for top-level items")
			},
		)
		return p.parseStmt()
	case token.At:
		return p.parseAttributedStmt()
	case token.KwAsync:
		asyncTok := p.advance()
		p.emitDiagnostic(
			diag.SynAsyncNotAllowed,
			diag.SevError,
			asyncTok.Span,
			"'async' modifier is not allowed inside blocks",
			func(b *diag.ReportBuilder) {
				if b == nil {
					return
				}
				fixID := fix.MakeFixID(diag.SynAsyncNotAllowed, asyncTok.Span)
				suggestion := fix.DeleteSpan(
					"remove 'async'",
					asyncTok.Span,
					"",
					fix.WithID(fixID),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
				)
				b.WithFixSuggestion(suggestion)
				b.WithNote(asyncTok.Span, "move async usage to top-level declarations")
			},
		)
		return p.parseStmt()
	case token.KwLet:
		return p.parseLetStmt()
	case token.KwSignal:
		return p.parseSignalStmt()
	case token.KwReturn:
		return p.parseReturnStmt()
	case token.KwIf:
		return p.parseIfStmt()
	case token.KwWhile:
		return p.parseWhileStmt()
	case token.KwFor:
		return p.parseForStmt()
	case token.KwBreak:
		return p.parseBreakStmt()
	case token.KwContinue:
		return p.parseContinueStmt()
	case token.KwType:
		typeTok := p.advance()
		p.emitDiagnostic(
			diag.SynTypeNotAllowed,
			diag.SevError,
			typeTok.Span,
			"type declarations are not allowed inside blocks",
			nil,
		)
		return ast.NoStmtID, false
	default:
		return p.parseExprStmt()
	}
}

func (p *Parser) parseAttributedStmt() (ast.StmtID, bool) {
	attrs, attrSpan, ok := p.parseAttributes()
	if !ok {
		return ast.NoStmtID, false
	}
	if stmtID, handled := p.tryParseDropStmt(attrs, attrSpan); handled {
		return stmtID, true
	}
	p.emitDiagnostic(
		diag.SynAttributeNotAllowed,
		diag.SevError,
		attrSpan,
		"attributes are not allowed on statements (except '@drop')",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynAttributeNotAllowed, attrSpan)
			suggestion := fix.DeleteSpan(
				"remove statement attribute",
				attrSpan,
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(attrSpan, "remove unsupported attribute or replace with '@drop'")
		},
	)
	return p.parseStmt()
}

func (p *Parser) tryParseDropStmt(attrs []ast.Attr, attrSpan source.Span) (ast.StmtID, bool) {
	if len(attrs) != 1 || p.arenas == nil || p.arenas.StringsInterner == nil {
		return ast.NoStmtID, false
	}
	attr := attrs[0]
	spec, ok := ast.LookupAttrID(p.arenas.StringsInterner, attr.Name)
	if !ok || spec.Name != "drop" {
		return ast.NoStmtID, false
	}
	if len(attr.Args) > 0 {
		p.emitDiagnostic(
			diag.SynUnexpectedToken,
			diag.SevError,
			attr.Span,
			"'@drop' does not accept arguments",
			nil,
		)
		return ast.NoStmtID, false
	}

	exprID, ok := p.parseExpr()
	if !ok {
		return ast.NoStmtID, true
	}

	insertSpan := p.lastSpan.ZeroideToEnd()
	semiTok, semiOK := p.expect(
		token.Semicolon,
		diag.SynExpectSemicolon,
		"expected ';' after @drop expression",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertSpan)
			suggestion := fix.InsertText(
				"insert ';' after @drop expression",
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
	)
	if !semiOK {
		return ast.NoStmtID, true
	}

	stmtSpan := attrSpan
	if node := p.arenas.Exprs.Get(exprID); node != nil {
		stmtSpan = stmtSpan.Cover(node.Span)
	}
	if semiTok.Kind != token.Invalid {
		stmtSpan = stmtSpan.Cover(semiTok.Span)
	}
	stmtID := p.arenas.Stmts.NewDrop(stmtSpan, exprID)
	return stmtID, true
}

func (p *Parser) parseLetStmt() (ast.StmtID, bool) {
	letTok := p.advance()

	binding, ok := p.parseLetBinding()
	if !ok {
		return ast.NoStmtID, false
	}

	insertSpan := p.lastSpan.ZeroideToEnd()
	semiTok, semiOK := p.expect(
		token.Semicolon,
		diag.SynExpectSemicolon,
		"expected ';' after let statement",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertSpan)
			suggestion := fix.InsertText(
				"insert ';' after let statement",
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
	if !semiOK {
		return ast.NoStmtID, false
	}

	stmtSpan := coverOptional(letTok.Span, binding.Span)
	stmtSpan = stmtSpan.Cover(semiTok.Span)
	stmtID := p.arenas.Stmts.NewLet(stmtSpan, binding.Name, binding.Type, binding.Value, binding.IsMut)
	return stmtID, true
}

func (p *Parser) parseReturnStmt() (ast.StmtID, bool) {
	retTok := p.advance()

	exprID := ast.NoExprID
	if !p.at(token.Semicolon) && !p.at(token.RBrace) && !p.at(token.EOF) {
		var ok bool
		exprID, ok = p.parseExpr()
		if !ok {
			return ast.NoStmtID, false
		}
	}

	insertSpan := p.lastSpan.ZeroideToEnd()
	semiTok, semiOK := p.expect(
		token.Semicolon,
		diag.SynExpectSemicolon,
		"expected ';' after return statement",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertSpan)
			suggestion := fix.InsertText(
				"insert ';' after return statement",
				insertSpan,
				";",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
				fix.Preferred(),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert missing semicolon")
		},
	)
	if !semiOK {
		return ast.NoStmtID, false
	}

	stmtSpan := retTok.Span
	if exprID.IsValid() {
		exprSpan := p.arenas.Exprs.Get(exprID).Span
		stmtSpan = stmtSpan.Cover(exprSpan)
	}
	stmtSpan = stmtSpan.Cover(semiTok.Span)

	stmtID := p.arenas.Stmts.NewReturn(stmtSpan, exprID)
	return stmtID, true
}

func (p *Parser) parseExprStmt() (ast.StmtID, bool) {
	exprID, ok := p.parseExpr()
	if !ok {
		return ast.NoStmtID, false
	}

	insertSpan := p.lastSpan.ZeroideToEnd()
	semiTok, semiOK := p.expect(
		token.Semicolon,
		diag.SynExpectSemicolon,
		"expected ';' after expression statement",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertSpan)
			suggestion := fix.InsertText(
				"insert ';' after expression statement",
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
	if !semiOK {
		return ast.NoStmtID, false
	}

	exprSpan := p.arenas.Exprs.Get(exprID).Span
	stmtSpan := exprSpan.Cover(semiTok.Span)
	stmtID := p.arenas.Stmts.NewExpr(stmtSpan, exprID)
	return stmtID, true
}

// coverOptional returns the span that covers base and other, or base if other is the zero span.
// The other span is considered zero when its File, Start, and End fields are all zero.
func coverOptional(base, other source.Span) source.Span {
	if other.File == 0 && other.Start == 0 && other.End == 0 {
		return base
	}
	return base.Cover(other)
}
