package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/token"
)

func (p *Parser) parseBlock() (ast.StmtID, bool) {
	if !p.at(token.LBrace) {
		return ast.NoStmtID, false
	}

	open := p.advance()
	depth := 1
	for !p.at(token.EOF) {
		tok := p.advance()
		switch tok.Kind {
		case token.LBrace:
			depth++
		case token.RBrace:
			depth--
			if depth == 0 {
				span := open.Span.Cover(tok.Span)
				return p.arenas.NewStmt(ast.StmtBlock, span), true
			}
		}
	}

	insertSpan := p.lastSpan.ZeroideToEnd()
	p.emitDiagnostic(
		diag.SynUnclosedBrace,
		diag.SevError,
		insertSpan,
		"expected '}' to close block",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
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
		},
	)
	return ast.NoStmtID, false
}

func (p *Parser) parseStmt() (ast.StmtID, bool) {
	// TODO: реализовать statement parser
	return ast.NoStmtID, false
}
