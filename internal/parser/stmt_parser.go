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
		if p.at(token.RBrace) || p.at(token.EOF) {
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
	if !ok {
		return ast.NoStmtID, false
	}

	blockSpan := openTok.Span.Cover(closeTok.Span)
	blockID := p.arenas.Stmts.NewBlock(blockSpan, stmtIDs)
	return blockID, true
}

func (p *Parser) parseStmt() (ast.StmtID, bool) {
	switch p.lx.Peek().Kind {
	case token.KwLet:
		return p.parseLetStmt()
	case token.KwReturn:
		return p.parseReturnStmt()
	default:
		return p.parseExprStmt()
	}
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

	var exprID ast.ExprID = ast.NoExprID
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
func coverOptional(base source.Span, other source.Span) source.Span {
	if other.File == 0 && other.Start == 0 && other.End == 0 {
		return base
	}
	return base.Cover(other)
}
