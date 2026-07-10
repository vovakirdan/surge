package parser

import "surge/internal/token"

// Contextual keyword text for the explicit crossing surface. `on` is
// contextual: it is recognized as a keyword only in its specific syntactic
// positions and remains an ordinary identifier everywhere else (e.g.
// `let on = 1;` keeps parsing).
const contextualOn = "on"

// atContextualKeyword reports whether the next token is an identifier whose text
// equals name. It never treats reserved keyword tokens as a match.
func (p *Parser) atContextualKeyword(name string) bool {
	tok := p.lx.Peek()
	return tok.Kind == token.Ident && tok.Text == name
}

// atOnCrossingHead reports whether the parser is at the head of an
// `on <dst> { ... }` crossing expression: the contextual `on` keyword followed
// immediately by a destination head. Valid destinations are
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

// atSpawnOnRemoteHead reports whether, with the `spawn` keyword already consumed,
// the parser is at the head of a `spawn on <dst> { ... }` remote-spawn expression
// (Block 3). It matches the contextual `on` keyword followed either by a
// destination head (the same heads as atOnCrossingHead) or by `{`. The `{` case
// is the missing-destination shape `spawn on { ... }` (diagnosed SYN2033): after
// `spawn`, `on {` is unambiguously a remote spawn whose destination was omitted,
// since a local `spawn` of an identifier `on` would be followed by a terminator
// (`spawn on;`) rather than a block. `spawn on;` and other non-head follows keep
// `on` as an ordinary identifier so the existing local-spawn grammar applies.
func (p *Parser) atSpawnOnRemoteHead() bool {
	if !p.atContextualKeyword(contextualOn) {
		return false
	}
	switch p.lx.Peek2().Kind {
	case token.Ident, token.KwBlocking,
		token.IntLit, token.UintLit, token.FloatLit,
		token.StringLit, token.FStringLit,
		token.KwTrue, token.KwFalse, token.NothingLit,
		token.LBrace:
		return true
	default:
		return false
	}
}
