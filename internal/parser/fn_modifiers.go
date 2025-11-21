package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

type fnModifiers struct {
	flags     ast.FnModifier
	span      source.Span
	hasSpan   bool
	seenPub   bool
	seenAsync bool
}

func (m *fnModifiers) extend(sp source.Span) {
	if !m.hasSpan {
		m.span = sp
		m.hasSpan = true
		return
	}
	m.span = m.span.Cover(sp)
}

func (p *Parser) parseFnModifiers() fnModifiers {
	mods := fnModifiers{}

	for {
		tok := p.lx.Peek()
		switch tok.Kind {
		case token.KwFn:
			return mods
		case token.KwPub:
			tok = p.advance()
			if mods.seenPub {
				p.emitDiagnostic(
					diag.SynUnexpectedToken,
					diag.SevError,
					tok.Span,
					"duplicate 'pub' modifier",
					nil,
				)
			} else {
				mods.seenPub = true
				mods.flags |= ast.FnModifierPublic
			}
			mods.extend(tok.Span)

		case token.KwAsync:
			tok = p.advance()
			if mods.seenAsync {
				p.emitDiagnostic(
					diag.SynUnexpectedToken,
					diag.SevError,
					tok.Span,
					"duplicate 'async' modifier",
					func(b *diag.ReportBuilder) {
						if b == nil {
							return
						}
						fixID := fix.MakeFixID(diag.SynUnexpectedToken, tok.Span)
						suggestion := fix.DeleteSpan(
							"remove the duplicate 'async' modifier",
							tok.Span.ExtendRight(p.lx.Peek().Span),
							"",
							fix.WithID(fixID),
							fix.WithKind(diag.FixKindRefactor),
							fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
						)
						b.WithFixSuggestion(suggestion)
						b.WithNote(tok.Span, "Async modifier can be only once")
					},
				)
			} else {
				mods.seenAsync = true
				mods.flags |= ast.FnModifierAsync
			}
			mods.extend(tok.Span)

		case token.KwExtern:
			tok = p.advance()
			p.emitDiagnostic(
				diag.SynUnexpectedToken,
				diag.SevError,
				tok.Span,
				"'extern' cannot be used as a function modifier",
				nil,
			)
			mods.extend(tok.Span)
			continue
		case token.Ident:
			tok = p.advance()
			msg := "unknown function modifier"
			note := "Possible fn modifier: pub, async"
			if tok.Text == "unsafe" {
				msg = "'unsafe' must be specified via attribute"
				note = "'unsafe' should be declared via attribute before the function"
			} else if tok.Text != "" {
				msg = "unknown function modifier '" + tok.Text + "'"
			}
			p.emitDiagnostic(
				diag.SynUnexpectedModifier,
				diag.SevError,
				tok.Span,
				msg,
				func(b *diag.ReportBuilder) {
					if b == nil {
						return
					}
					fixID := fix.MakeFixID(diag.SynUnexpectedModifier, tok.Span)
					suggestion := fix.DeleteSpan(
						"remove the unknown function modifier",
						tok.Span.ExtendRight(p.lx.Peek().Span),
						"",
						fix.WithID(fixID),
						fix.WithKind(diag.FixKindRefactor),
						fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
					)
					b.WithFixSuggestion(suggestion)
					b.WithNote(tok.Span, note)
				},
			)
			mods.extend(tok.Span)
			continue
		case token.EOF:
			return mods
		default:
			if isTopLevelStarter(tok.Kind) || tok.Kind == token.Semicolon {
				return mods
			}
			return mods
		}
	}
}
