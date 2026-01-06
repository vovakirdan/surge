package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

// ConstBinding represents a parsed constant declaration.
type ConstBinding struct {
	Name       source.StringID
	Type       ast.TypeID
	Value      ast.ExprID
	Span       source.Span
	NameSpan   source.Span
	ColonSpan  source.Span
	TypeSpan   source.Span
	AssignSpan source.Span
	ValueSpan  source.Span
}

func (p *Parser) parseConstBinding() (ConstBinding, bool) {
	startSpan := p.lx.Peek().Span

	nameID, ok := p.parseIdent()
	if !ok {
		return ConstBinding{}, false
	}
	nameSpan := p.lastSpan

	var colonSpan source.Span
	typeID, ok := func() (ast.TypeID, bool) {
		if p.at(token.Colon) {
			colonSpan = p.lx.Peek().Span
		}
		return p.parseTypeExpr()
	}()
	if !ok {
		return ConstBinding{}, false
	}
	var typeSpan source.Span
	if typeID.IsValid() {
		if typ := p.arenas.Types.Get(typeID); typ != nil {
			typeSpan = typ.Span
		}
	}

	assignTok, ok := p.expect(token.Assign, diag.SynUnexpectedToken, "expected '=' in const declaration", nil)
	if !ok {
		return ConstBinding{}, false
	}

	valueID, ok := p.parseExpr()
	if !ok {
		return ConstBinding{}, false
	}
	var valueSpan source.Span
	if expr := p.arenas.Exprs.Get(valueID); expr != nil {
		valueSpan = expr.Span
	}

	binding := ConstBinding{
		Name:       nameID,
		Type:       typeID,
		Value:      valueID,
		Span:       startSpan.Cover(p.lastSpan),
		NameSpan:   nameSpan,
		ColonSpan:  colonSpan,
		TypeSpan:   typeSpan,
		AssignSpan: assignTok.Span,
		ValueSpan:  valueSpan,
	}
	return binding, true
}

func (p *Parser) parseConstItemWithVisibility(attrs []ast.Attr, attrSpan source.Span, visibility ast.Visibility, prefixSpan source.Span, hasPrefix bool) (ast.ItemID, bool) {
	constTok := p.advance()

	binding, ok := p.parseConstBinding()
	if !ok {
		insertPos := p.lastSpan.ZeroideToEnd()
		p.resyncUntil(token.Semicolon, token.RBrace, token.EOF)
		if p.at(token.Semicolon) {
			p.advance()
		} else {
			p.emitDiagnostic(
				diag.SynExpectSemicolon,
				diag.SevError,
				insertPos,
				"expected semicolon after const item",
				func(b *diag.ReportBuilder) {
					if b == nil {
						return
					}
					fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertPos)
					suggestion := fix.InsertText(
						"insert semicolon after const item",
						insertPos,
						";",
						"",
						fix.WithID(fixID),
						fix.WithKind(diag.FixKindRefactor),
						fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
					)
					b.WithFixSuggestion(suggestion)
					b.WithNote(insertPos, "insert missing semicolon")
				},
			)
		}
		p.resyncTop()
		return ast.NoItemID, false
	}

	insertPos := p.lastSpan.ZeroideToEnd()

	if !p.at(token.Semicolon) {
		p.emitDiagnostic(
			diag.SynExpectSemicolon,
			diag.SevError,
			insertPos,
			"expected semicolon after const item",
			func(b *diag.ReportBuilder) {
				if b == nil {
					return
				}
				fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertPos)
				suggestion := fix.InsertText(
					"insert semicolon after const item",
					insertPos,
					";",
					"",
					fix.WithID(fixID),
					fix.WithKind(diag.FixKindRefactor),
					fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
					fix.Preferred(),
				)
				b.WithFixSuggestion(suggestion)
				b.WithNote(insertPos, "insert missing semicolon")
			},
		)
		p.resyncTop()
		return ast.NoItemID, false
	}
	semiTok := p.advance()

	finalSpan := constTok.Span.Cover(semiTok.Span)
	if attrSpan.End > attrSpan.Start {
		finalSpan = attrSpan.Cover(finalSpan)
	}
	if hasPrefix {
		finalSpan = prefixSpan.Cover(finalSpan)
	}

	itemID := p.arenas.Items.NewConst(
		binding.Name,
		binding.Type,
		binding.Value,
		visibility,
		attrs,
		constTok.Span,
		binding.NameSpan,
		binding.ColonSpan,
		binding.AssignSpan,
		semiTok.Span,
		finalSpan,
	)

	return itemID, true
}

func (p *Parser) parseConstStmt() (ast.StmtID, bool) {
	constTok := p.advance()

	binding, ok := p.parseConstBinding()
	if !ok {
		return ast.NoStmtID, false
	}

	insertPos := p.lastSpan.ZeroideToEnd()
	semiTok, semiOK := p.expect(
		token.Semicolon,
		diag.SynExpectSemicolon,
		"expected ';' after const statement",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertPos)
			suggestion := fix.InsertText(
				"insert ';' after const statement",
				insertPos,
				";",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertPos, "insert missing semicolon")
		},
	)
	if !semiOK {
		return ast.NoStmtID, false
	}

	stmtSpan := coverOptional(constTok.Span, binding.Span)
	stmtSpan = stmtSpan.Cover(semiTok.Span)
	stmtID := p.arenas.Stmts.NewConst(stmtSpan, binding.Name, binding.Type, binding.Value)
	return stmtID, true
}
