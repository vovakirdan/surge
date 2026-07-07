package parser

import "surge/internal/token"

// Contextual keyword texts for Epic 11 explicit crossing surface. `on` and
// `crosses` are contextual: they are recognized as keywords only in their
// specific syntactic positions and remain ordinary identifiers everywhere else
// (e.g. `let on = 1;`, `let crosses: int = 1;` keep parsing).
const (
	contextualOn      = "on"
	contextualCrosses = "crosses"
)

// atContextualKeyword reports whether the next token is an identifier whose text
// equals name. It never treats reserved keyword tokens as a match.
func (p *Parser) atContextualKeyword(name string) bool {
	tok := p.lx.Peek()
	return tok.Kind == token.Ident && tok.Text == name
}

// atOnCrossingHead reports whether the parser is at the head of an
// `on <dst> { ... }` crossing expression: the contextual `on` keyword followed
// immediately by a destination head. Valid Epic 11 destinations are
// identifier-headed (`pool`, `distributed`, `shard(id)`, `route_for(id)`, a
// `Placement` variable, or a `far` handle); the postponed `on blocking` and the
// invalid literal destinations (`on 1 { ... }`) are also recognized so sema can
// reject them deterministically. Because Surge has no expression juxtaposition,
// `on` followed by an identifier, a literal, or `blocking` is unambiguously a
// crossing, while `on` followed by an operator, `.`, `(`, `[`, `;`, `=`, or a
// terminator stays an ordinary identifier use (preserving `on(x)`, `on[i]`,
// `on.f`, `on + 1`, `on;`).
func (p *Parser) atOnCrossingHead() bool {
	if !p.atContextualKeyword(contextualOn) {
		return false
	}
	switch p.lx.Peek2().Kind {
	case token.Ident, token.KwBlocking,
		token.IntLit, token.UintLit, token.FloatLit,
		token.StringLit, token.FStringLit,
		token.KwTrue, token.KwFalse, token.NothingLit:
		return true
	default:
		return false
	}
}

// atContextualCrosses reports whether the next token is the contextual `crosses`
// effect keyword. Used only in function-signature positions, where an ordinary
// identifier is never syntactically valid, so the match is unambiguous.
func (p *Parser) atContextualCrosses() bool {
	return p.atContextualKeyword(contextualCrosses)
}
