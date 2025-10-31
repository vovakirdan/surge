package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/token"
)

func (p *Parser) consumeModulePragma() {
	if !p.at(token.KwPragma) {
		return
	}
	p.parsePragma(true)
	for p.at(token.KwPragma) {
		p.parsePragma(false)
	}
}

func (p *Parser) parsePragma(atTop bool) {
	pragmaTok := p.advance()

	entries, lastSpan, flags := p.parsePragmaEntries()
	totalSpan := pragmaTok.Span
	if lastSpan != (source.Span{}) {
		totalSpan = totalSpan.Cover(lastSpan)
	}

	if !atTop || p.pragmaParsed {
		p.emitDiagnostic(
			diag.SynPragmaPosition,
			diag.SevError,
			pragmaTok.Span,
			"pragma must appear before any module items",
			nil,
		)
		return
	}

	if len(entries) == 0 {
		p.emitDiagnostic(
			diag.SynUnexpectedToken,
			diag.SevError,
			pragmaTok.Span,
			"expected at least one pragma flag",
			nil,
		)
		return
	}

	file := p.arenas.Files.Get(p.file)
	if file != nil {
		file.Pragma = ast.Pragma{
			Span:    totalSpan,
			Flags:   flags,
			Entries: entries,
		}
	}
	p.pragmaParsed = true
}

func (p *Parser) parsePragmaEntries() ([]ast.PragmaEntry, source.Span, ast.PragmaFlags) {
	var (
		entries  []ast.PragmaEntry
		lastSpan source.Span
		flags    ast.PragmaFlags
	)

	expectEntry := true
	for {
		peek := p.lx.Peek()
		if peek.Kind == token.EOF {
			break
		}

		if containsNewlineTrivia(peek.Leading) {
			break
		}

		if peek.Kind == token.Comma {
			expectEntry = true
			lastSpan = p.advance().Span
			continue
		}

		if peek.Kind != token.Ident {
			if expectEntry {
				p.emitDiagnostic(
					diag.SynUnexpectedToken,
					diag.SevError,
					peek.Span,
					"expected identifier after 'pragma'",
					nil,
				)
			}
			break
		}

		nameTok := p.advance()
		entryStart := nameTok.Span
		entryEnd := nameTok.Span

		parenDepth := 0
		for {
			next := p.lx.Peek()
			if next.Kind == token.EOF {
				break
			}
			if parenDepth == 0 {
				if next.Kind == token.Comma {
					break
				}
				if containsNewlineTrivia(next.Leading) {
					break
				}
			}

			if next.Kind == token.LParen {
				parenDepth++
			} else if next.Kind == token.RParen && parenDepth > 0 {
				parenDepth--
			}

			entryEnd = next.Span
			p.advance()
		}

		entrySpan := entryStart
		if entryEnd != (source.Span{}) {
			entrySpan = entryStart.Cover(entryEnd)
		}
		rawText := p.textForSpan(entrySpan)
		entry := ast.PragmaEntry{
			Name: p.arenas.StringsInterner.Intern(nameTok.Text),
			Raw:  p.arenas.StringsInterner.Intern(rawText),
			Span: entrySpan,
		}
		switch nameTok.Text {
		case "directive":
			flags |= ast.PragmaFlagDirective
		case "no_std":
			flags |= ast.PragmaFlagNoStd
		case "strict":
			flags |= ast.PragmaFlagStrict
		case "unsafe":
			flags |= ast.PragmaFlagUnsafe
		}
		entries = append(entries, entry)
		lastSpan = entrySpan

		if p.at(token.Comma) {
			lastSpan = p.advance().Span
			expectEntry = true
			continue
		}

		next := p.lx.Peek()
		if next.Kind == token.EOF || containsNewlineTrivia(next.Leading) {
			break
		}

		p.emitDiagnostic(
			diag.SynUnexpectedToken,
			diag.SevError,
			next.Span,
			"unexpected token in pragma directive",
			nil,
		)
		break
	}

	return entries, lastSpan, flags
}

func (p *Parser) textForSpan(sp source.Span) string {
	if sp == (source.Span{}) {
		return ""
	}
	file := p.fs.Get(sp.File)
	if file == nil {
		return ""
	}
	if sp.End < sp.Start {
		return ""
	}
	contentLen := len(file.Content)
	start := int(sp.Start)
	end := int(sp.End)
	if start < 0 || end < 0 || start > contentLen || end > contentLen {
		return ""
	}
	return string(file.Content[start:end])
}

func containsNewlineTrivia(trivia []token.Trivia) bool {
	for _, tr := range trivia {
		if tr.Kind == token.TriviaNewline {
			return true
		}
	}
	return false
}
